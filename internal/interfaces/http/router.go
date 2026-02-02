package http

import (
	"net/http"

	appauth "easychat/internal/app/auth"
	"easychat/internal/interfaces/ws"

	"github.com/gorilla/mux"
)

func NewRouter(authService *appauth.Service, authHandler *AuthHandler, chatRoomHandler *ChatRoomHandler, wsHandler *ws.Handler) http.Handler {
	r := mux.NewRouter()

	api := r.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/auth/login", authHandler.Login).Methods(http.MethodPost)

	protected := api.NewRoute().Subrouter()
	protected.Use(AuthMiddleware(authService))
	protected.HandleFunc("/chatrooms", chatRoomHandler.Create).Methods(http.MethodPost)
	protected.HandleFunc("/chatrooms/{chatRoomId}", chatRoomHandler.GetByID).Methods(http.MethodGet)
	protected.HandleFunc("/chatrooms/reference/{reference}", chatRoomHandler.GetByReference).Methods(http.MethodGet)

	r.HandleFunc("/ws/chatrooms/{chatRoomId}", wsHandler.ServeWS).Methods(http.MethodGet)
	r.PathPrefix("/swagger/").Handler(http.StripPrefix("/swagger/", http.FileServer(http.Dir("docs/swagger"))))

	r.NotFoundHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		writeError(w, http.StatusNotFound, "route not found")
	})

	return r
}
