package chat

import "time"

const (
	DeliveryStatusSent      = "SENT"
	DeliveryStatusDelivered = "DELIVERED"
)

type User struct {
	ID          string         `json:"id" bson:"_id"`
	DisplayName string         `json:"displayName" bson:"displayName"`
	Metadata    map[string]any `json:"metadata,omitempty" bson:"metadata,omitempty"`
	CreatedAt   time.Time      `json:"createdAt" bson:"createdAt"`
}

type UserSummary struct {
	ID          string `json:"id" bson:"id"`
	DisplayName string `json:"displayName" bson:"displayName"`
}

type ChatRoom struct {
	ID        string        `json:"id" bson:"_id"`
	Reference string        `json:"reference" bson:"reference"`
	Users     []UserSummary `json:"users" bson:"users"`
	CreatedAt time.Time     `json:"createdAt" bson:"createdAt"`
}

type DeliveryReceipt struct {
	UserID      string    `json:"userId" bson:"userId"`
	UserName    string    `json:"userName" bson:"userName"`
	Status      string    `json:"status" bson:"status"`
	SentAt      time.Time `json:"sentAt" bson:"sentAt"`
	DeliveredAt time.Time `json:"deliveredAt,omitempty" bson:"deliveredAt,omitempty"`
}

type ReadReceipt struct {
	UserID   string    `json:"userId" bson:"userId"`
	UserName string    `json:"userName" bson:"userName"`
	ReadAt   time.Time `json:"readAt" bson:"readAt"`
}

type Message struct {
	ID               string            `json:"id" bson:"_id"`
	ChatRoomID       string            `json:"chatRoomId" bson:"chatRoomId"`
	SenderUserID     string            `json:"senderUserId" bson:"senderUserId"`
	SenderUserName   string            `json:"senderUserName" bson:"senderUserName"`
	Content          string            `json:"content" bson:"content"`
	CreatedAt        time.Time         `json:"createdAt" bson:"createdAt"`
	DeliveryReceipts []DeliveryReceipt `json:"deliveryReceipts" bson:"deliveryReceipts"`
	ReadReceipts     []ReadReceipt     `json:"readReceipts" bson:"readReceipts"`
}
