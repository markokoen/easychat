package nats

import "context"

type Publisher interface {
	Publish(ctx context.Context, subject string, payload []byte) error
}

type NoopPublisher struct{}

func NewNoopPublisher() *NoopPublisher {
	return &NoopPublisher{}
}

func (n *NoopPublisher) Publish(_ context.Context, _ string, _ []byte) error {
	return nil
}
