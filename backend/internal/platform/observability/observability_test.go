package observability

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/logx"
	dto "github.com/prometheus/client_model/go"
)

func TestHTTPMetricsUseRoutePatternWithoutCardinalityGrowth(t *testing.T) {
	metrics := NewMetrics()
	router := chi.NewRouter()
	router.Use(metrics.HTTPMiddleware)
	router.Get("/v1/buyer/orders/{id}", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for i := 0; i < 1000; i++ {
		request := httptest.NewRequest(http.MethodGet, "/v1/buyer/orders/"+strings.Repeat("x", i%40+1), nil)
		router.ServeHTTP(httptest.NewRecorder(), request)
	}
	family := metricFamily(t, metrics, "drlife_http_requests_total")
	if len(family.Metric) != 1 {
		t.Fatalf("series = %d, want 1", len(family.Metric))
	}
	if got := labelValue(family.Metric[0], "route"); got != "/v1/buyer/orders/{id}" {
		t.Fatalf("route label = %q", got)
	}
}

func TestGatewayAndInternalMetrics(t *testing.T) {
	metrics := NewMetrics()
	router := chi.NewRouter()
	router.Use(metrics.HTTPMiddleware)
	router.Get("/v1/buyer/ai/chat", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTooManyRequests) })
	router.Get("/v1/admin/denied", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusForbidden) })
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/buyer/ai/chat", nil))
	router.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/v1/admin/denied", nil))
	if got := metricFamily(t, metrics, "drlife_gateway_rate_limit_hits_total").Metric[0].Counter.GetValue(); got != 1 {
		t.Fatalf("rate-limit counter = %v", got)
	}
	if got := metricFamily(t, metrics, "drlife_gateway_auth_failures_total").Metric[0].Counter.GetValue(); got != 1 {
		t.Fatalf("auth-failure counter = %v", got)
	}
	response := httptest.NewRecorder()
	MetricsHandler(metrics).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), "drlife_http_requests_total") {
		t.Fatalf("internal metrics status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestLogRedactionRegression(t *testing.T) {
	var logs bytes.Buffer
	logx.NewLogger(slog.LevelInfo, &logs).Info("external call", "phone", "0123456789", "bot_token", "secret-token", "result", "failed")
	if strings.Contains(logs.String(), "0123456789") || strings.Contains(logs.String(), "secret-token") || !strings.Contains(logs.String(), "[REDACTED]") {
		t.Fatalf("sensitive log fields were not redacted: %s", logs.String())
	}
}

func TestBusinessMetricMethods(t *testing.T) {
	metrics := NewMetrics()
	b := metrics.Business()
	b.OrderCreated()
	b.OrderSigned()
	b.WithdrawalRequested()
	b.WithdrawalPaid()
	b.AIRequested()
	b.AIFallback("local")
	b.BotSent("buyer", 0, true, "")
	b.BotSent("merchant", 1, false, "network")
	b.LoginValidationFailed("driver", "expired")
	b.LedgerPosted()
	b.LedgerFailed("unbalanced")
	b.ReconciliationMismatch()
	for _, name := range []string{
		"drlife_orders_total", "drlife_withdrawals_total", "drlife_ai_requests_total", "drlife_bot_sends_total",
		"drlife_login_validation_failures_total", "drlife_ledger_posts_total", "drlife_ledger_reconciliation_mismatches_total",
	} {
		if len(metricFamily(t, metrics, name).Metric) == 0 {
			t.Fatalf("%s has no samples", name)
		}
	}
}

func TestSlowQueryUsesFingerprintAndNeverLogsArguments(t *testing.T) {
	metrics := NewMetrics()
	var logs bytes.Buffer
	tracer := QueryTracer{Threshold: time.Nanosecond, Metrics: metrics, Logger: logx.NewLogger(slog.LevelInfo, &logs)}
	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: "SELECT * FROM account WHERE phone = $1", Args: []any{"0123456789"}})
	time.Sleep(time.Microsecond)
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	if strings.Contains(logs.String(), "0123456789") || strings.Contains(logs.String(), "SELECT *") {
		t.Fatalf("query details leaked: %s", logs.String())
	}
	family := metricFamily(t, metrics, "drlife_db_slow_queries_total")
	if len(family.Metric) != 1 || labelValue(family.Metric[0], "fingerprint") != statementFingerprint("SELECT * FROM account WHERE phone = $1") {
		t.Fatalf("unexpected slow-query metric: %#v", family.Metric)
	}
}

func TestSentryDisabledWithoutDSNAndScrubsEvent(t *testing.T) {
	disabledTransport := &fakeTransport{}
	disabled, err := NewErrorReporter("", "test", "release", disabledTransport)
	if err != nil || disabled.Enabled() {
		t.Fatalf("disabled reporter: enabled=%v err=%v", disabled.Enabled(), err)
	}
	disabled.Capture(context.Background(), errors.New("ignored"))
	if disabledTransport.configured || len(disabledTransport.events) != 0 {
		t.Fatal("empty DSN configured or sent through transport")
	}

	transport := &fakeTransport{}
	reporter, err := NewErrorReporter("https://public@example.invalid/1", "test", "release", transport)
	if err != nil {
		t.Fatal(err)
	}
	reporter.hub.CaptureEvent(&sentry.Event{
		User:  sentry.User{ID: "public-1", Email: "person@example.test", IPAddress: "203.0.113.10", Name: "Person", Data: map[string]string{"phone": "0123456789"}},
		Extra: map[string]any{"phone": "0123456789"},
		Request: &sentry.Request{
			URL: "https://api.example.test/path?init_data=secret&safe=yes", QueryString: "token=secret&phone=0123456789&safe=yes", Data: "request-secret", Cookies: "session=secret",
			Headers: map[string]string{"Authorization": "Bearer secret", "Cookie": "session=secret", "Idempotency-Key": "idem-secret", "X-Safe": "visible"},
		},
	})
	event := transport.last(t)
	serialized := event.Request.URL + event.Request.QueryString + event.Request.Data + event.Request.Cookies + event.User.Email + event.User.IPAddress + event.User.Name
	for _, value := range []string{"secret", "0123456789", "person@example.test", "203.0.113.10"} {
		if strings.Contains(strings.ToLower(serialized), strings.ToLower(value)) {
			t.Fatalf("event leaked %q: %#v", value, event)
		}
	}
	if event.User.ID != "public-1" || event.Request.Headers["X-Safe"] != "visible" || event.Request.QueryString != "safe=yes" || event.Extra != nil {
		t.Fatalf("unexpected scrubbed event: %#v", event)
	}
}

func TestPanicHasSameRequestIDInResponseLogAndSentry(t *testing.T) {
	transport := &fakeTransport{}
	reporter, err := NewErrorReporter("https://public@example.invalid/1", "test", "release", transport)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	handler := httpx.RequestID(Recover(logx.NewLogger(slog.LevelInfo, &logs), reporter)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("must-not-leak")
	})))
	request := httptest.NewRequest(http.MethodGet, "/panic", nil)
	request.Header.Set("X-Request-ID", "request-safe-1")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusInternalServerError || !strings.Contains(response.Body.String(), `"request_id":"request-safe-1"`) || !strings.Contains(logs.String(), `"request_id":"request-safe-1"`) {
		t.Fatalf("request ID missing: response=%s logs=%s", response.Body.String(), logs.String())
	}
	event := transport.last(t)
	if event.Tags["request_id"] != "request-safe-1" || strings.Contains(response.Body.String()+logs.String()+event.Message, "must-not-leak") {
		t.Fatalf("unsafe panic event: %#v", event)
	}
}

func BenchmarkHTTPMiddleware(b *testing.B) {
	metrics := NewMetrics()
	router := chi.NewRouter()
	router.Use(metrics.HTTPMiddleware)
	router.Get("/v1/public/ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		router.ServeHTTP(httptest.NewRecorder(), request)
	}
}

func BenchmarkHTTPWithoutMetrics(b *testing.B) {
	router := chi.NewRouter()
	router.Get("/v1/public/ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		router.ServeHTTP(httptest.NewRecorder(), request)
	}
}

func metricFamily(t *testing.T, metrics *Metrics, name string) *dto.MetricFamily {
	t.Helper()
	families, err := metrics.registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, family := range families {
		if family.GetName() == name {
			return family
		}
	}
	t.Fatalf("metric family %s missing", name)
	return nil
}

func labelValue(metric *dto.Metric, name string) string {
	for _, label := range metric.Label {
		if label.GetName() == name {
			return label.GetValue()
		}
	}
	return ""
}

type fakeTransport struct {
	mu         sync.Mutex
	configured bool
	events     []*sentry.Event
}

func (f *fakeTransport) Configure(sentry.ClientOptions) { f.configured = true }
func (f *fakeTransport) SendEvent(event *sentry.Event) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.events = append(f.events, event)
}
func (f *fakeTransport) Flush(time.Duration) bool              { return true }
func (f *fakeTransport) FlushWithContext(context.Context) bool { return true }
func (f *fakeTransport) Close()                                {}
func (f *fakeTransport) last(t *testing.T) *sentry.Event {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.events) == 0 {
		t.Fatal("no Sentry event captured")
	}
	return f.events[len(f.events)-1]
}
