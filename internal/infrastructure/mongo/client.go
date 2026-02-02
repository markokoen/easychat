package mongo

import (
	"context"
	"time"

	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var mongoConnect = mongo.Connect

func Connect(ctx context.Context, uri string) (*mongo.Client, error) {
	return connect(ctx, uri, mongoConnect, func(client *mongo.Client, pingCtx context.Context) error {
		return client.Ping(pingCtx, nil)
	}, func(client *mongo.Client, disconnectCtx context.Context) error {
		return client.Disconnect(disconnectCtx)
	})
}

func connect(
	ctx context.Context,
	uri string,
	connectFn func(context.Context, ...*options.ClientOptions) (*mongo.Client, error),
	pingFn func(*mongo.Client, context.Context) error,
	disconnectFn func(*mongo.Client, context.Context) error,
) (*mongo.Client, error) {
	client, err := connectFn(ctx, options.Client().ApplyURI(uri))
	if err != nil {
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := pingFn(client, pingCtx); err != nil {
		_ = disconnectFn(client, ctx)
		return nil, err
	}
	return client, nil
}
