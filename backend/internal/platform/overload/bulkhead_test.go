package overload

import (
	"context"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/observability"
)

func TestClassesRemainIsolatedAtConcurrency(t *testing.T) {
	for _, class := range []Class{Critical, Auth, Write, Read, Heavy} {
		class := class
		t.Run(string(class), func(t *testing.T) {
			b := New(Config{Total: 12, Wait: 100 * time.Millisecond, Limits: Limits{Critical: 2, Auth: 2, Write: 2, Read: 2, Heavy: 2}, Timeouts: allTimeouts(time.Second)})
			block := make(chan struct{})
			started := make(chan struct{}, 2)
			var finished sync.WaitGroup
			finished.Add(1000)
			for range 1000 {
				go func() {
					defer finished.Done()
					b.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
						select {
						case started <- struct{}{}:
						default:
						}
						<-block
					})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, pathFor(class), nil))
				}()
			}
			for range 2 {
				select {
				case <-started:
				case <-time.After(time.Second):
					t.Fatal("requests did not start")
				}
			}
			response := httptest.NewRecorder()
			b.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(response, httptest.NewRequest(http.MethodGet, pathFor(other(class)), nil))
			close(block)
			if response.Code != http.StatusNoContent {
				t.Fatalf("other class status = %d", response.Code)
			}
			waitGroup(t, &finished)
		})
	}
}

func TestCriticalUsesReservedCapacity(t *testing.T) {
	b := New(Config{Total: 4, Wait: time.Millisecond, Limits: Limits{Critical: 4, Auth: 4, Write: 4, Read: 4, Heavy: 4}, Timeouts: allTimeouts(time.Second)})
	release := make(chan struct{})
	var started sync.WaitGroup
	started.Add(3)
	for _, path := range []string{"/v1/public/auth/x", "/v1/buyer/orders", "/v1/public/ping"} {
		go func(path string) {
			b.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { started.Done(); <-release })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, path, nil))
		}(path)
	}
	waitGroup(t, &started)
	response := httptest.NewRecorder()
	b.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/v1/buyer/withdraw", nil))
	close(release)
	if response.Code != http.StatusNoContent {
		t.Fatalf("critical status = %d", response.Code)
	}
}

func TestConfiguredTemplateClassifiesConcretePath(t *testing.T) {
	b := New(Config{Total: 4, Wait: time.Millisecond, Limits: Limits{Critical: 4, Auth: 4, Write: 4, Read: 4, Heavy: 4}, Timeouts: allTimeouts(time.Second), Rules: map[string]Class{"/v1/buyer/orders/{orderID}": Heavy}})
	if got := b.Classify("/v1/buyer/orders/order_123"); got != Heavy {
		t.Fatalf("template class = %q, want %q", got, Heavy)
	}
}

func TestDefaultAuthClassificationCoversEveryAudience(t *testing.T) {
	b := New(DefaultConfig())
	for _, path := range []string{
		"/v1/public/auth/telegram", "/v1/buyer/auth/refresh", "/v1/merchant/auth/session",
		"/v1/driver/auth/session", "/v1/admin/auth/login", "/v1/wms/auth/login",
	} {
		if got := b.Classify(path); got != Auth {
			t.Errorf("Classify(%q) = %q, want %q", path, got, Auth)
		}
	}
	for _, path := range []string{"/v1/merchant/authors", "/v1/admin/authorize"} {
		if got := b.Classify(path); got == Auth {
			t.Errorf("Classify(%q) unexpectedly returned auth", path)
		}
	}
}

func TestDefaultClassificationUsesExactSegmentsAndCriticalPrefixes(t *testing.T) {
	b := New(DefaultConfig())
	tests := []struct {
		path string
		want Class
	}{
		{"/v1/buyer/withdraw", Critical},
		{"/v1/merchant/withdrawals", Critical},
		{"/v1/driver/withdraw-request", Critical},
		{"/v1/ledger", Critical},
		{"/v1/ledger/transactions", Critical},
		{"/v1/admin/finance/reconcile", Critical},
		{"/v1/buyer/search", Heavy},
		{"/v1/buyer/search-suggestions", Heavy},
		{"/v1/merchant/ai-chat", Heavy},
		{"/v1/admin/export-csv", Heavy},
		{"/v1/wms/report-daily", Heavy},
		{"/anything/withdraw", Write},
		{"/v1/public/withdraw-x", Read},
		{"/v1/admin/withdraw", Write},
		{"/v1/buyer/air-conditioners", Write},
		{"/v1/public/air-conditioners", Read},
		{"/v1/public/searching", Read},
		{"/v1/public/reporting", Read},
		{"/v1/buyer/daily-report", Write},
	}
	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			if got := b.Classify(test.path); got != test.want {
				t.Fatalf("Classify(%q) = %q, want %q", test.path, got, test.want)
			}
		})
	}
}

func TestConfiguredCriticalPrefixesReplaceDefaults(t *testing.T) {
	b := New(Config{CriticalPrefixes: []string{"/v1/admin/payout*", "/v1/custom"}})
	for _, test := range []struct {
		path string
		want Class
	}{
		{"/v1/admin/payout-batch", Critical},
		{"/v1/custom/operation", Critical},
		{"/v1/customary", Write},
		{"/v1/buyer/withdraw", Write},
	} {
		if got := b.Classify(test.path); got != test.want {
			t.Errorf("Classify(%q) = %q, want %q", test.path, got, test.want)
		}
	}
}

func TestOverloadRejectsWithinWaitAndSetsRetryAfter(t *testing.T) {
	b := New(Config{Total: 4, Wait: 20 * time.Millisecond, Limits: Limits{Critical: 4, Auth: 4, Write: 4, Read: 1, Heavy: 4}, Timeouts: allTimeouts(time.Second)})
	release, started := make(chan struct{}), make(chan struct{})
	go b.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(started); <-release })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil))
	<-started
	began := time.Now()
	response := httptest.NewRecorder()
	b.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Fatal("handler ran") })).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil))
	close(release)
	if response.Code != http.StatusServiceUnavailable || time.Since(began) > 100*time.Millisecond || response.Header().Get("Retry-After") < "1" || response.Header().Get("Retry-After") > "3" || !containsCode(response.Body.String(), httpx.CodeServiceUnavailable) {
		t.Fatalf("unexpected overload response: status=%d headers=%v body=%s", response.Code, response.Header(), response.Body.String())
	}
}

func TestHealthIsUnlimitedAndDeadlineCancelsHandler(t *testing.T) {
	b := New(Config{Total: 2, Wait: time.Millisecond, Limits: Limits{Critical: 2, Auth: 2, Write: 2, Read: 2, Heavy: 2}, Timeouts: Timeouts{Critical: time.Second, Auth: time.Second, Write: time.Second, Read: 5 * time.Millisecond, Heavy: time.Second}})
	release, started := make(chan struct{}), make(chan struct{})
	go b.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(started); <-release })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil))
	<-started
	health := httptest.NewRecorder()
	b.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusNoContent {
		t.Fatalf("health status=%d", health.Code)
	}
	close(release)

	deadline := New(Config{Total: 4, Wait: time.Millisecond, Limits: Limits{Critical: 4, Auth: 4, Write: 4, Read: 4, Heavy: 4}, Timeouts: Timeouts{Critical: time.Second, Auth: time.Second, Write: time.Second, Read: 5 * time.Millisecond, Heavy: time.Second}})
	canceled := make(chan struct{})
	timeout := httptest.NewRecorder()
	deadline.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done(); close(canceled) })).ServeHTTP(timeout, httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil))
	select {
	case <-canceled:
	case <-time.After(time.Second):
		t.Fatal("handler did not observe cancellation")
	}
	if timeout.Code != http.StatusServiceUnavailable || !containsCode(timeout.Body.String(), httpx.CodeServiceUnavailable) {
		t.Fatalf("timeout status=%d body=%s", timeout.Code, timeout.Body.String())
	}
}

func TestSSEDeadlineExemptionAndResponseControllerFlush(t *testing.T) {
	config := Config{
		Total: 4, Wait: time.Millisecond,
		Limits:             Limits{Critical: 4, Auth: 4, Write: 4, Read: 4, Heavy: 4},
		Timeouts:           Timeouts{Critical: time.Second, Auth: time.Second, Write: 5 * time.Millisecond, Read: 5 * time.Millisecond, Heavy: time.Second},
		DeadlineExemptions: map[string]bool{"/v1/buyer/events": true},
	}
	b := New(config)
	response := httptest.NewRecorder()
	b.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(15 * time.Millisecond)
		if err := r.Context().Err(); err != nil {
			t.Fatalf("SSE context was cancelled: %v", err)
		}
		unwrapper, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok || unwrapper.Unwrap() != response {
			t.Fatal("captureWriter did not expose the underlying writer")
		}
		if err := http.NewResponseController(w).Flush(); err != nil {
			t.Fatalf("flush through captureWriter: %v", err)
		}
	})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/buyer/events", nil))
	if response.Code != http.StatusOK || !response.Flushed {
		t.Fatalf("SSE response status=%d flushed=%v", response.Code, response.Flushed)
	}
	if !b.deadlineExempt("/v1/buyer/events/stream") || b.deadlineExempt("/v1/buyer/events-other") || New(Config{DeadlineExemptions: map[string]bool{"/v1/buyer/orders": true}}).deadlineExempt("/v1/buyer/orders") {
		t.Fatal("deadline exemption matched a non-SSE route")
	}

	deadlineObserved := make(chan struct{})
	normal := httptest.NewRecorder()
	b.Middleware(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		close(deadlineObserved)
	})).ServeHTTP(normal, httptest.NewRequest(http.MethodGet, "/v1/buyer/orders", nil))
	select {
	case <-deadlineObserved:
	case <-time.After(time.Second):
		t.Fatal("ordinary route did not observe deadline")
	}
	if normal.Code != http.StatusServiceUnavailable {
		t.Fatalf("ordinary route status=%d, want 503", normal.Code)
	}
}

func TestStreamCapacityIsIndependentFromOrdinaryRequests(t *testing.T) {
	b := New(Config{
		Total: 4, Wait: 10 * time.Millisecond,
		Limits:             Limits{Critical: 4, Auth: 4, Write: 1, Read: 1, Heavy: 1, Stream: 1},
		Timeouts:           allTimeouts(time.Second),
		DeadlineExemptions: map[string]bool{"/v1/buyer/events": true},
	})
	if got := b.Classify("/v1/buyer/events"); got != Stream {
		t.Fatalf("SSE class = %q, want %q", got, Stream)
	}
	if got := b.Limit(Stream); got != 1 {
		t.Fatalf("stream limit = %d, want 1", got)
	}

	release, started, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	streamHandler := b.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		close(started)
		<-release
	}))
	go func() {
		defer close(done)
		streamHandler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/buyer/events", nil))
	}()
	<-started
	if got := b.InFlight(); got != 0 {
		t.Fatalf("stream request consumed total capacity: in_flight=%d", got)
	}

	full := httptest.NewRecorder()
	streamHandler.ServeHTTP(full, httptest.NewRequest(http.MethodGet, "/v1/buyer/events", nil))
	if full.Code != http.StatusServiceUnavailable {
		t.Fatalf("second stream status=%d, want 503", full.Code)
	}
	read := httptest.NewRecorder()
	b.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })).ServeHTTP(read, httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil))
	if read.Code != http.StatusNoContent {
		t.Fatalf("read status=%d while stream full, want 204", read.Code)
	}
	close(release)
	<-done
}

func TestRejectedRequestsDoNotAffectBrownoutP95(t *testing.T) {
	clock := &fakeClock{now: time.Unix(1_700_000_000, 0)}
	brownout := NewBrownout(BrownoutConfig{BaseHeavy: 1}, nil, BrownoutOptions{Now: clock.Now})
	brownout.Record(time.Millisecond)
	want := brownout.recorder.P95()
	metrics := observability.NewMetrics()
	b := New(Config{Total: 2, Wait: 40 * time.Millisecond, Limits: Limits{Critical: 2, Auth: 2, Write: 2, Read: 1, Heavy: 2}, Timeouts: allTimeouts(time.Second), Metrics: metrics})
	b.SetBrownout(brownout)
	release, started, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		b.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { close(started); <-release })).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil))
	}()
	<-started
	response := httptest.NewRecorder()
	handlerRan := false
	b.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { handlerRan = true })).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", response.Code)
	}
	if got := brownout.recorder.P95(); got != want {
		t.Fatalf("rejected request changed P95 from %s to %s", want, got)
	}
	metricsResponse := httptest.NewRecorder()
	observability.MetricsHandler(metrics).ServeHTTP(metricsResponse, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if !strings.Contains(metricsResponse.Body.String(), `drlife_bulkhead_rejections_total{class="read"} 1`) {
		t.Fatalf("rejected request was not counted separately:\n%s", metricsResponse.Body.String())
	}
	close(release)
	<-done
	if handlerRan {
		t.Fatal("rejected handler ran")
	}
}

func TestHeavyWaiterWakesWhenDynamicLimitIncreases(t *testing.T) {
	b := New(Config{Total: 4, Wait: 100 * time.Millisecond, Limits: Limits{Critical: 4, Auth: 4, Write: 4, Read: 4, Heavy: 2}, Timeouts: allTimeouts(time.Second)})
	b.SetHeavyLimit(1)
	if !b.acquire(context.Background(), Heavy) {
		t.Fatal("first heavy acquire failed")
	}
	acquired := make(chan bool, 1)
	go func() { acquired <- b.acquire(context.Background(), Heavy) }()
	deadline := time.Now().Add(time.Second)
	for len(b.heavyWake) != 0 && time.Now().Before(deadline) {
		runtime.Gosched()
	}
	if len(b.heavyWake) != 0 {
		t.Fatal("heavy waiter did not block on the notification channel")
	}
	b.SetHeavyLimit(2)
	select {
	case ok := <-acquired:
		if !ok {
			t.Fatal("heavy waiter was rejected")
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("heavy waiter was not notified after limit increase")
	}
	b.release(Heavy)
	b.release(Heavy)
}

func TestDrainingRejectsNewRequestsAndMetricsStayLowCardinality(t *testing.T) {
	metrics := observability.NewMetrics()
	b := New(Config{Total: 2, Wait: time.Millisecond, Limits: Limits{Critical: 2, Auth: 2, Write: 2, Read: 2, Heavy: 2}, Timeouts: allTimeouts(time.Second), Metrics: metrics})
	b.BeginDraining()
	response := httptest.NewRecorder()
	b.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil))
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("draining status=%d", response.Code)
	}
	for i := 0; i < 1000; i++ {
		b.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil))
	}
	if metrics.BulkheadSeries() > len(allClasses) {
		t.Fatalf("bulkhead metric cardinality = %d", metrics.BulkheadSeries())
	}
}

func BenchmarkMiddleware(b *testing.B) {
	bulkhead := New(DefaultConfig())
	handler := bulkhead.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	request := httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
}

func waitGroup(t *testing.T, group *sync.WaitGroup) {
	t.Helper()
	done := make(chan struct{})
	go func() { group.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("requests did not start")
	}
}
func pathFor(class Class) string {
	return map[Class]string{Critical: "/v1/buyer/withdraw", Auth: "/v1/public/auth/telegram", Write: "/v1/buyer/orders", Read: "/v1/public/ping", Heavy: "/v1/public/search"}[class]
}
func other(class Class) Class {
	for _, candidate := range []Class{Critical, Auth, Write, Read, Heavy} {
		if candidate != class {
			return candidate
		}
	}
	return Read
}
func allTimeouts(timeout time.Duration) Timeouts {
	return Timeouts{Critical: timeout, Auth: timeout, Write: timeout, Read: timeout, Heavy: timeout}
}
func containsCode(body string, code httpx.ErrorCode) bool {
	return strings.Contains(body, `"code":`+strconv.Itoa(int(code)))
}
