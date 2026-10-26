package ledger

import (
	"context"
	"testing"
)

type spotCheckDropRecorder struct {
	dropped int
}

func (r *spotCheckDropRecorder) ReconRun(bool) {}

func (r *spotCheckDropRecorder) ReconMismatch(string, int) {}

func (r *spotCheckDropRecorder) ReconSpotCheckDropped() {
	r.dropped++
}

func TestSpotCheckRecordsDropWhenConcurrencyLimitIsFull(t *testing.T) {
	recorder := &spotCheckDropRecorder{}
	service := New(nil, Options{SpotCheckEvery: 1, SpotCheckConcurrency: 1, ReconciliationMetrics: recorder})
	service.spotCheckSem <- struct{}{}
	defer func() { <-service.spotCheckSem }()

	service.maybeSpotCheck([]Posting{{AccountID: 1}})

	if recorder.dropped != 1 {
		t.Fatalf("dropped=%d, want 1", recorder.dropped)
	}
	if err := service.StopSpotChecks(context.Background()); err != nil {
		t.Fatal(err)
	}
}
