package overload

import (
	"net/http"
	"net/http/httptest"
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
			b := New(Config{Total: 12, Wait: time.Millisecond, Limits: Limits{Critical: 2, Auth: 2, Write: 2, Read: 2, Heavy: 2}, Timeouts: allTimeouts(time.Second)})
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
