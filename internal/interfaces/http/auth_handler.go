package http

import (
	"encoding/json"
	"errors"
	"net/http"

	appauth "github.com/markokoen/easychat/internal/app/auth"
)

type AuthHandler struct {
	authService *appauth.Service
}

func NewAuthHandler(authService *appauth.Service) *AuthHandler {
	return &AuthHandler{authService: authService}
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	var input appauth.LoginInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}

	output, err := h.authService.Login(r.Context(), input)
	if err != nil {
		if errors.Is(err, appauth.ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		writeError(w, http.StatusInternalServerError, "failed to login")
		return
	}

	writeJSON(w, http.StatusOK, output)
}
