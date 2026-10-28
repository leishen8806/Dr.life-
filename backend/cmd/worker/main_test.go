package main

import (
	"errors"
	"testing"
)

func TestRelayExitCodeIsNonZeroForFatalError(t *testing.T) {
	if code := relayExitCode(errors.New("missing ledger.outbox")); code == 0 {
		t.Fatalf("fatal relay error exit code=%d, want non-zero", code)
	}
	if code := relayExitCode(nil); code != 0 {
		t.Fatalf("clean shutdown exit code=%d, want 0", code)
	}
}
