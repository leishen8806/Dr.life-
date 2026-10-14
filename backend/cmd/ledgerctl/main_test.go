package main

import (
	"bytes"
	"context"
	"errors"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
)

type fakeLedger struct {
	run ledger.ReconciliationRun
	err error
}

func (f fakeLedger) RunDailyReconciliation(context.Context, time.Time, ledger.ReconciliationOptions) (ledger.ReconciliationRun, error) {
	return f.run, f.err
}
func (fakeLedger) SnapshotDaily(context.Context, time.Time) (ledger.SnapshotResult, error) {
	return ledger.SnapshotResult{}, nil
}
func (fakeLedger) ListPayoutFreezes(context.Context, bool) ([]ledger.PayoutFreeze, error) {
	return nil, nil
}
func (fakeLedger) ReleasePayoutFreeze(context.Context, int64, int64, string) (ledger.PayoutFreeze, error) {
	return ledger.PayoutFreeze{}, nil
}

func TestParseCommand(t *testing.T) {
	valid := [][]string{
		{"recon", "--day", "2026-10-14"},
		{"recon", "--day", "2026-10-14", "--force", "--reason", "after repair"},
		{"snapshot", "--day", "2026-10-14"},
		{"freeze", "list"},
		{"freeze", "release", "--id", "1", "--by", "2", "--reason", "checked"},
	}
	for _, args := range valid {
		if _, err := parseCommand(args); err != nil {
			t.Fatalf("parse %v: %v", args, err)
		}
	}
	invalid := [][]string{
		nil, {"recon"}, {"recon", "--day", "bad"},
		{"recon", "--day", "2026-10-14", "--force"},
		{"snapshot", "--day", "2026-10-14", "--force"},
		{"freeze", "release", "--id", "0", "--by", "2", "--reason", "checked"},
	}
	for _, args := range invalid {
		if _, err := parseCommand(args); err == nil {
			t.Fatalf("parse %v unexpectedly succeeded", args)
		}
	}
}

func TestExecuteExitCodes(t *testing.T) {
	var stdout, stderr bytes.Buffer
	cmd := command{name: "recon", day: time.Now()}
	if got := execute(context.Background(), cmd, fakeLedger{run: ledger.ReconciliationRun{Status: "ok"}}, &stdout, &stderr); got != 0 {
		t.Fatalf("ok exit=%d stderr=%s", got, stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	if got := execute(context.Background(), cmd, fakeLedger{run: ledger.ReconciliationRun{Status: "failed"}}, &stdout, &stderr); got != 1 {
		t.Fatalf("failed reconciliation exit=%d", got)
	}
	if got := execute(context.Background(), cmd, fakeLedger{err: errors.New("database unavailable")}, &stdout, &stderr); got != 1 {
		t.Fatalf("operation error exit=%d", got)
	}
}
