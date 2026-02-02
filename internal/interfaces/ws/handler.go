package ws

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	appauth "github.com/markokoen/easychat/internal/app/auth"
	appchat "github.com/markokoen/easychat/internal/app/chat"
	domain "github.com/markokoen/easychat/internal/domain/chat"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
)

var (
	maxMessageBytes int64 = 64 * 1024
	writeWait             = 10 * time.Second
	pongWait              = 60 * time.Second
	pingPeriod            = 50 * time.Second
)

type incomingEnvelope struct {
	Type      string          `json:"type"`
	RequestID string          `json:"requestId,omitempty"`
	Payload   json.RawMessage `json:"payload"`
}

type Handler struct {
	authService *appauth.Service
	chatService *appchat.Service
	manager     *Manager
	log         *slog.Logger
	upgrader    websocket.Upgrader
	upgrade     func(http.ResponseWriter, *http.Request) (wsConn, error)
}

func NewHandler(authService *appauth.Service, chatService *appchat.Service, manager *Manager, log *slog.Logger) *Handler {
	handler := &Handler{
		authService: authService,
		chatService: chatService,
		manager:     manager,
		log:         log,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			CheckOrigin: func(_ *http.Request) bool {
				return true
			},
		},
	}
	handler.upgrade = func(w http.ResponseWriter, r *http.Request) (wsConn, error) {
		return handler.upgrader.Upgrade(w, r, nil)
	}
	return handler
}

func (h *Handler) ServeWS(w http.ResponseWriter, r *http.Request) {
	claims, ok := h.extractClaims(r)
	if !ok {
		writeHTTPError(w, http.StatusUnauthorized, "missing or invalid bearer token")
		return
	}

	chatRoomID := strings.TrimSpace(mux.Vars(r)["chatRoomId"])
	if chatRoomID == "" {
		writeHTTPError(w, http.StatusBadRequest, "chatRoomId is required")
		return
	}

	if err := h.chatService.JoinChatRoom(r.Context(), chatRoomID, claims); err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			writeHTTPError(w, http.StatusNotFound, "chatroom not found")
			return
		}
		writeHTTPError(w, http.StatusInternalServerError, "failed to join chatroom")
		return
	}

	conn, err := h.upgrade(w, r)
	if err != nil {
		h.log.Error("failed to upgrade websocket", "error", err)
		return
	}

	client, replaced := h.manager.Register(chatRoomID, claims, conn)
	if replaced != nil {
		replaced.Close()
		_ = h.manager.Unregister(replaced)
	}

	h.sendChatHistory(r.Context(), client)

	h.manager.Broadcast(chatRoomID, Envelope{
		Type: "user.joined",
		Payload: map[string]any{
			"chatRoomId": chatRoomID,
			"userId":     claims.UserID,
			"userName":   claims.DisplayName,
			"joinedAt":   time.Now().UTC().Truncate(time.Second),
		},
	}, claims.UserID)

	go h.writePump(client)
	h.readPump(client)
}

func (h *Handler) sendChatHistory(ctx context.Context, client *Client) {
	messages, err := h.chatService.ListChatRoomMessages(ctx, client.chatRoomID, client.user)
	if err != nil {
		h.log.Warn("failed to load chat history", "error", err, "chatRoomId", client.chatRoomID, "userId", client.user.UserID)
		h.sendError(client, "failed to load chat history", "")
		return
	}

	for _, message := range messages {
		if ok := h.manager.Enqueue(client, Envelope{
			Type:    "message.created",
			Payload: message,
		}); !ok {
			return
		}
	}
}

func (h *Handler) readPump(client *Client) {
	defer h.cleanupClient(client)

	client.conn.SetReadLimit(maxMessageBytes)
	_ = client.conn.SetReadDeadline(time.Now().Add(pongWait))
	client.conn.SetPongHandler(func(_ string) error {
		return client.conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	for {
		_, raw, err := client.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				h.log.Warn("websocket closed unexpectedly", "error", err)
			}
			return
		}

		var incoming incomingEnvelope
		if err := json.Unmarshal(raw, &incoming); err != nil {
			h.sendError(client, "invalid envelope", "")
			continue
		}

		switch incoming.Type {
		case "message.send":
			h.handleMessageSend(client, incoming)
		case "message.read":
			h.handleMessageRead(client, incoming)
		default:
			h.sendError(client, "unsupported event type", incoming.RequestID)
		}
	}
}

func (h *Handler) writePump(client *Client) {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		client.Close()
	}()

	for {
		select {
		case message, ok := <-client.send:
			_ = client.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				_ = client.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			if err := client.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}
		case <-ticker.C:
			_ = client.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := client.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (h *Handler) cleanupClient(client *Client) {
	client.Close()
	removed := h.manager.Unregister(client)
	if removed {
		if err := h.chatService.LeaveChatRoom(context.Background(), client.chatRoomID, client.user.UserID); err != nil {
			h.log.Warn("failed to remove user from chatroom", "error", err, "chatRoomId", client.chatRoomID, "userId", client.user.UserID)
		}
		h.manager.Broadcast(client.chatRoomID, Envelope{
			Type: "user.left",
			Payload: map[string]any{
				"chatRoomId": client.chatRoomID,
				"userId":     client.user.UserID,
				"userName":   client.user.DisplayName,
				"leftAt":     time.Now().UTC().Truncate(time.Second),
			},
		}, client.user.UserID)
	}
}

func (h *Handler) handleMessageSend(client *Client, incoming incomingEnvelope) {
	var payload SendMessagePayload
	if err := json.Unmarshal(incoming.Payload, &payload); err != nil {
		h.sendError(client, "invalid message.send payload", incoming.RequestID)
		return
	}

	message, err := h.chatService.SendMessage(context.Background(), appchat.SendMessageInput{
		ChatRoomID: client.chatRoomID,
		Sender:     client.user,
		Content:    payload.Content,
	})
	if err != nil {
		h.sendError(client, mapSendError(err), incoming.RequestID)
		return
	}

	h.manager.Broadcast(client.chatRoomID, Envelope{
		Type:      "message.created",
		RequestID: incoming.RequestID,
		Payload:   message,
	}, "")

	h.manager.Enqueue(client, Envelope{
		Type:      "message.sent",
		RequestID: incoming.RequestID,
		Payload: map[string]any{
			"messageId": message.ID,
			"sentAt":    time.Now().UTC().Truncate(time.Second),
		},
	})

	for _, recipient := range h.manager.RoomUsers(client.chatRoomID) {
		if recipient.UserID == client.user.UserID {
			continue
		}
		receipt, err := h.chatService.MarkDelivered(context.Background(), message.ID, recipient)
		if err != nil {
			h.log.Warn("failed to mark delivered", "messageId", message.ID, "userId", recipient.UserID, "error", err)
			continue
		}
		h.manager.Broadcast(client.chatRoomID, Envelope{
			Type:    "message.delivered",
			Payload: map[string]any{"messageId": message.ID, "receipt": receipt},
		}, "")
	}
}

func (h *Handler) handleMessageRead(client *Client, incoming incomingEnvelope) {
	var payload ReadMessagePayload
	if err := json.Unmarshal(incoming.Payload, &payload); err != nil {
		h.sendError(client, "invalid message.read payload", incoming.RequestID)
		return
	}

	receipt, err := h.chatService.MarkRead(context.Background(), client.chatRoomID, payload.MessageID, client.user)
	if err != nil {
		h.sendError(client, "failed to mark message as read", incoming.RequestID)
		return
	}

	h.manager.Broadcast(client.chatRoomID, Envelope{
		Type:      "message.read",
		RequestID: incoming.RequestID,
		Payload: map[string]any{
			"messageId": payload.MessageID,
			"receipt":   receipt,
		},
	}, "")
}

func (h *Handler) sendError(client *Client, message string, requestID string) {
	h.manager.Enqueue(client, Envelope{
		Type:      "error",
		RequestID: requestID,
		Payload: map[string]any{
			"message": message,
		},
	})
}

func (h *Handler) extractClaims(r *http.Request) (appauth.Claims, bool) {
	token := ""
	authorization := strings.TrimSpace(r.Header.Get("Authorization"))
	if authorization != "" {
		parts := strings.SplitN(authorization, " ", 2)
		if len(parts) == 2 && strings.EqualFold(parts[0], "Bearer") && strings.TrimSpace(parts[1]) != "" {
			token = strings.TrimSpace(parts[1])
		}
	}
	if token == "" {
		token = strings.TrimSpace(r.URL.Query().Get("token"))
	}
	if token == "" {
		return appauth.Claims{}, false
	}
	claims, err := h.authService.ParseToken(r.Context(), token)
	if err != nil {
		return appauth.Claims{}, false
	}
	return claims, true
}

func writeHTTPError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"message": message})
}

func mapSendError(err error) string {
	switch {
	case errors.Is(err, appchat.ErrMessageEmptyBody):
		return "message content is required"
	case errors.Is(err, appchat.ErrMessageTooLarge):
		return "message content exceeds size limit"
	case errors.Is(err, appchat.ErrNotRoomMember):
		return "sender is not part of this chatroom"
	case errors.Is(err, domain.ErrNotFound):
		return "chatroom not found"
	default:
		return "failed to send message"
	}
}
