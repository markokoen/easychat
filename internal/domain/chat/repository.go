package chat

import (
	"context"
	"errors"
)

var ErrNotFound = errors.New("resource not found")
var ErrAlreadyExists = errors.New("resource already exists")

type UserRepository interface {
	GetByID(ctx context.Context, id string) (*User, error)
	Upsert(ctx context.Context, user *User) error
}

type ChatRoomRepository interface {
	Create(ctx context.Context, chatRoom *ChatRoom) error
	GetByID(ctx context.Context, id string) (*ChatRoom, error)
	GetByReference(ctx context.Context, reference string) (*ChatRoom, error)
	AddUser(ctx context.Context, chatRoomID string, user UserSummary) error
	RemoveUser(ctx context.Context, chatRoomID string, userID string) error
}

type MessageRepository interface {
	Create(ctx context.Context, message *Message) error
	GetByID(ctx context.Context, id string) (*Message, error)
	ListByChatRoom(ctx context.Context, chatRoomID string, limit int64) ([]Message, error)
	UpsertDeliveryReceipt(ctx context.Context, messageID string, receipt DeliveryReceipt) error
	UpsertReadReceipt(ctx context.Context, messageID string, receipt ReadReceipt) error
}
