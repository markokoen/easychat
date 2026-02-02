package mongo

import (
	"context"
	"errors"

	"easychat/internal/domain/chat"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type ChatRoomRepository struct {
	col collectionAPI
}

func NewChatRoomRepository(db *mongo.Database) *ChatRoomRepository {
	return NewChatRoomRepositoryWithDatabase(wrapDatabase(db))
}

func NewChatRoomRepositoryWithDatabase(db databaseAPI) *ChatRoomRepository {
	return &ChatRoomRepository{col: db.Collection("chatrooms")}
}

func (r *ChatRoomRepository) Create(ctx context.Context, chatRoom *chat.ChatRoom) error {
	_, err := r.col.InsertOne(ctx, chatRoom)
	if isDuplicateKey(err) {
		return chat.ErrAlreadyExists
	}
	return err
}

func (r *ChatRoomRepository) GetByID(ctx context.Context, id string) (*chat.ChatRoom, error) {
	var room chat.ChatRoom
	if err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&room); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, chat.ErrNotFound
		}
		return nil, err
	}
	return &room, nil
}

func (r *ChatRoomRepository) GetByReference(ctx context.Context, reference string) (*chat.ChatRoom, error) {
	var room chat.ChatRoom
	if err := r.col.FindOne(ctx, bson.M{"reference": reference}).Decode(&room); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, chat.ErrNotFound
		}
		return nil, err
	}
	return &room, nil
}

func (r *ChatRoomRepository) AddUser(ctx context.Context, chatRoomID string, user chat.UserSummary) error {
	res, err := r.col.UpdateOne(
		ctx,
		bson.M{"_id": chatRoomID, "users.id": bson.M{"$ne": user.ID}},
		bson.M{"$push": bson.M{"users": user}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount > 0 {
		return nil
	}
	_, err = r.GetByID(ctx, chatRoomID)
	return err
}

func (r *ChatRoomRepository) RemoveUser(ctx context.Context, chatRoomID string, userID string) error {
	res, err := r.col.UpdateOne(
		ctx,
		bson.M{"_id": chatRoomID},
		bson.M{"$pull": bson.M{"users": bson.M{"id": userID}}},
	)
	if err != nil {
		return err
	}
	if res.MatchedCount == 0 {
		return chat.ErrNotFound
	}
	return nil
}
