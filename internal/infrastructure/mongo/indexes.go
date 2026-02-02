package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type indexViewAPI interface {
	CreateOne(ctx context.Context, model mongo.IndexModel, opts ...*options.CreateIndexesOptions) (string, error)
	CreateMany(ctx context.Context, models []mongo.IndexModel, opts ...*options.CreateIndexesOptions) ([]string, error)
}

type indexCollectionAPI interface {
	Indexes() indexViewAPI
}

type indexDatabaseAPI interface {
	Collection(name string, opts ...*options.CollectionOptions) indexCollectionAPI
}

type mongoIndexDatabase struct {
	db *mongo.Database
}

func wrapIndexDatabase(db *mongo.Database) indexDatabaseAPI {
	return mongoIndexDatabase{db: db}
}

func (d mongoIndexDatabase) Collection(name string, opts ...*options.CollectionOptions) indexCollectionAPI {
	return mongoIndexCollection{collection: d.db.Collection(name, opts...)}
}

type mongoIndexCollection struct {
	collection *mongo.Collection
}

func (c mongoIndexCollection) Indexes() indexViewAPI {
	return mongoIndexView{view: c.collection.Indexes()}
}

type mongoIndexView struct {
	view mongo.IndexView
}

func (v mongoIndexView) CreateOne(ctx context.Context, model mongo.IndexModel, opts ...*options.CreateIndexesOptions) (string, error) {
	return v.view.CreateOne(ctx, model, opts...)
}

func (v mongoIndexView) CreateMany(ctx context.Context, models []mongo.IndexModel, opts ...*options.CreateIndexesOptions) ([]string, error) {
	return v.view.CreateMany(ctx, models, opts...)
}

func EnsureIndexes(ctx context.Context, db *mongo.Database) error {
	return ensureIndexes(ctx, wrapIndexDatabase(db))
}

func ensureIndexes(ctx context.Context, db indexDatabaseAPI) error {
	if _, err := db.Collection("chatrooms").Indexes().CreateOne(ctx, mongo.IndexModel{
		Keys:    bson.D{{Key: "reference", Value: 1}},
		Options: options.Index().SetUnique(true),
	}); err != nil {
		return err
	}

	if _, err := db.Collection("messages").Indexes().CreateMany(ctx, []mongo.IndexModel{
		{Keys: bson.D{{Key: "chatRoomId", Value: 1}, {Key: "createdAt", Value: 1}}},
		{Keys: bson.D{{Key: "deliveryReceipts.userId", Value: 1}}},
		{Keys: bson.D{{Key: "readReceipts.userId", Value: 1}}},
	}); err != nil {
		return err
	}

	return nil
}
