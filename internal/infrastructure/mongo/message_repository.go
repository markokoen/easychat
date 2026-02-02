package mongo

import (
	"context"
	"errors"

	"github.com/markokoen/easychat/internal/domain/chat"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MessageRepository struct {
	col collectionAPI
}

func NewMessageRepository(db *mongo.Database) *MessageRepository {
	return NewMessageRepositoryWithDatabase(wrapDatabase(db))
}

func NewMessageRepositoryWithDatabase(db databaseAPI) *MessageRepository {
	return &MessageRepository{col: db.Collection("messages")}
}

func (r *MessageRepository) Create(ctx context.Context, message *chat.Message) error {
	_, err := r.col.InsertOne(ctx, message)
	return err
}

func (r *MessageRepository) GetByID(ctx context.Context, id string) (*chat.Message, error) {
	var message chat.Message
	if err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&message); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, chat.ErrNotFound
		}
		return nil, err
	}
	return &message, nil
}

func (r *MessageRepository) ListByChatRoom(ctx context.Context, chatRoomID string, limit int64) ([]chat.Message, error) {
	if limit <= 0 {
		limit = 50
	}
	cursor, err := r.col.Find(
		ctx,
		bson.M{"chatRoomId": chatRoomID},
		options.Find().SetSort(bson.D{{Key: "createdAt", Value: 1}}).SetLimit(limit),
	)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	messages := []chat.Message{}
	for cursor.Next(ctx) {
		var message chat.Message
		if err := cursor.Decode(&message); err != nil {
			return nil, err
		}
		messages = append(messages, message)
	}
	return messages, cursor.Err()
}

func (r *MessageRepository) UpsertDeliveryReceipt(ctx context.Context, messageID string, receipt chat.DeliveryReceipt) error {
	res, err := r.col.UpdateOne(
		ctx,
		bson.M{"_id": messageID, "deliveryReceipts.userId": receipt.UserID},
		bson.M{
			"$set": bson.M{
				"deliveryReceipts.$.userName":    receipt.UserName,
				"deliveryReceipts.$.status":      receipt.Status,
				"deliveryReceipts.$.sentAt":      receipt.SentAt,
				"deliveryReceipts.$.deliveredAt": receipt.DeliveredAt,
			},
		},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount > 0 {
		return nil
	}

	res, err = r.col.UpdateOne(
		ctx,
		bson.M{"_id": messageID},
		bson.M{"$push": bson.M{"deliveryReceipts": receipt}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return chat.ErrNotFound
	}
	return nil
}

func (r *MessageRepository) UpsertReadReceipt(ctx context.Context, messageID string, receipt chat.ReadReceipt) error {
	res, err := r.col.UpdateOne(
		ctx,
		bson.M{"_id": messageID, "readReceipts.userId": receipt.UserID},
		bson.M{
			"$set": bson.M{
				"readReceipts.$.userName": receipt.UserName,
				"readReceipts.$.readAt":   receipt.ReadAt,
			},
		},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount > 0 {
		return nil
	}

	res, err = r.col.UpdateOne(
		ctx,
		bson.M{"_id": messageID},
		bson.M{"$push": bson.M{"readReceipts": receipt}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return chat.ErrNotFound
	}
	return nil
}
