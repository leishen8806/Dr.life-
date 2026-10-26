package ledger

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

type spotCheckDropRecorder struct {
	dropped int
	failed  int
}

func (r *spotCheckDropRecorder) ReconRun(ok bool) {
	if !ok {
		r.failed++
	}
}

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

type failingSpotCheckDB struct{}

func (failingSpotCheckDB) BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error) {
	return nil, errors.New("database unavailable")
}

func (failingSpotCheckDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
	return nil, errors.New("unavailable")
}

func (failingSpotCheckDB) QueryRow(context.Context, string, ...any) pgx.Row { return nil }

func TestSpotCheckFailureRecordsMetric(t *testing.T) {
	recorder := &spotCheckDropRecorder{}
	service := New(failingSpotCheckDB{}, Options{ReconciliationMetrics: recorder})

	if err := service.runSpotCheck(context.Background(), []int64{1}); err == nil {
		t.Fatal("runSpotCheck error = nil")
	}
	if recorder.failed != 1 {
		t.Fatalf("failed=%d, want 1", recorder.failed)
	}
}

func TestStopSpotChecksDrainsBeforeCancellation(t *testing.T) {
	service := New(nil, Options{})
	release := make(chan struct{})
	canceled := make(chan bool, 1)
	service.spotCheckWG.Add(1)
	go func() {
		defer service.spotCheckWG.Done()
		<-release
		select {
		case <-service.spotCheckCtx.Done():
			canceled <- true
		default:
			canceled <- false
		}
	}()

	stopped := make(chan error, 1)
	go func() { stopped <- service.StopSpotChecks(context.Background()) }()

	select {
	case <-service.spotCheckCtx.Done():
		t.Fatal("spot check canceled before in-flight work drained")
	case <-time.After(20 * time.Millisecond):
	}
	close(release)

	if err := <-stopped; err != nil {
		t.Fatal(err)
	}
	if <-canceled {
		t.Fatal("in-flight spot check was canceled")
	}
}
