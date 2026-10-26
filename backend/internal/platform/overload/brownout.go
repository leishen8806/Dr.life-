package overload

import (
	"context"
	"log/slog"
	"math"
	"math/bits"
	"net/http"
	"sync"
	"sync/atomic"
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

type recorder struct {
	now    func() time.Time
	slices []histogramSlice
}

const (
	histogramSubBuckets = 4
	histogramBuckets    = 1 + 63*histogramSubBuckets
)

type histogramSlice [histogramBuckets]atomic.Uint64

func newRecorder(window time.Duration, now func() time.Time) *recorder {
	if window <= 0 {
		window = brownoutWindow
	}
	if now == nil {
		now = time.Now
	}
	sliceCount := int((window + time.Second - 1) / time.Second)
	return &recorder{now: now, slices: make([]histogramSlice, sliceCount)}
}

func (r *recorder) Record(value time.Duration) {
	second := r.now().Unix()
	cell := &r.slices[positiveMod(second, int64(len(r.slices)))][durationBucket(value)]
	// Pack the second and count together so a reused slice can rotate without a lock.
	tag := uint64(uint32(second)) << 32
	for {
		current := cell.Load()
		if current>>32 != tag>>32 {
			if cell.CompareAndSwap(current, tag|1) {
				return
			}
			continue
		}
		if uint32(current) == math.MaxUint32 || cell.CompareAndSwap(current, current+1) {
			return
		}
	}
}

func (r *recorder) P95() time.Duration {
	now := r.now().Unix()
	counts := [histogramBuckets]uint64{}
	var total uint64
	for second := now - int64(len(r.slices)) + 1; second <= now; second++ {
		slice := &r.slices[positiveMod(second, int64(len(r.slices)))]
		tag := uint64(uint32(second))
		for bucket := range slice {
			value := slice[bucket].Load()
			if value>>32 == tag {
				count := uint64(uint32(value))
				counts[bucket] += count
				total += count
			}
		}
	}
	if total == 0 {
		return 0
	}
	rank := (95*total + 99) / 100
	var seen uint64
	for bucket, count := range counts {
		seen += count
		if seen >= rank {
			return bucketUpperBound(bucket)
		}
	}
	return time.Duration(math.MaxInt64)
}

func durationBucket(value time.Duration) int {
	if value <= 0 {
		return 0
	}
	nanoseconds := uint64(value)
	exponent := bits.Len64(nanoseconds) - 1
	base := uint64(1) << exponent
	subBucket := int(((nanoseconds - base) * histogramSubBuckets) / base)
	if subBucket >= histogramSubBuckets {
		subBucket = histogramSubBuckets - 1
	}
	return 1 + exponent*histogramSubBuckets + subBucket
}

func bucketUpperBound(bucket int) time.Duration {
	if bucket <= 0 {
		return 0
	}
	bucket--
	exponent, subBucket := bucket/histogramSubBuckets, bucket%histogramSubBuckets
	base := uint64(1) << exponent
	width := base / histogramSubBuckets
	if width == 0 {
		width = 1
	}
	upper := base + width*uint64(subBucket+1) - 1
	if upper > math.MaxInt64 {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(upper)
}

func positiveMod(value, divisor int64) int {
	result := value % divisor
	if result < 0 {
		result += divisor
	}
	return int(result)
}
