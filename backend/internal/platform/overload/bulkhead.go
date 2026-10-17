// Package overload protects independent route classes from exhausting shared capacity.
package overload

import (
	"context"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
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
	Health   Class = "health"
)

var allClasses = []Class{Critical, Auth, Write, Read, Heavy}

type Limits struct{ Critical, Auth, Write, Read, Heavy int }

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
	Total    int
	Wait     time.Duration
	Limits   Limits
	Timeouts Timeouts
	Rules    map[string]Class
	Metrics  *observability.Metrics
}

func DefaultConfig() Config {
	return Config{Total: 512, Wait: 100 * time.Millisecond, Limits: Limits{Critical: 512, Auth: 64, Write: 128, Read: 256, Heavy: 32}, Timeouts: Timeouts{Critical: 10 * time.Second, Auth: 5 * time.Second, Write: 10 * time.Second, Read: 5 * time.Second, Heavy: 30 * time.Second}}
}

type Bulkhead struct {
	total, nonCritical chan struct{}
	classes            map[Class]chan struct{}
	config             Config
	draining           atomic.Bool
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
	b := &Bulkhead{total: make(chan struct{}, config.Total), nonCritical: make(chan struct{}, config.Total*3/4), classes: make(map[Class]chan struct{}), config: config}
	for _, c := range allClasses {
		n := config.Limits.forClass(c)
		if n < 1 {
			n = 1
		}
		b.classes[c] = make(chan struct{}, n)
	}
	return b
}
func (b *Bulkhead) BeginDraining() { b.draining.Store(true) }
func (b *Bulkhead) Draining() bool { return b.draining.Load() }
func (b *Bulkhead) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		class := b.Classify(r.URL.Path)
		if class == Health {
			next.ServeHTTP(w, r)
			return
		}
		if b.Draining() {
			b.observe(class, time.Now(), false)
			reject(w, r)
			return
		}
		if !b.acquire(r.Context(), class) {
			reject(w, r)
			return
		}
		defer b.release(class)
		ctx, cancel := context.WithTimeout(r.Context(), b.config.Timeouts.forClass(class))
		defer cancel()
		captured := &captureWriter{ResponseWriter: w}
		next.ServeHTTP(captured, r.WithContext(ctx))
		if ctx.Err() != nil && !captured.wrote {
			reject(w, r)
		}
	})
}

// Classify picks the longest matching configured prefix or template, then documented defaults.
func (b *Bulkhead) Classify(path string) Class {
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
	case path == "/healthz" || path == "/readyz":
		return Health
	case strings.HasPrefix(path, "/v1/public/auth/"):
		return Auth
	case strings.Contains(path, "withdraw") || strings.HasPrefix(path, "/v1/ledger") || strings.HasPrefix(path, "/v1/admin/finance"):
		return Critical
	case strings.Contains(path, "/search") || strings.Contains(path, "/ai") || strings.Contains(path, "/export") || strings.Contains(path, "/report"):
		return Heavy
	case strings.HasPrefix(path, "/v1/public/ping") || strings.HasPrefix(path, "/v1/public/"):
		return Read
	default:
		return Write
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
	needed := []chan struct{}{b.classes[class], b.total}
	if class != Critical {
		needed = append(needed, b.nonCritical)
	}
	held := make([]chan struct{}, 0, len(needed))
	for _, semaphore := range needed {
		select {
		case semaphore <- struct{}{}:
			held = append(held, semaphore)
		case <-ctx.Done():
			b.releaseN(held)
			b.observe(class, started, false)
			return false
		case <-timer.C:
			b.releaseN(held)
			b.observe(class, started, false)
			return false
		}
	}
	b.observe(class, started, true)
	return true
}
func (b *Bulkhead) release(class Class) {
	semaphores := []chan struct{}{b.classes[class], b.total}
	if class != Critical {
		semaphores = append(semaphores, b.nonCritical)
	}
	b.releaseN(semaphores)
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
