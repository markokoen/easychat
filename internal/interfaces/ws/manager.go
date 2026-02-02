package ws

import (
	"encoding/json"
	"log/slog"
	"sync"
	"time"

	appauth "github.com/markokoen/easychat/internal/app/auth"
)

const outboundBufferSize = 64

type Client struct {
	chatRoomID string
	user       appauth.Claims
	conn       wsConn
	send       chan []byte
	closeOnce  sync.Once
}

type wsConn interface {
	Close() error
	ReadMessage() (messageType int, p []byte, err error)
	WriteMessage(messageType int, data []byte) error
	SetReadLimit(limit int64)
	SetReadDeadline(t time.Time) error
	SetPongHandler(h func(string) error)
	SetWriteDeadline(t time.Time) error
}

type Manager struct {
	mu    sync.RWMutex
	rooms map[string]map[string]*Client
	log   *slog.Logger
}

func NewManager(log *slog.Logger) *Manager {
	return &Manager{
		rooms: make(map[string]map[string]*Client),
		log:   log,
	}
}

func (m *Manager) Register(chatRoomID string, user appauth.Claims, conn wsConn) (*Client, *Client) {
	client := &Client{
		chatRoomID: chatRoomID,
		user:       user,
		conn:       conn,
		send:       make(chan []byte, outboundBufferSize),
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if _, ok := m.rooms[chatRoomID]; !ok {
		m.rooms[chatRoomID] = make(map[string]*Client)
	}

	var replaced *Client
	if existing, ok := m.rooms[chatRoomID][user.UserID]; ok {
		replaced = existing
	}
	m.rooms[chatRoomID][user.UserID] = client
	return client, replaced
}

func (m *Manager) Unregister(client *Client) bool {
	m.mu.Lock()
	defer m.mu.Unlock()

	users, ok := m.rooms[client.chatRoomID]
	if !ok {
		return false
	}
	if current, ok := users[client.user.UserID]; ok && current == client {
		delete(users, client.user.UserID)
		if len(users) == 0 {
			delete(m.rooms, client.chatRoomID)
		}
		return true
	}
	return false
}

func (m *Manager) Enqueue(client *Client, envelope Envelope) bool {
	payload, err := json.Marshal(envelope)
	if err != nil {
		m.log.Error("failed to marshal envelope", "error", err)
		return false
	}

	select {
	case client.send <- payload:
		return true
	default:
		m.log.Warn("disconnecting slow websocket client", "userId", client.user.UserID, "chatRoomId", client.chatRoomID)
		client.Close()
		_ = m.Unregister(client)
		return false
	}
}

func (m *Manager) Broadcast(chatRoomID string, envelope Envelope, excludeUserID string) {
	m.mu.RLock()
	users, ok := m.rooms[chatRoomID]
	if !ok {
		m.mu.RUnlock()
		return
	}
	targets := make([]*Client, 0, len(users))
	for userID, client := range users {
		if excludeUserID != "" && userID == excludeUserID {
			continue
		}
		targets = append(targets, client)
	}
	m.mu.RUnlock()

	for _, target := range targets {
		m.Enqueue(target, envelope)
	}
}

func (m *Manager) SendToUser(chatRoomID string, userID string, envelope Envelope) {
	m.mu.RLock()
	client := m.rooms[chatRoomID][userID]
	m.mu.RUnlock()
	if client == nil {
		return
	}
	m.Enqueue(client, envelope)
}

func (m *Manager) RoomUserIDs(chatRoomID string) []string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	users, ok := m.rooms[chatRoomID]
	if !ok {
		return nil
	}
	ids := make([]string, 0, len(users))
	for userID := range users {
		ids = append(ids, userID)
	}
	return ids
}

func (m *Manager) RoomUsers(chatRoomID string) []appauth.Claims {
	m.mu.RLock()
	defer m.mu.RUnlock()
	users, ok := m.rooms[chatRoomID]
	if !ok {
		return nil
	}
	claims := make([]appauth.Claims, 0, len(users))
	for _, client := range users {
		claims = append(claims, client.user)
	}
	return claims
}

func (m *Manager) Shutdown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, users := range m.rooms {
		for _, client := range users {
			client.Close()
		}
	}
	m.rooms = make(map[string]map[string]*Client)
}

func (c *Client) Close() {
	c.closeOnce.Do(func() {
		close(c.send)
		_ = c.conn.Close()
	})
}
