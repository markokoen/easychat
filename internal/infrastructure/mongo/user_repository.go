package mongo

import (
	"context"
	"errors"

	"easychat/internal/domain/chat"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type UserRepository struct {
	col collectionAPI
}

func NewUserRepository(db *mongo.Database) *UserRepository {
	return NewUserRepositoryWithDatabase(wrapDatabase(db))
}

func NewUserRepositoryWithDatabase(db databaseAPI) *UserRepository {
	return &UserRepository{col: db.Collection("users")}
}

func (r *UserRepository) GetByID(ctx context.Context, id string) (*chat.User, error) {
	var user chat.User
	if err := r.col.FindOne(ctx, bson.M{"_id": id}).Decode(&user); err != nil {
		if errors.Is(err, mongo.ErrNoDocuments) {
			return nil, chat.ErrNotFound
		}
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) Upsert(ctx context.Context, user *chat.User) error {
	_, err := r.col.UpdateByID(ctx, user.ID, bson.M{"$set": user}, options.Update().SetUpsert(true))
	return err
}
