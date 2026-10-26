package overload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time          { return c.now }
func (c *fakeClock) Advance(d time.Duration) { c.now = c.now.Add(d) }

func TestBrownoutHalvesHeavyAndNeverTouchesCritical(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	signals := Signals{P95Set: true}
	bulkhead := New(Config{Total: 64, Wait: time.Millisecond, Limits: Limits{Critical: 16, Auth: 4, Write: 4, Read: 4, Heavy: 8}, Timeouts: allTimeouts(time.Second)})
	brownout := NewBrownout(BrownoutConfig{
		BaseHeavy: 8, EscalateAfter: 5 * time.Second, RecoverAfter: 60 * time.Second,
		P95Threshold: 2 * time.Second, P95Recovery: time.Second,
		DBWaitThreshold: 100 * time.Millisecond, DBWaitRecovery: 50 * time.Millisecond,
		InFlightRatioThreshold: .8, InFlightRatioRecovery: .6,
	}, SignalFunc(func() Signals { return signals }), BrownoutOptions{
		Now:      clock.Now,
		OnChange: func(_ BrownoutLevel, limit int) { bulkhead.SetHeavyLimit(limit) },
	})

	if brownout.Level() != 0 || brownout.HeavyLimit() != 8 {
		t.Fatalf("initial brownout = level %d limit %d", brownout.Level(), brownout.HeavyLimit())
	}
	if bulkhead.Limit(Heavy) != 8 || bulkhead.Limit(Critical) != 16 {
		t.Fatalf("initial bulkhead = heavy %d critical %d", bulkhead.Limit(Heavy), bulkhead.Limit(Critical))
	}

	signals.P95 = 3 * time.Second
	for _, want := range []struct {
		level BrownoutLevel
		limit int
	}{{1, 4}, {2, 2}, {3, 1}} {
		brownout.Evaluate()
		clock.Advance(5 * time.Second)
		brownout.Evaluate()
		if brownout.Level() != want.level || brownout.HeavyLimit() != want.limit {
			t.Fatalf("after breach: level = %d limit = %d, want %d/%d", brownout.Level(), brownout.HeavyLimit(), want.level, want.limit)
		}
		if bulkhead.Limit(Heavy) != want.limit {
			t.Fatalf("bulkhead heavy limit = %d, want %d", bulkhead.Limit(Heavy), want.limit)
		}
		if bulkhead.Limit(Critical) != 16 {
			t.Fatalf("critical limit changed to %d", bulkhead.Limit(Critical))
		}
	}

	signals.P95 = 500 * time.Millisecond
	brownout.Evaluate()
	for _, want := range []struct {
		level BrownoutLevel
		limit int
	}{{2, 2}, {1, 4}, {0, 8}} {
		clock.Advance(60 * time.Second)
		brownout.Evaluate()
		if brownout.Level() != want.level || brownout.HeavyLimit() != want.limit {
			t.Fatalf("after recovery: level = %d limit = %d, want %d/%d", brownout.Level(), brownout.HeavyLimit(), want.level, want.limit)
		}
		if bulkhead.Limit(Heavy) != want.limit {
			t.Fatalf("bulkhead heavy limit = %d, want %d", bulkhead.Limit(Heavy), want.limit)
		}
	}
}

func TestBrownoutRequiresSustainedSignalsAndRecovery(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	signals := Signals{P95Set: true}
	brownout := NewBrownout(BrownoutConfig{
		BaseHeavy: 8, EscalateAfter: 5 * time.Second, RecoverAfter: 60 * time.Second,
		P95Threshold: 2 * time.Second, P95Recovery: time.Second,
		DBWaitThreshold: 100 * time.Millisecond, DBWaitRecovery: 50 * time.Millisecond,
		InFlightRatioThreshold: .8, InFlightRatioRecovery: .6,
	}, SignalFunc(func() Signals { return signals }), BrownoutOptions{Now: clock.Now})

	for i := 0; i < 120; i++ {
		if i%2 == 0 {
			signals.P95 = 3 * time.Second
		} else {
			signals.P95 = 1500 * time.Millisecond
		}
		brownout.Evaluate()
		clock.Advance(time.Second)
	}
	if brownout.Level() != 0 {
		t.Fatalf("oscillating signals changed level to %d", brownout.Level())
	}

	signals.P95 = 3 * time.Second
	for i := 0; i < 6; i++ {
		brownout.Evaluate()
		clock.Advance(time.Second)
	}
	if brownout.Level() != 1 {
		t.Fatalf("level after sustained breach = %d, want 1", brownout.Level())
	}

	for i := 0; i < 120; i++ {
		if i%2 == 0 {
			signals.P95 = 3 * time.Second
		} else {
			signals.P95 = 1500 * time.Millisecond
		}
		brownout.Evaluate()
		clock.Advance(time.Second)
	}
	if brownout.Level() != 1 {
		t.Fatalf("oscillating signals changed level to %d", brownout.Level())
	}

	signals.P95 = 500 * time.Millisecond
	for i := 0; i < 59; i++ {
		brownout.Evaluate()
		clock.Advance(time.Second)
	}
	brownout.Evaluate()
	if brownout.Level() != 1 {
		t.Fatalf("level recovered before 60 seconds: %d", brownout.Level())
	}
	clock.Advance(time.Second)
	brownout.Evaluate()
	if brownout.Level() != 0 {
		t.Fatalf("level after 60 seconds = %d, want 0", brownout.Level())
	}
}

func TestLevelIsVisibleThroughContext(t *testing.T) {
	if got := Level(context.Background()); got != 0 {
		t.Fatalf("background level = %d, want 0", got)
	}
	if got := Level(WithLevel(context.Background(), 2)); got != 2 {
		t.Fatalf("injected level = %d, want 2", got)
	}

	var seen BrownoutLevel
	handler := LevelMiddleware(func() BrownoutLevel { return 1 })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = Level(r.Context())
		w.WriteHeader(http.StatusNoContent)
	}))
	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if seen != 1 {
		t.Fatalf("middleware level = %d, want 1", seen)
	}
}

func TestRecorderUsesRollingWindow(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	recorder := newRecorder(30*time.Second, clock.Now)
	for i := 1; i <= 100; i++ {
		recorder.Record(time.Duration(i) * time.Millisecond)
		clock.Advance(time.Millisecond)
	}
	if got := recorder.P95(); got != 95*time.Millisecond {
		t.Fatalf("p95 = %s, want 95ms", got)
	}
	clock.Advance(31 * time.Second)
	if got := recorder.P95(); got != 0 {
		t.Fatalf("p95 after window = %s, want 0", got)
	}
}

func TestInjectedZeroP95IsNotOverridden(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	brownout := NewBrownout(BrownoutConfig{
		BaseHeavy: 8, EscalateAfter: 5 * time.Second, RecoverAfter: 60 * time.Second,
		P95Threshold: 2 * time.Second, P95Recovery: time.Second,
		DBWaitThreshold: 100 * time.Millisecond, DBWaitRecovery: 50 * time.Millisecond,
		InFlightRatioThreshold: .8, InFlightRatioRecovery: .6,
	}, SignalFunc(func() Signals { return Signals{P95Set: true} }), BrownoutOptions{Now: clock.Now})

	brownout.Record(10 * time.Second)
	for i := 0; i < 10; i++ {
		brownout.Evaluate()
		clock.Advance(5 * time.Second)
	}
	if got := brownout.Level(); got != 0 {
		t.Fatalf("injected zero P95 was overridden, level = %d", got)
	}
}
