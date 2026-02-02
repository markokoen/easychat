package nats

import (
	"context"
	"testing"
)

func TestNoopPublisher(t *testing.T) {
	pub := NewNoopPublisher()
	if pub == nil {
		t.Fatalf("expected publisher")
	}
	if err := pub.Publish(context.Background(), "topic", []byte("payload")); err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
}
