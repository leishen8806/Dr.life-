package trade

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
)

func TestReservationMaintenanceScheduleAndHandler(t *testing.T) {
	registry := queue.NewRegistry()
	if err := RegisterReservationTask(registry); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Lookup(TaskReservationActions); !ok {
		t.Fatal("reservation maintenance task was not registered")
	}
	processor := &reservationProcessorFixture{}
	payload := ReservationScheduledTask().Payload(time.Date(2026, 11, 9, 10, 3, 0, 0, time.UTC))
	if err := NewReservationHandler(processor)(context.Background(), asynq.NewTask(TaskReservationActions, payload)); err != nil {
		t.Fatal(err)
	}
	if processor.limit != 100 {
		t.Fatalf("limit=%d, want 100", processor.limit)
	}
	if err := NewReservationHandler(processor)(context.Background(), asynq.NewTask(TaskReservationActions, []byte(`{}`))); !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("invalid payload error=%v, want SkipRetry", err)
	}
}

type reservationProcessorFixture struct{ limit int }

func (p *reservationProcessorFixture) ProcessReservationActions(_ context.Context, limit int) error {
	p.limit = limit
	return nil
}
