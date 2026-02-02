package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/markokoen/easychat/internal/domain/chat"
)

type fakeUserRepo struct {
	users      map[string]chat.User
	getErr     error
	upsertErr  error
	upsertHits int
}

func (f *fakeUserRepo) GetByID(_ context.Context, id string) (*chat.User, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	if user, ok := f.users[id]; ok {
		return &user, nil
	}
	return nil, chat.ErrNotFound
}

func (f *fakeUserRepo) Upsert(_ context.Context, user *chat.User) error {
	f.upsertHits++
	if f.upsertErr != nil {
		return f.upsertErr
	}
	f.users[user.ID] = *user
	return nil
}

type fakeTokenProvider struct {
	generateErr error
	parseErr    error
	lastClaims  Claims
}

func (f *fakeTokenProvider) GenerateToken(_ context.Context, claims Claims) (string, error) {
	f.lastClaims = claims
	if f.generateErr != nil {
		return "", f.generateErr
	}
	return "token-" + claims.UserID, nil
}

func (f *fakeTokenProvider) ParseToken(_ context.Context, token string) (Claims, error) {
	if f.parseErr != nil {
		return Claims{}, f.parseErr
	}
	return Claims{UserID: token}, nil
}

func TestLoginSuccess(t *testing.T) {
	repo := &fakeUserRepo{users: map[string]chat.User{}}
	tokens := &fakeTokenProvider{}
	svc := NewService(repo, tokens)

	output, err := svc.Login(context.Background(), LoginInput{DisplayName: "Marko"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if output.Token == "" {
		t.Fatalf("expected token")
	}
	if output.User.ID == "" {
		t.Fatalf("expected generated user ID")
	}
	if tokens.lastClaims.UserID != output.User.ID {
		t.Fatalf("expected claim user ID to match output user")
	}
}

func TestLoginPreservesCreatedAtForExistingUser(t *testing.T) {
	createdAt := time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)
	repo := &fakeUserRepo{users: map[string]chat.User{
		"u1": {ID: "u1", DisplayName: "Old", CreatedAt: createdAt},
	}}
	svc := NewService(repo, &fakeTokenProvider{})

	output, err := svc.Login(context.Background(), LoginInput{UserID: "u1", DisplayName: "New"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if !output.User.CreatedAt.Equal(createdAt) {
		t.Fatalf("expected createdAt to be preserved")
	}
}

func TestLoginRejectsEmptyDisplayName(t *testing.T) {
	repo := &fakeUserRepo{users: map[string]chat.User{}}
	svc := NewService(repo, &fakeTokenProvider{})

	_, err := svc.Login(context.Background(), LoginInput{DisplayName: "  "})
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected invalid credentials error, got %v", err)
	}
}

func TestLoginReturnsGetByIDError(t *testing.T) {
	repo := &fakeUserRepo{users: map[string]chat.User{}, getErr: errors.New("db error")}
	svc := NewService(repo, &fakeTokenProvider{})

	_, err := svc.Login(context.Background(), LoginInput{UserID: "u1", DisplayName: "Marko"})
	if err == nil || err.Error() != "db error" {
		t.Fatalf("expected get error, got %v", err)
	}
}

func TestLoginReturnsUpsertError(t *testing.T) {
	repo := &fakeUserRepo{users: map[string]chat.User{}, upsertErr: errors.New("write failed")}
	svc := NewService(repo, &fakeTokenProvider{})

	_, err := svc.Login(context.Background(), LoginInput{DisplayName: "Marko"})
	if err == nil || err.Error() != "write failed" {
		t.Fatalf("expected upsert error, got %v", err)
	}
}

func TestLoginReturnsGenerateTokenError(t *testing.T) {
	repo := &fakeUserRepo{users: map[string]chat.User{}}
	svc := NewService(repo, &fakeTokenProvider{generateErr: errors.New("token failed")})

	_, err := svc.Login(context.Background(), LoginInput{DisplayName: "Marko"})
	if err == nil || err.Error() != "token failed" {
		t.Fatalf("expected token generation error, got %v", err)
	}
}

func TestParseTokenDelegatesToProvider(t *testing.T) {
	tokens := &fakeTokenProvider{}
	svc := NewService(&fakeUserRepo{users: map[string]chat.User{}}, tokens)

	claims, err := svc.ParseToken(context.Background(), "abc")
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if claims.UserID != "abc" {
		t.Fatalf("unexpected claims: %+v", claims)
	}

	tokens.parseErr = errors.New("bad")
	if _, err := svc.ParseToken(context.Background(), "abc"); err == nil {
		t.Fatalf("expected parse error")
	}
}
