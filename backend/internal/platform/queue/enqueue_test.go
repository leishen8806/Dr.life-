package queue

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestEnqueueRejectsUnregisteredAndOversizedPayload(t *testing.T) {
	registry := NewRegistry()
	client := NewMemoryClient()
	enqueuer := NewEnqueuer(client, registry, EnqueueOptions{PayloadMaxBytes: 4})
	if _, err := enqueuer.Enqueue(context.Background(), "missing:task:v1", []byte("{}")); !errors.Is(err, ErrUnregisteredType) {
		t.Fatalf("unregistered enqueue error = %v", err)
	}
	if err := registry.Register(TypeSpec{Name: "ledger:outbox_relay:v1", Queue: QueueCritical, Timeout: time.Second, MaxRetry: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := enqueuer.Enqueue(context.Background(), "ledger:outbox_relay:v1", []byte("12345")); !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("oversized enqueue error = %v", err)
	}
}
