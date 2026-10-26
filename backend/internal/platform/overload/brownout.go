package overload

import (
	"context"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/observability"
)

// BrownoutLevel is the current brownout severity. Zero means normal operation.
type BrownoutLevel int

const (
	brownoutNormal         BrownoutLevel = 0
	maxBrownoutLevel       BrownoutLevel = 3
	brownoutWindow                       = 30 * time.Second
	defaultEscalateAfter                 = 5 * time.Second
	defaultRecoverAfter                  = 60 * time.Second
	defaultP95Threshold                  = 2 * time.Second
	defaultP95Recovery                   = time.Second
	defaultDBWaitThreshold               = 100 * time.Millisecond
	defaultDBWaitRecovery                = 50 * time.Millisecond
)

// Signals are the health inputs sampled by the brownout controller.
type Signals struct {
	P95           time.Duration
	P95Set        bool
	DBWait        time.Duration
	InFlightRatio float64
}

// SignalFunc returns the latest health signals.
type SignalFunc func() Signals

// BrownoutConfig controls escalation and recovery. Recovery thresholds must be
// lower than their breach counterparts so ordinary noise cannot flap the level.
type BrownoutConfig struct {
	BaseHeavy              int
	EscalateAfter          time.Duration
	RecoverAfter           time.Duration
	P95Threshold           time.Duration
	P95Recovery            time.Duration
	DBWaitThreshold        time.Duration
	DBWaitRecovery         time.Duration
	InFlightRatioThreshold float64
	InFlightRatioRecovery  float64
}

// BrownoutOptions injects the clock and transition observers.
type BrownoutOptions struct {
	Now      func() time.Time
	OnChange func(BrownoutLevel, int)
	Logger   *slog.Logger
	Metrics  *observability.Metrics
}

// Brownout halves the heavy limit for each degraded level and restores it one
// level at a time after a sustained recovery window.
type Brownout struct {
	mu          sync.RWMutex
	config      BrownoutConfig
	source      SignalFunc
	now         func() time.Time
	onChange    func(BrownoutLevel, int)
	logger      *slog.Logger
	metrics     *observability.Metrics
	recorder    *recorder
	level       BrownoutLevel
	breachFrom  time.Time
	recoverFrom time.Time
}

// NewBrownout builds a controller. It starts in the normal level.
func NewBrownout(config BrownoutConfig, source SignalFunc, options BrownoutOptions) *Brownout {
	config = withBrownoutDefaults(config)
	now := options.Now
	if now == nil {
		now = time.Now
	}
	if source == nil {
		source = func() Signals { return Signals{} }
	}
	brownout := &Brownout{
		config:   config,
		source:   source,
		now:      now,
		onChange: options.OnChange,
		logger:   options.Logger,
		metrics:  options.Metrics,
		recorder: newRecorder(brownoutWindow, now),
	}
	if brownout.metrics != nil {
		brownout.metrics.BrownoutLevel(int(brownoutNormal), config.BaseHeavy)
	}
	return brownout
}

func withBrownoutDefaults(config BrownoutConfig) BrownoutConfig {
	if config.BaseHeavy < 1 {
		config.BaseHeavy = 1
	}
	if config.EscalateAfter <= 0 {
		config.EscalateAfter = defaultEscalateAfter
	}
	if config.RecoverAfter <= 0 {
		config.RecoverAfter = defaultRecoverAfter
	}
	if config.P95Threshold <= 0 {
		config.P95Threshold = defaultP95Threshold
	}
	if config.P95Recovery <= 0 {
		config.P95Recovery = defaultP95Recovery
	}
	if config.DBWaitThreshold <= 0 {
		config.DBWaitThreshold = defaultDBWaitThreshold
	}
	if config.DBWaitRecovery <= 0 {
		config.DBWaitRecovery = defaultDBWaitRecovery
	}
	if config.InFlightRatioThreshold <= 0 {
		config.InFlightRatioThreshold = 0.8
	}
	if config.InFlightRatioRecovery <= 0 {
		config.InFlightRatioRecovery = 0.6
	}
	return config
}

// Level returns the current severity.
func (b *Brownout) Level() BrownoutLevel {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.level
}

// HeavyLimit returns the heavy concurrency limit for the current level.
func (b *Brownout) HeavyLimit() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.heavyLimit(b.level)
}

// Record adds one request latency sample to the rolling P95 window.
func (b *Brownout) Record(elapsed time.Duration) {
	b.recorder.Record(elapsed)
}

// Run evaluates signals until the context is cancelled.
func (b *Brownout) Run(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		interval = time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.Evaluate()
		}
	}
}

// Evaluate advances the state machine using the current clock and signals.
func (b *Brownout) Evaluate() {
	now := b.now()
	signals := b.source()
	if !signals.P95Set {
		signals.P95 = b.recorder.P95()
	}
	b.mu.Lock()
	from := b.level
	switch {
	case b.breaching(signals):
		b.recoverFrom = time.Time{}
		if b.breachFrom.IsZero() {
			b.breachFrom = now
		}
		if b.level < maxBrownoutLevel && now.Sub(b.breachFrom) >= b.config.EscalateAfter {
			b.level++
			b.breachFrom = now
		}
	case b.recovered(signals):
		b.breachFrom = time.Time{}
		if b.level > brownoutNormal {
			if b.recoverFrom.IsZero() {
				b.recoverFrom = now
			}
			if now.Sub(b.recoverFrom) >= b.config.RecoverAfter {
				b.level--
				b.recoverFrom = now
			}
		}
	default:
		// The hysteresis band resets both timers so mixed signals cannot
		// accumulate an escalation or recovery window.
		b.breachFrom = time.Time{}
		b.recoverFrom = time.Time{}
	}
	to, limit := b.level, b.heavyLimit(b.level)
	b.mu.Unlock()

	if to != from {
		b.observe(from, to, limit)
	}
}

func (b *Brownout) breaching(signals Signals) bool {
	return b.config.P95Threshold > 0 && signals.P95 >= b.config.P95Threshold ||
		b.config.DBWaitThreshold > 0 && signals.DBWait >= b.config.DBWaitThreshold ||
		b.config.InFlightRatioThreshold > 0 && signals.InFlightRatio >= b.config.InFlightRatioThreshold
}

func (b *Brownout) recovered(signals Signals) bool {
	return signals.P95 <= b.config.P95Recovery &&
		signals.DBWait <= b.config.DBWaitRecovery &&
		signals.InFlightRatio <= b.config.InFlightRatioRecovery
}

func (b *Brownout) heavyLimit(level BrownoutLevel) int {
	limit := b.config.BaseHeavy
	for i := brownoutNormal; i < level && limit > 1; i++ {
		limit /= 2
	}
	if limit < 1 {
		return 1
	}
	return limit
}

func (b *Brownout) observe(from, to BrownoutLevel, limit int) {
	if b.onChange != nil {
		b.onChange(to, limit)
	}
	if b.logger != nil {
		b.logger.Info("brownout level changed", "from", int(from), "to", int(to), "heavy_limit", limit)
	}
	if b.metrics != nil {
		b.metrics.BrownoutLevel(int(to), limit)
		b.metrics.BrownoutTransition(int(from), int(to))
	}
}

type levelKey struct{}

// WithLevel attaches a brownout level to a context.
func WithLevel(ctx context.Context, level BrownoutLevel) context.Context {
	return context.WithValue(ctx, levelKey{}, level)
}

// Level reads the brownout level from a context. Missing values mean normal.
func Level(ctx context.Context) BrownoutLevel {
	level, _ := ctx.Value(levelKey{}).(BrownoutLevel)
	return level
}

// LevelMiddleware exposes the controller's current level to downstream code.
func LevelMiddleware(source func() BrownoutLevel) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if source != nil {
				r = r.WithContext(WithLevel(r.Context(), source()))
			}
			next.ServeHTTP(w, r)
		})
	}
}

type sample struct {
	at    time.Time
	value time.Duration
}

type recorder struct {
	mu      sync.Mutex
	window  time.Duration
	now     func() time.Time
	samples []sample
}

func newRecorder(window time.Duration, now func() time.Time) *recorder {
	if window <= 0 {
		window = brownoutWindow
	}
	if now == nil {
		now = time.Now
	}
	return &recorder{window: window, now: now}
}

func (r *recorder) Record(value time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	r.prune(now)
	r.samples = append(r.samples, sample{at: now, value: value})
}

func (r *recorder) P95() time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.prune(r.now())
	if len(r.samples) == 0 {
		return 0
	}
	values := make([]time.Duration, len(r.samples))
	for i, item := range r.samples {
		values[i] = item.value
	}
	sort.Slice(values, func(i, j int) bool { return values[i] < values[j] })
	rank := (95*len(values)+99)/100 - 1
	if rank < 0 {
		rank = 0
	}
	return values[rank]
}

func (r *recorder) prune(now time.Time) {
	cutoff := now.Add(-r.window)
	first := 0
	for first < len(r.samples) && !r.samples[first].at.After(cutoff) {
		first++
	}
	if first > 0 {
		r.samples = append(r.samples[:0], r.samples[first:]...)
	}
}
