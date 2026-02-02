package http

import (
	"net/http"
	"strings"

	appauth "easychat/internal/app/auth"

	"github.com/gorilla/mux"
)

func AuthMiddleware(authService *appauth.Service) mux.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authorization := strings.TrimSpace(r.Header.Get("Authorization"))
			parts := strings.SplitN(authorization, " ", 2)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || strings.TrimSpace(parts[1]) == "" {
				writeError(w, http.StatusUnauthorized, "missing or invalid bearer token")
				return
			}

			claims, err := authService.ParseToken(r.Context(), strings.TrimSpace(parts[1]))
			if err != nil {
				writeError(w, http.StatusUnauthorized, "invalid token")
				return
			}

			next.ServeHTTP(w, r.WithContext(withClaims(r.Context(), claims)))
		})
	}
}
