package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"easychat/internal/domain/chat"
	"easychat/internal/platform/id"
)

var ErrInvalidCredentials = errors.New("invalid credentials")

type LoginInput struct {
	UserID      string         `json:"userId"`
	DisplayName string         `json:"displayName"`
	Metadata    map[string]any `json:"metadata"`
}

type LoginOutput struct {
	Token string    `json:"token"`
	User  chat.User `json:"user"`
}

type Service struct {
	users  chat.UserRepository
	tokens TokenProvider
}

func NewService(users chat.UserRepository, tokens TokenProvider) *Service {
	return &Service{users: users, tokens: tokens}
}

func (s *Service) Login(ctx context.Context, input LoginInput) (*LoginOutput, error) {
	displayName := strings.TrimSpace(input.DisplayName)
	if displayName == "" {
		return nil, ErrInvalidCredentials
	}

	userID := strings.TrimSpace(input.UserID)
	if userID == "" {
		userID = id.New()
	}

	createdAt := time.Now().UTC().Truncate(time.Second)
	existing, err := s.users.GetByID(ctx, userID)
	if err == nil {
		createdAt = existing.CreatedAt
	} else if !errors.Is(err, chat.ErrNotFound) {
		return nil, err
	}

	user := &chat.User{
		ID:          userID,
		DisplayName: displayName,
		Metadata:    input.Metadata,
		CreatedAt:   createdAt,
	}
	if err := s.users.Upsert(ctx, user); err != nil {
		return nil, err
	}

	token, err := s.tokens.GenerateToken(ctx, Claims{
		UserID:      user.ID,
		DisplayName: user.DisplayName,
		Metadata:    user.Metadata,
	})
	if err != nil {
		return nil, err
	}

	return &LoginOutput{Token: token, User: *user}, nil
}

func (s *Service) ParseToken(ctx context.Context, bearerToken string) (Claims, error) {
	return s.tokens.ParseToken(ctx, bearerToken)
}
