package http

import (
	"encoding/json"
	"errors"
	"net/http"

	appchat "easychat/internal/app/chat"
	domain "easychat/internal/domain/chat"

	"github.com/gorilla/mux"
)

type ChatRoomHandler struct {
	chatService *appchat.Service
}

func NewChatRoomHandler(chatService *appchat.Service) *ChatRoomHandler {
	return &ChatRoomHandler{chatService: chatService}
}

func (h *ChatRoomHandler) Create(w http.ResponseWriter, r *http.Request) {
	var input appchat.CreateChatRoomInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	room, err := h.chatService.CreateChatRoom(r.Context(), input)
	if err != nil {
		switch {
		case errors.Is(err, appchat.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, "invalid chatroom payload")
		case errors.Is(err, domain.ErrAlreadyExists):
			writeError(w, http.StatusConflict, "chatroom reference already exists")
		default:
			writeError(w, http.StatusInternalServerError, "failed to create chatroom")
		}
		return
	}

	writeJSON(w, http.StatusCreated, room)
}

func (h *ChatRoomHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	chatRoomID := mux.Vars(r)["chatRoomId"]
	room, err := h.chatService.GetChatRoomByID(r.Context(), chatRoomID)
	if err != nil {
		switch {
		case errors.Is(err, appchat.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, "chatRoomId is required")
		case errors.Is(err, domain.ErrNotFound):
			writeError(w, http.StatusNotFound, "chatroom not found")
		default:
			writeError(w, http.StatusInternalServerError, "failed to get chatroom")
		}
		return
	}
	writeJSON(w, http.StatusOK, room)
}

func (h *ChatRoomHandler) GetByReference(w http.ResponseWriter, r *http.Request) {
	reference := mux.Vars(r)["reference"]
	room, err := h.chatService.GetChatRoomByReference(r.Context(), reference)
	if err != nil {
		switch {
		case errors.Is(err, appchat.ErrInvalidInput):
			writeError(w, http.StatusBadRequest, "reference is required")
		case errors.Is(err, domain.ErrNotFound):
			writeError(w, http.StatusNotFound, "chatroom not found")
		default:
			writeError(w, http.StatusInternalServerError, "failed to get chatroom")
		}
		return
	}
	writeJSON(w, http.StatusOK, room)
}
