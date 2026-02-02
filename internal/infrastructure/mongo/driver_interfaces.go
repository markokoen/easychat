package mongo

import (
	"context"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type databaseAPI interface {
	Collection(name string, opts ...*options.CollectionOptions) collectionAPI
}

type collectionAPI interface {
	InsertOne(ctx context.Context, document any, opts ...*options.InsertOneOptions) (*mongo.InsertOneResult, error)
	FindOne(ctx context.Context, filter any, opts ...*options.FindOneOptions) singleResultAPI
	UpdateOne(ctx context.Context, filter any, update any, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error)
	UpdateByID(ctx context.Context, id any, update any, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error)
	Find(ctx context.Context, filter any, opts ...*options.FindOptions) (cursorAPI, error)
}

type singleResultAPI interface {
	Decode(v any) error
}

type cursorAPI interface {
	Next(context.Context) bool
	Decode(any) error
	Close(context.Context) error
	Err() error
}

type mongoDatabase struct {
	db *mongo.Database
}

func wrapDatabase(db *mongo.Database) databaseAPI {
	return mongoDatabase{db: db}
}

func (d mongoDatabase) Collection(name string, opts ...*options.CollectionOptions) collectionAPI {
	return mongoCollection{col: d.db.Collection(name, opts...)}
}

type mongoCollection struct {
	col *mongo.Collection
}

func (c mongoCollection) InsertOne(ctx context.Context, document any, opts ...*options.InsertOneOptions) (*mongo.InsertOneResult, error) {
	return c.col.InsertOne(ctx, document, opts...)
}

func (c mongoCollection) FindOne(ctx context.Context, filter any, opts ...*options.FindOneOptions) singleResultAPI {
	return mongoSingleResult{result: c.col.FindOne(ctx, filter, opts...)}
}

func (c mongoCollection) UpdateOne(ctx context.Context, filter any, update any, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	return c.col.UpdateOne(ctx, filter, update, opts...)
}

func (c mongoCollection) UpdateByID(ctx context.Context, id any, update any, opts ...*options.UpdateOptions) (*mongo.UpdateResult, error) {
	return c.col.UpdateByID(ctx, id, update, opts...)
}

func (c mongoCollection) Find(ctx context.Context, filter any, opts ...*options.FindOptions) (cursorAPI, error) {
	cursor, err := c.col.Find(ctx, filter, opts...)
	if err != nil {
		return nil, err
	}
	return mongoCursor{cursor: cursor}, nil
}

type mongoSingleResult struct {
	result *mongo.SingleResult
}

func (s mongoSingleResult) Decode(v any) error {
	return s.result.Decode(v)
}

type mongoCursor struct {
	cursor *mongo.Cursor
}

func (c mongoCursor) Next(ctx context.Context) bool {
	return c.cursor.Next(ctx)
}

func (c mongoCursor) Decode(v any) error {
	return c.cursor.Decode(v)
}

func (c mongoCursor) Close(ctx context.Context) error {
	return c.cursor.Close(ctx)
}

func (c mongoCursor) Err() error {
	return c.cursor.Err()
}
