package auth

import (
	"context"
	"errors"
	"time"

	appauth "easychat/internal/app/auth"

	"github.com/golang-jwt/jwt/v5"
)

type JWTProvider struct {
	secret []byte
	ttl    time.Duration
}

type tokenClaims struct {
	DisplayName string         `json:"displayName"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	jwt.RegisteredClaims
}

func NewJWTProvider(secret string, ttl time.Duration) *JWTProvider {
	return &JWTProvider{
		secret: []byte(secret),
		ttl:    ttl,
	}
}

func (j *JWTProvider) GenerateToken(_ context.Context, claims appauth.Claims) (string, error) {
	now := time.Now().UTC()
	jwtClaims := tokenClaims{
		DisplayName: claims.DisplayName,
		Metadata:    claims.Metadata,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   claims.UserID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(j.ttl)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwtClaims)
	return token.SignedString(j.secret)
}

func (j *JWTProvider) ParseToken(_ context.Context, tokenStr string) (appauth.Claims, error) {
	claims := &tokenClaims{}
	token, err := jwt.ParseWithClaims(tokenStr, claims, func(token *jwt.Token) (any, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return j.secret, nil
	})
	if err != nil {
		return appauth.Claims{}, err
	}
	if !token.Valid {
		return appauth.Claims{}, errors.New("invalid token")
	}
	if claims.Subject == "" {
		return appauth.Claims{}, errors.New("missing subject")
	}

	return appauth.Claims{
		UserID:      claims.Subject,
		DisplayName: claims.DisplayName,
		Metadata:    claims.Metadata,
	}, nil
}
