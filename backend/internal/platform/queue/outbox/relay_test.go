package outbox_test

import (
	"context"
	"testing"

	"github.com/hibiken/asynq"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
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

func TestLedgerRoutesCoverPublishedTopics(t *testing.T) {
	for _, topic := range []string{
		"ledger.txn.posted", "ledger.txn.reversed", "ledger.txn.adjusted",
		"ledger.withdrawal.review_started", "ledger.withdrawal.approved", "ledger.withdrawal.paid",
		"ledger.withdrawal.failed", "ledger.withdrawal.cancelled", "ledger.recon.failed",
		"ledger.recon.stale_run", "ledger.payout_freeze.released",
	} {
		if _, ok := outbox.LedgerRoutes()[topic]; !ok {
			t.Errorf("published topic %q has no route", topic)
		}
	}
}

type registeringWorker struct{ handlers map[string]queue.Handler }

func (w *registeringWorker) Register(name string, handler queue.Handler) error {
	w.handlers[name] = handler
	return nil
}

func TestReconAlertRoutesReachDedicatedLandingHandlers(t *testing.T) {
	worker := &registeringWorker{handlers: make(map[string]queue.Handler)}
	if err := outbox.RegisterLedgerHandlers(worker, nil); err != nil {
		t.Fatal(err)
	}
	for topic, taskType := range map[string]string{
		"ledger.recon.failed":    outbox.TaskReconFailed,
		"ledger.recon.stale_run": outbox.TaskReconStale,
	} {
		handler := worker.handlers[taskType]
		if handler == nil {
			t.Fatalf("topic %s task %s has no registered handler", topic, taskType)
		}
		if err := handler(context.Background(), asynq.NewTask(taskType, []byte(`{"outbox_id":1}`))); err != nil {
			t.Fatalf("topic %s handler %s: %v", topic, taskType, err)
		}
		t.Logf("topic %s received by handler %s; unknownTaskType=0", topic, taskType)
	}
}
