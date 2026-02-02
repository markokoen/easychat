package http

import (
	"context"

	appauth "github.com/markokoen/easychat/internal/app/auth"
)

type ctxKey string

const claimsCtxKey ctxKey = "claims"

func withClaims(ctx context.Context, claims appauth.Claims) context.Context {
	return context.WithValue(ctx, claimsCtxKey, claims)
}

func ClaimsFromContext(ctx context.Context) (appauth.Claims, bool) {
	claims, ok := ctx.Value(claimsCtxKey).(appauth.Claims)
	return claims, ok
}
