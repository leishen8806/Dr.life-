package ledger_test

import "testing"

func TestClaimOutboxMarksReconciliationAlertWithoutTransaction(t *testing.T) {
	e := newBusinessEnv(t)
	var outboxID int64
	if err := e.admin.QueryRow(e.ctx, `
		INSERT INTO ledger.outbox(topic,payload)
		VALUES('ledger.recon.failed','{"run_id":1}'::jsonb)
		RETURNING id`).Scan(&outboxID); err != nil {
		t.Fatal(err)
	}

	batch, err := e.appService.ClaimOutbox(e.ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	events := batch.Events()
	if len(events) != 1 || events[0].ID != outboxID || events[0].Topic != "ledger.recon.failed" {
		t.Fatalf("claimed events=%+v", events)
	}
	if events[0].TxnID != nil {
		t.Fatalf("txn_id=%d, want nil", *events[0].TxnID)
	}
	if err := batch.MarkSent(e.ctx); err != nil {
		t.Fatal(err)
	}
	var sent bool
	if err := e.admin.QueryRow(e.ctx, `SELECT sent_at IS NOT NULL FROM ledger.outbox WHERE id=$1`, outboxID).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if !sent {
		t.Fatal("reconciliation alert was not marked sent")
	}
}
