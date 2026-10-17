package ledger_test

import (
	"testing"
	"time"
)

func TestOldestUnsentOutboxAgeIntegration(t *testing.T) {
	e := newBusinessEnv(t)

	t.Run("empty outbox has no lag", func(t *testing.T) {
		age, err := e.appService.OldestUnsentOutboxAge(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if age != 0 {
			t.Fatalf("age=%s, want 0", age)
		}
	})

	t.Run("uses the oldest unsent event", func(t *testing.T) {
		oldest := time.Now().Add(-6 * time.Minute)
		if _, err := e.admin.Exec(e.ctx, `
			INSERT INTO ledger.outbox(topic,payload,created_at)
			VALUES ('ledger.test.oldest','{}',$1), ('ledger.test.newer','{}',$2)`, oldest, oldest.Add(2*time.Minute)); err != nil {
			t.Fatal(err)
		}
		age, err := e.appService.OldestUnsentOutboxAge(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if age < 5*time.Minute || age > 7*time.Minute {
			t.Fatalf("age=%s, want approximately 6m", age)
		}
	})

	t.Run("sent events do not contribute lag", func(t *testing.T) {
		if _, err := e.admin.Exec(e.ctx, `UPDATE ledger.outbox SET sent_at=now() WHERE sent_at IS NULL`); err != nil {
			t.Fatal(err)
		}
		age, err := e.appService.OldestUnsentOutboxAge(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if age != 0 {
			t.Fatalf("age=%s, want 0", age)
		}
	})
}
