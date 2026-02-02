package auth

import "context"

type Claims struct {
	UserID      string         `json:"userId"`
	DisplayName string         `json:"displayName"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

type TokenProvider interface {
	GenerateToken(ctx context.Context, claims Claims) (string, error)
	ParseToken(ctx context.Context, token string) (Claims, error)
}
