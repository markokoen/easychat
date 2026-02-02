package chat

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	authapp "github.com/markokoen/easychat/internal/app/auth"
	domain "github.com/markokoen/easychat/internal/domain/chat"
	"github.com/markokoen/easychat/internal/platform/id"
)

var (
	ErrInvalidInput     = errors.New("invalid input")
	ErrNotRoomMember    = errors.New("user is not in chat room")
	ErrDuplicateRef     = errors.New("chat room reference already exists")
	ErrMessageTooLarge  = errors.New("message content too large")
	ErrMessageEmptyBody = errors.New("message content is required")
)

type Service struct {
	users     domain.UserRepository
	chatRooms domain.ChatRoomRepository
	messages  domain.MessageRepository
}

func NewService(users domain.UserRepository, chatRooms domain.ChatRoomRepository, messages domain.MessageRepository) *Service {
	return &Service{users: users, chatRooms: chatRooms, messages: messages}
}

type CreateChatRoomInput struct {
	Reference string               `json:"reference"`
	Users     []domain.UserSummary `json:"users"`
}

func (s *Service) CreateChatRoom(ctx context.Context, input CreateChatRoomInput) (*domain.ChatRoom, error) {
	reference := strings.TrimSpace(input.Reference)
	if reference == "" {
		return nil, ErrInvalidInput
	}
	if len(input.Users) == 0 {
		return nil, ErrInvalidInput
	}

	users := make([]domain.UserSummary, 0, len(input.Users))
	seen := make(map[string]struct{}, len(input.Users))
	for _, user := range input.Users {
		uid := strings.TrimSpace(user.ID)
		uname := strings.TrimSpace(user.DisplayName)
		if uid == "" || uname == "" {
			return nil, ErrInvalidInput
		}
		if _, ok := seen[uid]; ok {
			continue
		}
		seen[uid] = struct{}{}
		users = append(users, domain.UserSummary{ID: uid, DisplayName: uname})

		existing, err := s.users.GetByID(ctx, uid)
		createdAt := time.Now().UTC().Truncate(time.Second)
		if err == nil {
			createdAt = existing.CreatedAt
		} else if !errors.Is(err, domain.ErrNotFound) {
			return nil, err
		}
		if err := s.users.Upsert(ctx, &domain.User{ID: uid, DisplayName: uname, CreatedAt: createdAt}); err != nil {
			return nil, err
		}
	}

	sort.Slice(users, func(i, j int) bool {
		return users[i].ID < users[j].ID
	})

	chatRoom := &domain.ChatRoom{
		ID:        id.New(),
		Reference: reference,
		Users:     users,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
	}

	if err := s.chatRooms.Create(ctx, chatRoom); err != nil {
		return nil, err
	}
	return chatRoom, nil
}

func (s *Service) GetChatRoomByID(ctx context.Context, chatRoomID string) (*domain.ChatRoom, error) {
	chatRoomID = strings.TrimSpace(chatRoomID)
	if chatRoomID == "" {
		return nil, ErrInvalidInput
	}
	return s.chatRooms.GetByID(ctx, chatRoomID)
}

func (s *Service) GetChatRoomByReference(ctx context.Context, reference string) (*domain.ChatRoom, error) {
	reference = strings.TrimSpace(reference)
	if reference == "" {
		return nil, ErrInvalidInput
	}
	return s.chatRooms.GetByReference(ctx, reference)
}

func (s *Service) JoinChatRoom(ctx context.Context, chatRoomID string, claims authapp.Claims) error {
	chatRoom, err := s.chatRooms.GetByID(ctx, chatRoomID)
	if err != nil {
		return err
	}

	alreadyMember := false
	for _, user := range chatRoom.Users {
		if user.ID == claims.UserID {
			alreadyMember = true
			break
		}
	}
	if alreadyMember {
		return nil
	}

	if err := s.chatRooms.AddUser(ctx, chatRoomID, domain.UserSummary{ID: claims.UserID, DisplayName: claims.DisplayName}); err != nil {
		return err
	}
	return nil
}

func (s *Service) LeaveChatRoom(ctx context.Context, chatRoomID string, userID string) error {
	chatRoomID = strings.TrimSpace(chatRoomID)
	userID = strings.TrimSpace(userID)
	if chatRoomID == "" || userID == "" {
		return ErrInvalidInput
	}
	return s.chatRooms.RemoveUser(ctx, chatRoomID, userID)
}

type SendMessageInput struct {
	ChatRoomID string
	Sender     authapp.Claims
	Content    string
}

func (s *Service) SendMessage(ctx context.Context, input SendMessageInput) (*domain.Message, error) {
	chatRoom, err := s.chatRooms.GetByID(ctx, strings.TrimSpace(input.ChatRoomID))
	if err != nil {
		return nil, err
	}

	content := strings.TrimSpace(input.Content)
	if content == "" {
		return nil, ErrMessageEmptyBody
	}
	if len(content) > 4000 {
		return nil, ErrMessageTooLarge
	}

	isMember := false
	for _, user := range chatRoom.Users {
		if user.ID == input.Sender.UserID {
			isMember = true
			break
		}
	}
	if !isMember {
		return nil, ErrNotRoomMember
	}

	now := time.Now().UTC().Truncate(time.Second)
	message := &domain.Message{
		ID:             id.New(),
		ChatRoomID:     chatRoom.ID,
		SenderUserID:   input.Sender.UserID,
		SenderUserName: input.Sender.DisplayName,
		Content:        content,
		CreatedAt:      now,
		DeliveryReceipts: []domain.DeliveryReceipt{{
			UserID:   input.Sender.UserID,
			UserName: input.Sender.DisplayName,
			Status:   domain.DeliveryStatusSent,
			SentAt:   now,
		}},
		ReadReceipts: []domain.ReadReceipt{},
	}

	if err := s.messages.Create(ctx, message); err != nil {
		return nil, err
	}
	return message, nil
}

func (s *Service) MarkDelivered(ctx context.Context, messageID string, claims authapp.Claims) (*domain.DeliveryReceipt, error) {
	msg, err := s.messages.GetByID(ctx, strings.TrimSpace(messageID))
	if err != nil {
		return nil, err
	}

	chatRoom, err := s.chatRooms.GetByID(ctx, msg.ChatRoomID)
	if err != nil {
		return nil, err
	}

	isMember := false
	for _, user := range chatRoom.Users {
		if user.ID == claims.UserID {
			isMember = true
			break
		}
	}
	if !isMember {
		return nil, ErrNotRoomMember
	}

	now := time.Now().UTC().Truncate(time.Second)
	receipt := domain.DeliveryReceipt{
		UserID:      claims.UserID,
		UserName:    claims.DisplayName,
		Status:      domain.DeliveryStatusDelivered,
		SentAt:      now,
		DeliveredAt: now,
	}

	if err := s.messages.UpsertDeliveryReceipt(ctx, msg.ID, receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}

func (s *Service) MarkRead(ctx context.Context, chatRoomID string, messageID string, claims authapp.Claims) (*domain.ReadReceipt, error) {
	chatRoom, err := s.chatRooms.GetByID(ctx, strings.TrimSpace(chatRoomID))
	if err != nil {
		return nil, err
	}

	isMember := false
	for _, user := range chatRoom.Users {
		if user.ID == claims.UserID {
			isMember = true
			break
		}
	}
	if !isMember {
		return nil, ErrNotRoomMember
	}

	msg, err := s.messages.GetByID(ctx, strings.TrimSpace(messageID))
	if err != nil {
		return nil, err
	}
	if msg.ChatRoomID != chatRoom.ID {
		return nil, ErrInvalidInput
	}

	now := time.Now().UTC().Truncate(time.Second)
	receipt := domain.ReadReceipt{
		UserID:   claims.UserID,
		UserName: claims.DisplayName,
		ReadAt:   now,
	}

	if err := s.messages.UpsertReadReceipt(ctx, msg.ID, receipt); err != nil {
		return nil, err
	}
	return &receipt, nil
}
