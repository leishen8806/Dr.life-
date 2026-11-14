package kess

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
	"github.com/leishen8806/Dr.life-/backend/internal/trade"
)

func TestMaintenanceRecoversInvalidatedSuccessForManualReconciliation(t *testing.T) {
	intent := Intent{ID: 21, SourceRef: "buyer-order", TradeNo: "DRL-buyer-order-1", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller", Status: StatusInvalidated}
	store := &memoryStore{intent: intent, due: []Intent{intent}}
	client := &queryClient{result: ChannelOrder{TradeNo: intent.TradeNo, SellerCode: "seller", AmountE4: 10000, Currency: "USD", Status: "SUCCESS", TransactionID: "late-tx"}}
	confirmer := &confirmFixture{err: trade.ErrReservationReleased}
	maintenance, err := NewMaintenance(store, newTestWorker(t, store, client, 24*time.Hour), confirmer)
	if err != nil {
		t.Fatal(err)
	}

	if err := maintenance.ProcessDue(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	if confirmer.calls != 1 || confirmer.source.Reference != "late-tx" || store.intent.Status != StatusSuccess {
		t.Fatalf("calls=%d source=%+v intent=%+v", confirmer.calls, confirmer.source, store.intent)
	}
}

func TestMaintenanceClosesExpiredPendingWithoutPaymentLink(t *testing.T) {
	now := time.Date(2026, 11, 9, 10, 0, 0, 0, time.UTC)
	expiresAt := now.Add(-time.Minute)
	intent := Intent{ID: 22, SourceRef: "buyer-order-2", TradeNo: "DRL-buyer-order-2-1", AmountE4: 10000, Currency: "USD", ExpectedSellerCode: "seller", Status: StatusPending, CreateAttempts: 1, ExpiresAt: &expiresAt}
	store := &memoryStore{intent: intent, due: []Intent{intent}, now: func() time.Time { return now }}
	worker := newTestWorker(t, store, &queryClient{queryErr: ErrChannelMissing}, 24*time.Hour)
	worker.now = func() time.Time { return now }
	maintenance, err := NewMaintenance(store, worker, &confirmFixture{})
	if err != nil {
		t.Fatal(err)
	}
	maintenance.now = func() time.Time { return now }

	if err := maintenance.ProcessDue(context.Background(), 100); err != nil {
		t.Fatal(err)
	}
	if store.intent.Status != StatusClosed {
		t.Fatalf("status=%q, want CLOSED", store.intent.Status)
	}
}

func TestKESSMaintenanceScheduleAndHandler(t *testing.T) {
	registry := queue.NewRegistry()
	if err := RegisterTask(registry); err != nil {
		t.Fatal(err)
	}
	if _, ok := registry.Lookup(TaskReconcilePayments); !ok {
		t.Fatal("KESS maintenance task was not registered")
	}
	processor := &dueProcessorFixture{}
	payload := ScheduledTask().Payload(time.Date(2026, 11, 9, 10, 2, 0, 0, time.UTC))
	if err := NewHandler(processor)(context.Background(), asynq.NewTask(TaskReconcilePayments, payload)); err != nil {
		t.Fatal(err)
	}
	if processor.limit != 100 {
		t.Fatalf("limit=%d, want 100", processor.limit)
	}
	if err := NewHandler(processor)(context.Background(), asynq.NewTask(TaskReconcilePayments, []byte(`{}`))); !errors.Is(err, asynq.SkipRetry) {
		t.Fatalf("invalid payload error=%v, want SkipRetry", err)
	}
}

type confirmFixture struct {
	calls  int
	source trade.EventSource
	err    error
}

func (c *confirmFixture) ConfirmPayment(_ context.Context, _ string, _ int64, source trade.EventSource) (bool, error) {
	c.calls++
	c.source = source
	return false, c.err
}

type dueProcessorFixture struct{ limit int }

func (p *dueProcessorFixture) ProcessDue(_ context.Context, limit int) error {
	p.limit = limit
	return nil
}
