// Package overload protects independent route classes from exhausting shared capacity.
package overload

import (
	"context"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/observability"
)

type Class string

const (
	Critical Class = "critical"
	Auth     Class = "auth"
	Write    Class = "write"
	Read     Class = "read"
	Heavy    Class = "heavy"
	Stream   Class = "stream"
	Health   Class = "health"
)

var allClasses = []Class{Critical, Auth, Write, Read, Heavy, Stream}

type Limits struct{ Critical, Auth, Write, Read, Heavy, Stream int }

func (l Limits) forClass(c Class) int {
	switch c {
	case Critical:
		return l.Critical
	case Auth:
		return l.Auth
	case Write:
		return l.Write
	case Heavy:
		return l.Heavy
	case Stream:
		return l.Stream
	default:
		return l.Read
	}
}

type Timeouts struct{ Critical, Auth, Write, Read, Heavy time.Duration }

func (t Timeouts) forClass(c Class) time.Duration {
	switch c {
	case Critical:
		return t.Critical
	case Auth:
		return t.Auth
	case Write:
		return t.Write
	case Heavy:
		return t.Heavy
	default:
		return t.Read
	}
}

type Config struct {
	Total              int
	Wait               time.Duration
	Limits             Limits
	Timeouts           Timeouts
	Rules              map[string]Class
	CriticalPrefixes   []string
	DeadlineExemptions map[string]bool
	Metrics            *observability.Metrics
}

func DefaultConfig() Config {
	return Config{
		Total: 512, Wait: 100 * time.Millisecond,
		Limits:           Limits{Critical: 512, Auth: 64, Write: 128, Read: 256, Heavy: 32, Stream: 256},
		Timeouts:         Timeouts{Critical: 10 * time.Second, Auth: 5 * time.Second, Write: 10 * time.Second, Read: 5 * time.Second, Heavy: 30 * time.Second},
		CriticalPrefixes: []string{"/v1/buyer/withdraw*", "/v1/merchant/withdraw*", "/v1/driver/withdraw*", "/v1/ledger", "/v1/admin/finance"},
	}
}

type Bulkhead struct {
	total, nonCritical chan struct{}
	classes            map[Class]chan struct{}
	heavyMu            sync.Mutex
	heavyInFlight      int
	heavyMax           int
	heavyLimit         atomic.Int64
	heavyWake          chan struct{}
	config             Config
	draining           atomic.Bool
	brownout           atomic.Pointer[Brownout]
}

func New(config Config) *Bulkhead {
	if config.Total < 1 {
		config.Total = 512
	}
	if config.Wait <= 0 {
		config.Wait = 100 * time.Millisecond
	}
	defaults := DefaultConfig().Timeouts
	if config.Timeouts.Critical <= 0 {
		config.Timeouts.Critical = defaults.Critical
	}
	if config.Timeouts.Auth <= 0 {
		config.Timeouts.Auth = defaults.Auth
	}
	if config.Timeouts.Write <= 0 {
		config.Timeouts.Write = defaults.Write
	}
	if config.Timeouts.Read <= 0 {
		config.Timeouts.Read = defaults.Read
	}
	if config.Timeouts.Heavy <= 0 {
		config.Timeouts.Heavy = defaults.Heavy
	}
	if len(config.CriticalPrefixes) == 0 {
		config.CriticalPrefixes = DefaultConfig().CriticalPrefixes
	}
	b := &Bulkhead{total: make(chan struct{}, config.Total), nonCritical: make(chan struct{}, config.Total*3/4), classes: make(map[Class]chan struct{}), heavyWake: make(chan struct{}, 1), config: config}
	for _, c := range allClasses {
		if c == Heavy {
			continue
		}
		n := config.Limits.forClass(c)
		if n < 1 {
			n = 1
		}
		b.classes[c] = make(chan struct{}, n)
	}
	heavy := config.Limits.Heavy
	if heavy < 1 {
		heavy = 1
	}
	b.heavyMax = heavy
	b.heavyLimit.Store(int64(heavy))
	return b
}
func (b *Bulkhead) BeginDraining() { b.draining.Store(true) }
func (b *Bulkhead) Draining() bool { return b.draining.Load() }

// SetBrownout lets the bulkhead lower the heavy limit and expose the current level to handlers.
func (b *Bulkhead) SetBrownout(brownout *Brownout) { b.brownout.Store(brownout) }

// Limit reports the configured concurrency limit for a class.
func (b *Bulkhead) Limit(class Class) int {
	if class == Heavy {
		return int(b.heavyLimit.Load())
	}
	semaphore, ok := b.classes[class]
	if !ok {
		return 0
	}
	return cap(semaphore)
}

// SetHeavyLimit adjusts the heavy limit; critical classes are never affected.
func (b *Bulkhead) SetHeavyLimit(limit int) {
	if limit < 1 {
		limit = 1
	}
	if limit > b.heavyMax {
		limit = b.heavyMax
	}
	b.heavyLimit.Store(int64(limit))
	b.signalHeavy()
}

// InFlight reports the number of admitted requests.
func (b *Bulkhead) InFlight() int { return len(b.total) }

// InFlightRatio reports admitted requests as a fraction of the global limit.
func (b *Bulkhead) InFlightRatio() float64 {
	if cap(b.total) == 0 {
		return 0
	}
	return float64(len(b.total)) / float64(cap(b.total))
}

func (b *Bulkhead) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		class := b.Classify(r.URL.Path)
		if class == Health {
			next.ServeHTTP(w, r)
			return
		}
		started := time.Now()
		if b.Draining() {
			b.observe(class, started, false)
			reject(w, r)
			return
		}
		if !b.acquire(r.Context(), class) {
			reject(w, r)
			return
		}
		defer b.release(class)
		brownout := b.brownout.Load()
		if brownout != nil {
			defer func() { brownout.Record(time.Since(started)) }()
		}
		ctx, cancel := r.Context(), func() {}
		if class != Stream {
			ctx, cancel = context.WithTimeout(ctx, b.config.Timeouts.forClass(class))
		}
		defer cancel()
		if brownout != nil {
			ctx = WithLevel(ctx, brownout.Level())
		}
		captured := &captureWriter{ResponseWriter: w}
		next.ServeHTTP(captured, r.WithContext(ctx))
		if ctx.Err() != nil && !captured.wrote {
			reject(w, r)
		}
	})
}

// Classify picks the longest matching configured prefix or template, then documented defaults.
func (b *Bulkhead) Classify(path string) Class {
	if path == "/healthz" || path == "/readyz" {
		return Health
	}
	if b.deadlineExempt(path) {
		return Stream
	}
	best := 0
	picked := Class("")
	for prefix, class := range b.config.Rules {
		if matchesRule(path, prefix) && len(prefix) > best {
			best, picked = len(prefix), class
		}
	}
	if picked != "" {
		return picked
	}
	switch {
	case isAuthPath(path):
		return Auth
	case matchesCriticalPrefix(path, b.config.CriticalPrefixes):
		return Critical
	case hasHeavySegment(path):
		return Heavy
	case strings.HasPrefix(path, "/v1/public/ping") || strings.HasPrefix(path, "/v1/public/"):
		return Read
	default:
		return Write
	}
}

func matchesCriticalPrefix(path string, prefixes []string) bool {
	for _, configured := range prefixes {
		prefix, wildcard := strings.CutSuffix(configured, "*")
		if wildcard && strings.HasPrefix(path, prefix) {
			return true
		}
		if !wildcard && (path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/")) {
			return true
		}
	}
	return false
}

func hasHeavySegment(path string) bool {
	for _, segment := range strings.Split(strings.Trim(path, "/"), "/") {
		for _, prefix := range []string{"search", "ai", "export", "report"} {
			if segment == prefix || strings.HasPrefix(segment, prefix+"-") {
				return true
			}
		}
	}
	return false
}

func isAuthPath(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	return len(parts) >= 3 && parts[0] == "v1" && parts[2] == "auth"
}

func (b *Bulkhead) deadlineExempt(path string) bool {
	for prefix, exempt := range b.config.DeadlineExemptions {
		if exempt && isSSEPrefix(prefix) && (path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/")) {
			return true
		}
	}
	return false
}

func isSSEPrefix(path string) bool {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) != 3 || parts[0] != "v1" || parts[2] != "events" {
		return false
	}
	switch parts[1] {
	case "buyer", "merchant", "driver", "wms", "admin":
		return true
	default:
		return false
	}
}

func matchesRule(path, rule string) bool {
	if !strings.Contains(rule, "{") {
		return strings.HasPrefix(path, rule)
	}
	pathParts, ruleParts := strings.Split(strings.Trim(path, "/"), "/"), strings.Split(strings.Trim(rule, "/"), "/")
	if len(pathParts) != len(ruleParts) {
		return false
	}
	for i, part := range ruleParts {
		if strings.HasPrefix(part, "{") && strings.HasSuffix(part, "}") {
			continue
		}
		if pathParts[i] != part {
			return false
		}
	}
	return true
}

func (b *Bulkhead) acquire(ctx context.Context, class Class) bool {
	started := time.Now()
	timer := time.NewTimer(b.config.Wait)
	defer timer.Stop()
	needed := make([]chan struct{}, 0, 3)
	heavyHeld := false
	if class == Stream {
		needed = append(needed, b.classes[Stream])
	} else if class == Heavy {
		if !b.acquireHeavy(ctx, timer) {
			b.observe(class, started, false)
			return false
		}
		heavyHeld = true
	} else {
		needed = append(needed, b.classes[class])
	}
	if class != Stream {
		needed = append(needed, b.total)
		if class != Critical {
			needed = append(needed, b.nonCritical)
		}
	}
	held := make([]chan struct{}, 0, len(needed))
	for _, semaphore := range needed {
		select {
		case semaphore <- struct{}{}:
			held = append(held, semaphore)
		case <-ctx.Done():
			b.releaseN(held)
			if heavyHeld {
				b.releaseHeavy()
			}
			b.observe(class, started, false)
			return false
		case <-timer.C:
			b.releaseN(held)
			if heavyHeld {
				b.releaseHeavy()
			}
			b.observe(class, started, false)
			return false
		}
	}
	b.observe(class, started, true)
	return true
}

func (b *Bulkhead) acquireHeavy(ctx context.Context, timer *time.Timer) bool {
	for {
		b.heavyMu.Lock()
		limit := int(b.heavyLimit.Load())
		if b.heavyInFlight < limit {
			b.heavyInFlight++
			moreAvailable := b.heavyInFlight < limit
			b.heavyMu.Unlock()
			if moreAvailable {
				b.signalHeavy()
			}
			return true
		}
		b.heavyMu.Unlock()
		select {
		case <-ctx.Done():
			return false
		case <-timer.C:
			return false
		case <-b.heavyWake:
		}
	}
}

func (b *Bulkhead) releaseHeavy() {
	b.heavyMu.Lock()
	if b.heavyInFlight > 0 {
		b.heavyInFlight--
	}
	available := b.heavyInFlight < int(b.heavyLimit.Load())
	b.heavyMu.Unlock()
	if available {
		b.signalHeavy()
	}
}

func (b *Bulkhead) signalHeavy() {
	select {
	case b.heavyWake <- struct{}{}:
	default:
	}
}

func (b *Bulkhead) release(class Class) {
	semaphores := make([]chan struct{}, 0, 3)
	if class == Stream {
		semaphores = append(semaphores, b.classes[Stream])
	} else if class == Heavy {
		semaphores = append(semaphores, b.total)
		semaphores = append(semaphores, b.nonCritical)
	} else {
		semaphores = append(semaphores, b.total)
		semaphores = append(semaphores, b.classes[class])
	}
	if class != Critical && class != Heavy && class != Stream {
		semaphores = append(semaphores, b.nonCritical)
	}
	b.releaseN(semaphores)
	if class == Heavy {
		b.releaseHeavy()
	}
	if b.config.Metrics != nil {
		b.config.Metrics.BulkheadRelease(string(class))
	}
}
func (b *Bulkhead) releaseN(semaphores []chan struct{}) {
	for i := len(semaphores) - 1; i >= 0; i-- {
		select {
		case <-semaphores[i]:
		default:
		}
	}
}
func (b *Bulkhead) observe(class Class, started time.Time, admitted bool) {
	if b.config.Metrics != nil {
		b.config.Metrics.Bulkhead(string(class), time.Since(started), admitted)
	}
}
func reject(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Retry-After", strconvRetry())
	httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
}
func strconvRetry() string { return strconv.Itoa(1 + rand.Intn(3)) }

type captureWriter struct {
	http.ResponseWriter
	wrote bool
}

func (w *captureWriter) WriteHeader(status int) { w.wrote = true; w.ResponseWriter.WriteHeader(status) }
func (w *captureWriter) Write(body []byte) (int, error) {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

func (w *captureWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *captureWriter) Flush() {
	if !w.wrote {
		w.WriteHeader(http.StatusOK)
	}
	_ = http.NewResponseController(w.ResponseWriter).Flush()
}
