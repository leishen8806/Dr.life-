package observability

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf8"

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

func TestHTTPMetricsCollapseCustomMethodsToOther(t *testing.T) {
	metrics := NewMetrics()
	router := chi.NewRouter()
	router.Use(metrics.HTTPMiddleware)
	router.HandleFunc("/v1/public/ping", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	for i := 0; i < 1000; i++ {
		request := httptest.NewRequest("CUSTOM"+strings.Repeat("X", i+1), "/v1/public/ping", nil)
		router.ServeHTTP(httptest.NewRecorder(), request)
	}
	family := metricFamily(t, metrics, "drlife_http_requests_total")
	if len(family.Metric) != 1 {
		t.Fatalf("series = %d, want 1", len(family.Metric))
	}
	if got := labelValue(family.Metric[0], "method"); got != "OTHER" {
		t.Fatalf("method label = %q, want OTHER", got)
	}
}

func TestHTTPMetricLabelsUseFixedSets(t *testing.T) {
	for _, test := range []struct {
		input string
		want  string
	}{{http.MethodGet, http.MethodGet}, {"get", http.MethodGet}, {"TRACE", "OTHER"}, {"CUSTOM", "OTHER"}} {
		if got := methodLabel(test.input); got != test.want {
			t.Errorf("methodLabel(%q) = %q, want %q", test.input, got, test.want)
		}
	}
	for _, test := range []struct {
		input int
		want  string
	}{{100, "1xx"}, {204, "2xx"}, {302, "3xx"}, {403, "4xx"}, {503, "5xx"}, {999, "other"}} {
		if got := statusClass(test.input); got != test.want {
			t.Errorf("statusClass(%d) = %q, want %q", test.input, got, test.want)
		}
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
	b.WithdrawalReviewed()
	b.WithdrawalPaid()
	b.WithdrawalFailed()
	b.WithdrawalCancelled()
	b.WithdrawalOverdue(2)
	b.AIRequested()
	b.AIFallback("local")
	b.BotSent("buyer", 0, true, "")
	b.BotSent("merchant", 1, false, "network")
	b.LoginValidationFailed("driver", "expired")
	b.LoginAuditWriteFailed("driver")
	b.SessionRefresh("buyer", "success")
	b.LedgerPosted()
	b.LedgerFailed("unbalanced")
	b.ReconciliationMismatch()
	b.AuditAppend(true)
	b.AuditAppend(false)
	b.AuditVerifyFailure()
	b.AuditAnchorMissing(true)
	b.ReconRun(true)
	b.ReconRun(false)
	b.ReconMismatch("BALANCE_CACHE", 2)
	b.ReconSpotCheckDropped()
	for _, name := range []string{
		"drlife_orders_total", "drlife_withdrawals_total", "drlife_ai_requests_total", "drlife_bot_sends_total",
		"drlife_login_validation_failures_total", "drlife_login_audit_write_failures_total", "drlife_session_refresh_total", "drlife_ledger_posts_total", "drlife_ledger_reconciliation_runs_total", "drlife_ledger_reconciliation_mismatches_total", "drlife_ledger_spot_checks_dropped_total",
		"drlife_audit_appends_total", "drlife_audit_verify_failures_total", "drlife_audit_anchor_missing",
	} {
		if len(metricFamily(t, metrics, name).Metric) == 0 {
			t.Fatalf("%s has no samples", name)
		}
	}
}

func TestSlowQueryUsesFingerprintAndNeverLogsArguments(t *testing.T) {
	metrics := NewMetrics()
	var logs bytes.Buffer
	tracer := NewQueryTracer(time.Nanosecond, metrics, logx.NewLogger(slog.LevelInfo, &logs))
	template := "SELECT * FROM account WHERE phone = $1"
	ctx := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: template, Args: []any{"0123456789"}})
	time.Sleep(time.Microsecond)
	tracer.TraceQueryEnd(ctx, nil, pgx.TraceQueryEndData{})
	repeated := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: template, Args: []any{"second-secret-value"}})
	time.Sleep(time.Microsecond)
	tracer.TraceQueryEnd(repeated, nil, pgx.TraceQueryEndData{})
	if strings.Contains(logs.String(), "0123456789") || strings.Contains(logs.String(), "second-secret-value") || !strings.Contains(logs.String(), template) || strings.Count(logs.String(), "slow database query fingerprint registered") != 1 {
		t.Fatalf("unexpected query log: %s", logs.String())
	}
	family := metricFamily(t, metrics, "drlife_db_slow_queries_total")
	if len(family.Metric) != 1 || labelValue(family.Metric[0], "fingerprint") != statementFingerprint(template) || family.Metric[0].GetCounter().GetValue() != 2 {
		t.Fatalf("unexpected slow-query metric: %#v", family.Metric)
	}
}

func TestSlowQueryFingerprintLimitAndTemplateTruncation(t *testing.T) {
	metrics := NewMetrics()
	var logs bytes.Buffer
	tracer := newQueryTracer(time.Nanosecond, metrics, logx.NewLogger(slog.LevelInfo, &logs), 1)
	firstTemplate := "SELECT '" + strings.Repeat("ក", 600) + "' WHERE secret = $1"
	first := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: firstTemplate, Args: []any{"first-secret-value"}})
	time.Sleep(time.Microsecond)
	tracer.TraceQueryEnd(first, nil, pgx.TraceQueryEndData{})
	secondTemplate := "SELECT * FROM second_table WHERE secret = $1"
	second := tracer.TraceQueryStart(context.Background(), nil, pgx.TraceQueryStartData{SQL: secondTemplate, Args: []any{"second-secret-value"}})
	time.Sleep(time.Microsecond)
	tracer.TraceQueryEnd(second, nil, pgx.TraceQueryEndData{})

	if got := utf8.RuneCountInString(sqlTemplate(firstTemplate)); got != maxSQLTemplateRunes {
		t.Fatalf("template length = %d, want %d", got, maxSQLTemplateRunes)
	}
	if !strings.HasSuffix(sqlTemplate(firstTemplate), "...") {
		t.Fatalf("truncated template lacks ellipsis: %q", sqlTemplate(firstTemplate))
	}
	logText := logs.String()
	for _, secret := range []string{"first-secret-value", "second-secret-value"} {
		if strings.Contains(logText, secret) {
			t.Fatalf("query argument leaked: %s", logText)
		}
	}
	if strings.Contains(logText, secondTemplate) || strings.Contains(logText, statementFingerprint(secondTemplate)) {
		t.Fatalf("fingerprint beyond the bound was logged: %s", logText)
	}
	family := metricFamily(t, metrics, "drlife_db_slow_queries_total")
	if len(family.Metric) != 2 || metricValueForLabel(family, "fingerprint", statementFingerprint(firstTemplate)) != 1 || metricValueForLabel(family, "fingerprint", slowQueryOverflowLabel) != 1 {
		t.Fatalf("unexpected bounded slow-query metrics: %#v", family.Metric)
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
		Message: "call +855 12 345 678 with bot 123456:ABCdef_123 and Bearer header.payload.signature",
		Exception: []sentry.Exception{{
			Type: "Bearer type-secret", Value: "phone 0123456789 token 999:AlphaNum_1", Module: "Bearer module-secret",
		}},
		User: sentry.User{ID: "public-1", Email: "person@example.test", IPAddress: "203.0.113.10", Name: "Person", Data: map[string]string{"phone": "0123456789"}},
		Request: &sentry.Request{
			URL: "https://api.example.test/path?init_data=secret&safe=yes", QueryString: "token=secret&phone=0123456789&safe=yes", Data: "request-secret", Cookies: "session=secret",
			Headers: map[string]string{"Authorization": "Bearer secret", "Cookie": "session=secret", "Idempotency-Key": "idem-secret", "X-Safe": "visible"},
		},
	})
	event := transport.last(t)
	serialized := event.Request.URL + event.Request.QueryString + event.Request.Data + event.Request.Cookies + event.User.Email + event.User.IPAddress + event.User.Name + event.Message + event.Exception[0].Type + event.Exception[0].Value + event.Exception[0].Module
	for _, value := range []string{"secret", "0123456789", "+855 12 345 678", "123456:ABCdef_123", "999:AlphaNum_1", "header.payload.signature", "person@example.test", "203.0.113.10"} {
		if strings.Contains(strings.ToLower(serialized), strings.ToLower(value)) {
			t.Fatalf("event leaked %q: %#v", value, event)
		}
	}
	if event.User.ID != "public-1" || event.Request.Headers["X-Safe"] != "visible" || event.Request.QueryString != "safe=yes" || !strings.Contains(event.Message, "[REDACTED]") || !strings.Contains(event.Exception[0].Value, "[REDACTED]") {
		t.Fatalf("unexpected scrubbed event: %#v", event)
	}
}

func TestSentryScrubsAllFreeTextContainers(t *testing.T) {
	transport := &fakeTransport{}
	reporter, err := NewErrorReporter("https://public@example.invalid/1", "test", "release", transport)
	if err != nil {
		t.Fatal(err)
	}
	secrets := []string{"+855 12 345 678", "Bearer bearer-secret", "123456:BotSecret", "telegram-init-secret", "opaque-auth-secret"}
	reporter.hub.CaptureEvent(&sentry.Event{
		Message:     strings.Join(secrets[:3], " ") + " init_data=" + secrets[3] + " Authorization: Basic " + secrets[4],
		Fingerprint: []string{secrets[0]},
		Tags:        map[string]string{"authorization": secrets[1], "safe": secrets[2]},
		Contexts: map[string]sentry.Context{
			"request": {"init_data": secrets[3], "nested": []any{secrets[0], map[string]any{"token": secrets[2]}}},
		},
		Breadcrumbs: []*sentry.Breadcrumb{{
			Type: secrets[0], Category: secrets[1], Message: secrets[2],
			Data: map[string]any{"init_data": secrets[3], "phone": secrets[0]},
		}},
		Exception: []sentry.Exception{{
			Type: secrets[1], Value: secrets[0], Module: secrets[2],
			Stacktrace: &sentry.Stacktrace{Frames: []sentry.Frame{{ContextLine: secrets[1], Vars: map[string]any{"init_data": secrets[3]}}}},
			Mechanism:  &sentry.Mechanism{Type: "test", Description: secrets[0], Data: map[string]any{"authorization": secrets[1]}},
		}},
		Request: &sentry.Request{
			URL:         "https://api.example.test/" + secrets[2] + "?init_data=" + secrets[3] + "&safe=" + url.QueryEscape(secrets[0]),
			QueryString: "init_data=" + secrets[3] + "&safe=" + url.QueryEscape(secrets[1]),
			Headers:     map[string]string{"Authorization": secrets[1], "X-Debug": secrets[2]},
			Env:         map[string]string{"init_data": secrets[3], "SAFE": secrets[0]},
		},
	})
	event := transport.last(t)
	serialized := fmt.Sprintf("%#v", event)
	for _, secret := range secrets {
		if strings.Contains(serialized, secret) {
			t.Fatalf("event leaked %q: %s", secret, serialized)
		}
	}
	if event.Contexts["request"]["init_data"] != "[REDACTED]" || event.Tags["authorization"] != "[REDACTED]" || event.Breadcrumbs[0].Data["init_data"] != "[REDACTED]" {
		t.Fatalf("sensitive containers were not redacted: %#v", event)
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

func metricValueForLabel(family *dto.MetricFamily, labelName, labelValueWant string) float64 {
	for _, metric := range family.Metric {
		if labelValue(metric, labelName) == labelValueWant {
			return metric.GetCounter().GetValue()
		}
	}
	return 0
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
