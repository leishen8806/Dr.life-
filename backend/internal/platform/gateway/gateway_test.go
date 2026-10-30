package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/logx"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type bearerAuthenticator struct{}

func (bearerAuthenticator) Authenticate(r *http.Request) (Identity, bool, error) {
	value := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	role := Role(value)
	if !validRole(role) {
		return Identity{}, false, nil
	}
	return Identity{AccountID: "account-" + value, Role: role}, true, nil
}

type accountAuthenticator struct{}

func (accountAuthenticator) Authenticate(r *http.Request) (Identity, bool, error) {
	return Identity{AccountID: r.Header.Get("X-Test-Account"), Role: RoleBuyer}, true, nil
}

func TestPathRoleGuard(t *testing.T) {
	roles := []Role{RoleBuyer, RoleMerchant, RoleDriver, RoleWMS, RoleAdmin}
	for _, pathRole := range roles {
		for _, tokenRole := range roles {
			request := httptest.NewRequest(http.MethodGet, "/v1/"+string(pathRole)+"/resource", nil)
			request.Header.Set("Authorization", "Bearer "+string(tokenRole))
			response := httptest.NewRecorder()
			accessHandler().ServeHTTP(response, request)
			want := http.StatusForbidden
			if pathRole == tokenRole {
				want = http.StatusNoContent
			}
			if response.Code != want {
				t.Errorf("path role %s token role %s: status=%d body=%s", pathRole, tokenRole, response.Code, response.Body.String())
			}
		}
	}

	for _, test := range []struct {
		path   string
		header string
		status int
		code   httpx.ErrorCode
	}{
		{path: "/v1/buyer/resource", status: http.StatusUnauthorized, code: httpx.CodeUnauthenticated},
		{path: "/v1/buyer/resource", header: "Bearer invalid", status: http.StatusUnauthorized, code: httpx.CodeUnauthenticated},
		{path: "/v1/public/ping", status: http.StatusNoContent},
		{path: "/healthz", status: http.StatusNoContent},
		{path: "/readyz", status: http.StatusNoContent},
		{path: "/v1/foo/x", status: http.StatusForbidden, code: httpx.CodePermissionDenied},
	} {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("Authorization", test.header)
		response := httptest.NewRecorder()
		accessHandler().ServeHTTP(response, request)
		assertStatusCode(t, response, test.status, test.code)
	}
}

func TestAuthenticationFailuresHaveOneExternalShape(t *testing.T) {
	handler := httpx.RequestID(Authenticate(bearerAuthenticator{}, "")(Authorize("")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))))
	var first string
	for _, authorization := range []string{"", "invalid", "Bearer invalid", "Bearer expired"} {
		request := httptest.NewRequest(http.MethodGet, "/v1/buyer/x", nil)
		request.Header.Set("Authorization", authorization)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusUnauthorized || response.Header().Get("WWW-Authenticate") != "Bearer" {
			t.Fatalf("authorization=%q status=%d headers=%v", authorization, response.Code, response.Header())
		}
		var body map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		delete(body, "request_id")
		normalized, _ := json.Marshal(body)
		if first == "" {
			first = string(normalized)
		} else if string(normalized) != first {
			t.Fatalf("authentication failures differ: %s != %s", normalized, first)
		}
	}
}

func TestNonV1PathsFailClosedUnlessConfiguredAsWebhook(t *testing.T) {
	for _, path := range []string{"/metrics", "/debug/pprof", "/admin", "/internal/x"} {
		response := httptest.NewRecorder()
		accessHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
		assertStatusCode(t, response, http.StatusForbidden, httpx.CodePermissionDenied)
	}
	webhook := httpx.RequestID(Authenticate(nil, "/webhooks/telegram")(Authorize("/webhooks/telegram")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))))
	for path, want := range map[string]int{"/webhooks/telegram/update": http.StatusNoContent, "/webhooks/telegramx": http.StatusForbidden} {
		response := httptest.NewRecorder()
		webhook.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != want {
			t.Fatalf("path=%s status=%d want=%d body=%s", path, response.Code, want, response.Body.String())
		}
	}
}

func TestRouteFitEventEndpointIsTheOnlyPublicIntegrationRoute(t *testing.T) {
	handler := httpx.RequestID(Authenticate(nil, "")(Authorize("")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))))
	for path, want := range map[string]int{
		"/v1/integrations/routefit/events": http.StatusNoContent,
		"/v1/integrations/routefit":        http.StatusForbidden,
		"/v1/integrations/routefit/other":  http.StatusForbidden,
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, path, nil))
		if response.Code != want {
			t.Fatalf("path=%s status=%d want=%d", path, response.Code, want)
		}
	}
}

func TestPathBypassesAreRejected(t *testing.T) {
	for _, path := range []string{
		"/v1/public/../admin/x", "/v1/public//x", "/v1/Public/x",
		"/v1/public/%2e%2e/admin/x", "/v1/public%2fadmin/x",
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			accessHandler().ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
			assertStatusCode(t, response, http.StatusForbidden, httpx.CodePermissionDenied)
		})
	}
}

func TestCORSAllowlistAndPreflight(t *testing.T) {
	allowed := "https://miniapp.settlecore.org"
	handler := GroupedCORS(OriginGroups{Public: []string{allowed}})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, test := range []struct {
		name   string
		origin string
		status int
	}{
		{name: "no origin", status: http.StatusNoContent},
		{name: "allowed", origin: allowed, status: http.StatusNoContent},
		{name: "denied", origin: "https://evil.example", status: http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil)
			request.Header.Set("Origin", test.origin)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if response.Header().Get("Vary") != "Origin" {
				t.Fatalf("Vary=%q", response.Header().Get("Vary"))
			}
		})
	}

	request := httptest.NewRequest(http.MethodOptions, "/v1/public/ping", nil)
	request.Header.Set("Origin", allowed)
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Allow-Origin") != allowed || response.Header().Get("Access-Control-Allow-Headers") != allowedHeaders || response.Header().Get("Access-Control-Expose-Headers") != "X-Request-ID, Idempotent-Replayed" || response.Header().Get("Access-Control-Max-Age") == "" {
		t.Fatalf("bad preflight: status=%d headers=%v", response.Code, response.Header())
	}

	defaultHandler := GroupedCORS(OriginGroups{})(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	response = httptest.NewRecorder()
	request = httptest.NewRequest(http.MethodGet, "/", nil)
	request.Header.Set("Origin", allowed)
	defaultHandler.ServeHTTP(response, request)
	assertStatusCode(t, response, http.StatusForbidden, httpx.CodePermissionDenied)
}

func TestIdempotencyValidationAndExemption(t *testing.T) {
	handler := RequireIdempotency(func(r *http.Request) bool { return r.URL.Path == "/v1/public/login" })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Seen-Key", IdempotencyKeyFromContext(r.Context()))
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, test := range []struct {
		name, method, path, key string
		status                  int
	}{
		{name: "read", method: http.MethodGet, path: "/x", status: http.StatusNoContent},
		{name: "missing", method: http.MethodPost, path: "/x", status: http.StatusBadRequest},
		{name: "space", method: http.MethodPost, path: "/x", key: "bad key", status: http.StatusBadRequest},
		{name: "too long", method: http.MethodPost, path: "/x", key: strings.Repeat("a", 65), status: http.StatusBadRequest},
		{name: "valid", method: http.MethodPost, path: "/x", key: "order_123-retry", status: http.StatusNoContent},
		{name: "exempt", method: http.MethodPost, path: "/v1/public/login", status: http.StatusNoContent},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(test.method, test.path, nil)
			request.Header.Set("Idempotency-Key", test.key)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.status {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
			if test.name == "valid" && response.Header().Get("Seen-Key") != test.key {
				t.Fatal("valid key missing from context")
			}
		})
	}
}

func TestMemoryLimitAndTrustedProxy(t *testing.T) {
	resolver, err := NewClientIPResolver("CF-Connecting-IP", []string{"10.0.0.0/8"})
	if err != nil {
		t.Fatal(err)
	}
	rule := Rule{Limit: 1, Window: time.Minute}
	rules := RuleSet{Default: rule, Login: rule, Withdrawal: rule, AI: rule}
	handler := LimitByIP(NewMemoryLimiter(), resolver, rules)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil)
	request.RemoteAddr = "198.51.100.10:1234"
	request.Header.Set("CF-Connecting-IP", "203.0.113.1")
	handler.ServeHTTP(httptest.NewRecorder(), request)

	request = httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil)
	request.RemoteAddr = "198.51.100.10:5678"
	request.Header.Set("CF-Connecting-IP", "203.0.113.2")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusTooManyRequests || response.Header().Get("Retry-After") == "" {
		t.Fatalf("spoofed header bypassed limit: status=%d headers=%v", response.Code, response.Header())
	}

	for _, forwarded := range []string{"203.0.113.1", "203.0.113.2"} {
		request = httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil)
		request.RemoteAddr = "10.0.0.1:1234"
		request.Header.Set("CF-Connecting-IP", forwarded)
		response = httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusNoContent {
			t.Fatalf("trusted proxy client %s status=%d", forwarded, response.Code)
		}
	}
}

func TestIPv6AddressesSharePrefixLimit(t *testing.T) {
	rule := Rule{Limit: 1, Window: time.Minute}
	rules := RuleSet{Default: rule, Login: rule, Withdrawal: rule, AI: rule}
	handler := LimitByIP(NewMemoryLimiter(), ClientIPResolver{}, rules)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for i, remote := range []string{"[2001:db8:abcd:42::1]:1234", "[2001:db8:abcd:42::ffff]:5678"} {
		request := httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil)
		request.RemoteAddr = remote
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusNoContent
		if i == 1 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("remote=%s status=%d want=%d", remote, response.Code, want)
		}
	}
}

func TestHealthChecksAreNotRateLimited(t *testing.T) {
	rule := Rule{Limit: 1, Window: time.Minute}
	rules := RuleSet{Default: rule, Login: rule, Withdrawal: rule, AI: rule}
	handler := LimitByIP(NewMemoryLimiter(), ClientIPResolver{}, rules)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for i := 0; i < 3; i++ {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/healthz", nil))
		if response.Code != http.StatusNoContent {
			t.Fatalf("health request %d status=%d", i+1, response.Code)
		}
	}
}

func TestAccountLimitUsesSensitiveRuleAndAccountIdentity(t *testing.T) {
	rules := RuleSet{
		Default: Rule{Limit: 10, Window: time.Minute}, Login: Rule{Limit: 2, Window: time.Minute},
		Withdrawal: Rule{Limit: 1, Window: time.Minute}, AI: Rule{Limit: 1, Window: time.Minute},
	}
	handler := Authenticate(accountAuthenticator{}, "")(Authorize("")(LimitByAccount(NewMemoryLimiter(), rules)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))))
	for i, account := range []string{"one", "one", "two"} {
		request := httptest.NewRequest(http.MethodGet, "/v1/buyer/ai/chat", nil)
		request.Header.Set("X-Test-Account", account)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		want := http.StatusNoContent
		if i == 1 {
			want = http.StatusTooManyRequests
		}
		if response.Code != want {
			t.Fatalf("request %d account=%s status=%d want=%d", i+1, account, response.Code, want)
		}
	}
}

func TestSelectRuleMatchesWholePathSegments(t *testing.T) {
	rules := RuleSet{
		Default: Rule{Limit: 100, Window: time.Minute}, Login: Rule{Limit: 10, Window: time.Minute},
		Withdrawal: Rule{Limit: 5, Window: time.Minute}, AI: Rule{Limit: 30, Window: time.Minute}, Admin: Rule{Limit: 2, Window: time.Minute},
	}
	for _, test := range []struct {
		path, group string
		want        Rule
	}{
		{"/v1/buyer/airport-pickup", "v1/buyer", rules.Default},
		{"/v1/merchant/authors", "v1/merchant", rules.Default},
		{"/v1/merchant/orders/aids", "v1/merchant", rules.Default},
		{"/v1/buyer/ai/chat", "ai", rules.AI},
		{"/v1/public/auth/telegram", "login", rules.Login},
		{"/v1/buyer/withdrawals", "withdrawal", rules.Withdrawal},
		{"/v1/admin/users", "admin", rules.Admin},
		{"/v1/wms/stock", "admin", rules.Admin},
		{"/v1/admin/auth/login", "login", rules.Login},
	} {
		got, group := selectRule(test.path, rules)
		if got != test.want || group != test.group {
			t.Errorf("path=%s rule=%+v group=%s; want=%+v %s", test.path, got, group, test.want, test.group)
		}
	}
}

func TestMemoryLimiterRemovesExpiredCounters(t *testing.T) {
	now := time.Unix(100, 0)
	limiter := NewMemoryLimiter()
	limiter.now = func() time.Time { return now }
	rule := Rule{Limit: 1, Window: time.Second}
	if allowed, _, err := limiter.Allow(context.Background(), "old", rule); err != nil || !allowed {
		t.Fatal("initial request failed")
	}
	now = now.Add(2 * time.Minute)
	if allowed, _, err := limiter.Allow(context.Background(), "new", rule); err != nil || !allowed {
		t.Fatal("later request failed")
	}
	if _, exists := limiter.counters["old"]; exists {
		t.Fatal("expired counter was retained")
	}
}

type errorLimiter struct{}

func (errorLimiter) Allow(context.Context, string, Rule) (bool, time.Duration, error) {
	return false, 0, errors.New("redis unavailable")
}

func TestLimiterFallbackLogsAndContinues(t *testing.T) {
	var logs bytes.Buffer
	limiter := &FallbackLimiter{Primary: errorLimiter{}, Fallback: NewMemoryLimiter(), Logger: logx.NewLogger(slog.LevelInfo, &logs)}
	allowed, _, err := limiter.Allow(context.Background(), "ip:test", Rule{Limit: 1, Window: time.Minute})
	if err != nil || !allowed || !strings.Contains(logs.String(), "process-local fallback") || strings.Contains(logs.String(), "redis unavailable") {
		t.Fatalf("fallback result allowed=%v err=%v logs=%s", allowed, err, logs.String())
	}
}

func TestInvalidRedisURLDoesNotLeakCredentials(t *testing.T) {
	secret := "do-not-log-this"
	_, err := NewRedisLimiter("redis://:" + secret + "@%")
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("unsafe Redis URL error: %v", err)
	}
}

func TestRedisLimiterWithRealRedis(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := testcontainers.Run(ctx, "redis:7-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("6379/tcp")),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate Redis: %v", err)
		}
	})
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatal(err)
	}
	limiter, err := NewRedisLimiter("redis://" + host + ":" + port.Port() + "/0")
	if err != nil {
		t.Fatal(err)
	}
	defer limiter.Close()
	rule := Rule{Limit: 1, Window: time.Minute}
	if allowed, _, err := limiter.Allow(ctx, "test", rule); err != nil || !allowed {
		t.Fatalf("first allow=%v err=%v", allowed, err)
	}
	if allowed, retry, err := limiter.Allow(ctx, "test", rule); err != nil || allowed || retry <= 0 {
		t.Fatalf("second allow=%v retry=%v err=%v", allowed, retry, err)
	}
}

func TestAccessLogIncludesRoleWithoutToken(t *testing.T) {
	var logs bytes.Buffer
	logger := logx.NewLogger(slog.LevelInfo, &logs)
	handler := httpx.RequestLogger(logger, RoleFromContext)(Authenticate(bearerAuthenticator{}, "")(Authorize("")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))))
	request := httptest.NewRequest(http.MethodGet, "/v1/buyer/x", nil)
	request.Header.Set("Authorization", "Bearer buyer")
	handler.ServeHTTP(httptest.NewRecorder(), request)
	var entry map[string]any
	if err := json.Unmarshal(logs.Bytes(), &entry); err != nil {
		t.Fatal(err)
	}
	if entry["role"] != "buyer" || strings.Contains(logs.String(), "Bearer buyer") {
		t.Fatalf("unsafe access log: %s", logs.String())
	}
}

func accessHandler() http.Handler {
	return httpx.RequestID(Authenticate(bearerAuthenticator{}, "")(Authorize("")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))))
}

func assertStatusCode(t *testing.T, response *httptest.ResponseRecorder, status int, code httpx.ErrorCode) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status=%d want=%d body=%s", response.Code, status, response.Body.String())
	}
	if code == 0 {
		return
	}
	var body struct {
		Error struct {
			Code httpx.ErrorCode `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error.Code != code {
		t.Fatalf("code=%d want=%d body=%s", body.Error.Code, code, response.Body.String())
	}
}
