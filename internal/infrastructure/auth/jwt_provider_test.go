package auth

import (
	"context"
	"testing"
	"time"

	appauth "easychat/internal/app/auth"

	"github.com/golang-jwt/jwt/v5"
)

func TestJWTProviderGenerateAndParse(t *testing.T) {
	provider := NewJWTProvider("secret", time.Hour)
	token, err := provider.GenerateToken(context.Background(), appauth.Claims{
		UserID:      "u1",
		DisplayName: "User 1",
		Metadata:    map[string]any{"role": "admin"},
	})
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}

	claims, err := provider.ParseToken(context.Background(), token)
	if err != nil {
		t.Fatalf("parse token failed: %v", err)
	}
	if claims.UserID != "u1" || claims.DisplayName != "User 1" {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestJWTProviderParseInvalidToken(t *testing.T) {
	provider := NewJWTProvider("secret", time.Hour)
	if _, err := provider.ParseToken(context.Background(), "not-a-token"); err == nil {
		t.Fatalf("expected parse error")
	}
}

func TestJWTProviderParseWrongSecret(t *testing.T) {
	providerA := NewJWTProvider("secret-a", time.Hour)
	providerB := NewJWTProvider("secret-b", time.Hour)
	token, err := providerA.GenerateToken(context.Background(), appauth.Claims{UserID: "u1"})
	if err != nil {
		t.Fatalf("generate token failed: %v", err)
	}
	if _, err := providerB.ParseToken(context.Background(), token); err == nil {
		t.Fatalf("expected invalid signature error")
	}
}

func TestJWTProviderMissingSubject(t *testing.T) {
	provider := NewJWTProvider("secret", time.Hour)
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, tokenClaims{
		DisplayName: "no-subject",
	})
	raw, err := tok.SignedString([]byte("secret"))
	if err != nil {
		t.Fatalf("sign token failed: %v", err)
	}
	if _, err := provider.ParseToken(context.Background(), raw); err == nil {
		t.Fatalf("expected missing subject error")
	}
}
