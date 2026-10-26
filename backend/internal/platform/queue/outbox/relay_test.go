package outbox_test

import (
	"context"
	"testing"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue/outbox"
)

func TestRelayRequiresDependencies(t *testing.T) {
	if _, err := outbox.New(nil, nil, outbox.Options{}); err == nil {
		t.Fatal("relay accepted missing dependencies")
	}
	if outbox.DefaultBatchSize != 100 {
		t.Fatalf("batch size=%d, want 100", outbox.DefaultBatchSize)
	}
	_ = context.Background()
}
