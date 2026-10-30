package server

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/overload"
)

func TestHealthAndReadiness(t *testing.T) {
	for _, test := range []struct {
		name    string
		handler http.Handler
		path    string
		status  int
		body    string
	}{
		{"health", NewHandler(nil), "/healthz", http.StatusOK, `{"ok":true}`},
		{"ready without checks", NewHandler(nil), "/readyz", http.StatusOK, ""},
		{"not ready", NewHandler(nil, func(context.Context) error { return context.DeadlineExceeded }), "/readyz", http.StatusServiceUnavailable, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, test.path, nil)
			w := httptest.NewRecorder()
			test.handler.ServeHTTP(w, r)
			if w.Code != test.status {
				t.Fatalf("status = %d, want %d", w.Code, test.status)
			}
			if test.body != "" && strings.TrimSpace(w.Body.String()) != test.body {
				t.Fatalf("body = %q, want %q", w.Body.String(), test.body)
			}
			if test.path == "/readyz" && !strings.Contains(w.Body.String(), `"request_id"`) {
				t.Fatalf("ready response missing request_id: %s", w.Body.String())
			}
			if test.name == "not ready" {
				if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
					t.Fatalf("Content-Type = %q", got)
				}
				var response struct {
					Error struct {
						Code    int               `json:"code"`
						Message map[string]string `json:"message"`
					} `json:"error"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &response); err != nil {
					t.Fatal(err)
				}
				if response.Error.Code != int(httpx.CodeServiceUnavailable) {
					t.Fatalf("error code = %d", response.Error.Code)
				}
				for _, language := range []string{"zh", "en", "km"} {
					if response.Error.Message[language] == "" {
						t.Errorf("missing %s message: %#v", language, response.Error.Message)
					}
				}
			}
		})
	}
}

func TestAuditEndpointRequiresAudViewPermission(t *testing.T) {
	rule := gateway.Rule{Limit: 1000, Window: time.Minute}
	newHandler := func(permissions map[string]string) http.Handler {
		return NewHandlerWithGateway(nil, GatewayOptions{
			AppEnv: "dev", OriginGroups: gateway.DefaultOriginGroups("dev"),
			Limiter: gateway.NewMemoryLimiter(), Authenticator: fixedAdminAuthenticator{permissions: permissions},
			IPRules: gateway.RuleSet{Default: rule, Admin: rule}, AccountRules: gateway.RuleSet{Default: rule, Admin: rule},
		})
	}
	request := func(handler http.Handler) int {
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/v1/admin/audit", nil))
		return recorder.Code
	}
	if got := request(newHandler(nil)); got != http.StatusForbidden {
		t.Fatalf("without aud permission status=%d", got)
	}
	if got := request(newHandler(map[string]string{"aud": "view"})); got != http.StatusServiceUnavailable {
		t.Fatalf("with aud view status=%d", got)
	}
}

type fixedAdminAuthenticator struct{ permissions map[string]string }

func (a fixedAdminAuthenticator) Authenticate(*http.Request) (gateway.Identity, bool, error) {
	return gateway.Identity{AccountID: "admin_test", Role: gateway.RoleAdmin, Permissions: a.permissions}, true, nil
}

func TestMetricsAreNotExposedOnPublicHandler(t *testing.T) {
	request := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	response := httptest.NewRecorder()
	NewHandler(nil).ServeHTTP(response, request)
	if response.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", response.Code, response.Body.String())
	}
}

func TestHTTPServerSecurityLimits(t *testing.T) {
	srv := NewHTTPServer(":0", NewHandler(nil), time.Second, 2*time.Second, 3*time.Second)
	if srv.MaxHeaderBytes != 16<<10 || srv.ReadHeaderTimeout != time.Second || srv.ReadTimeout != 2*time.Second || srv.IdleTimeout != 3*time.Second || srv.WriteTimeout != 0 {
		t.Fatalf("unexpected server limits: %+v", srv)
	}
}

func TestRouteFitCallbackExemptsContentTypeAndIdempotencyButKeepsBodyLimit(t *testing.T) {
	const path = "/v1/integrations/routefit/events"
	var calls atomic.Int64
	handler := NewHandlerWithGateway(nil, GatewayOptions{
		AppEnv: "test", OriginGroups: gateway.DefaultOriginGroups("test"),
		Limiter:         gateway.NewMemoryLimiter(),
		IPRules:         gateway.RuleSet{Default: gateway.Rule{Limit: 100, Window: time.Minute}},
		AccountRules:    gateway.RuleSet{Default: gateway.Rule{Limit: 100, Window: time.Minute}},
		WebhookMaxBody:  64 << 10,
		WebhookBindings: map[string]gateway.WebhookVerifier{path: allowWebhookVerifier{}},
		RouteFit:        http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNoContent) }),
	})
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || calls.Load() != 1 {
		t.Fatalf("callback status=%d calls=%d, want 204/1", response.Code, calls.Load())
	}

	request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(strings.Repeat("x", 64<<10+1)))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusRequestEntityTooLarge || calls.Load() != 1 {
		t.Fatalf("oversize callback status=%d calls=%d, want 413/1", response.Code, calls.Load())
	}
}

type allowWebhookVerifier struct{}

func (allowWebhookVerifier) Verify(*http.Request, []byte) error { return nil }

func TestSecurityHeadersCoverRouterErrors(t *testing.T) {
	handler := NewHandler(nil, func(context.Context) error { return context.DeadlineExceeded })
	for _, test := range []struct {
		method, path string
	}{
		{http.MethodGet, "/missing"},
		{http.MethodPost, "/v1/public/ping"},
		{http.MethodGet, "/readyz"},
	} {
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, httptest.NewRequest(test.method, test.path, nil))
		for _, name := range []string{"X-Content-Type-Options", "Referrer-Policy", "Content-Security-Policy", "Cross-Origin-Resource-Policy", "Cache-Control"} {
			if response.Header().Get(name) == "" {
				t.Errorf("%s %s missing %s", test.method, test.path, name)
			}
		}
	}
}

func TestBulkheadRejectsBeforeRateLimitAuthenticationAndIdempotency(t *testing.T) {
	bulkhead := overload.New(overload.Config{
		Total: 2, Wait: 10 * time.Millisecond,
		Limits:   overload.Limits{Critical: 2, Auth: 2, Write: 1, Read: 2, Heavy: 2},
		Timeouts: overload.DefaultConfig().Timeouts,
	})
	release, started, done := make(chan struct{}), make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		bulkhead.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			close(started)
			<-release
		})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/v1/buyer/block", nil))
	}()
	<-started

	limiter := &countingLimiter{}
	authenticator := &countingAuthenticator{}
	store := &countingIdempotencyStore{}
	rule := gateway.Rule{Limit: 100, Window: time.Minute}
	handler := NewHandlerWithGateway(nil, GatewayOptions{
		AppEnv: "test", OriginGroups: gateway.DefaultOriginGroups("test"), Bulkhead: bulkhead,
		Limiter: limiter, Authenticator: authenticator,
		IPRules:      gateway.RuleSet{Default: rule, Login: rule, Withdrawal: rule, AI: rule, Admin: rule},
		AccountRules: gateway.RuleSet{Default: rule, Login: rule, Withdrawal: rule, AI: rule, Admin: rule},
		Idempotency:  gateway.IdempotencyOptions{Store: store, TTL: time.Hour, MaxResponseBytes: 1024},
	})
	request := httptest.NewRequest(http.MethodPost, "/v1/buyer/missing", strings.NewReader(`{"value":1}`))
	request.Header.Set("Authorization", "Bearer placeholder")
	request.Header.Set("Idempotency-Key", "overload-test")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	close(release)
	<-done

	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d, want 503", response.Code)
	}
	if limiter.calls.Load() != 0 || authenticator.calls.Load() != 0 || store.calls.Load() != 0 {
		t.Fatalf("downstream calls limiter=%d authenticator=%d idempotency=%d", limiter.calls.Load(), authenticator.calls.Load(), store.calls.Load())
	}
	if response.Header().Get("X-Content-Type-Options") == "" {
		t.Fatal("security headers must still wrap overload rejection")
	}
}

type countingLimiter struct{ calls atomic.Int64 }

func (f *countingLimiter) Allow(context.Context, string, gateway.Rule) (bool, time.Duration, error) {
	f.calls.Add(1)
	return true, 0, nil
}

type countingAuthenticator struct{ calls atomic.Int64 }

func (f *countingAuthenticator) Authenticate(*http.Request) (gateway.Identity, bool, error) {
	f.calls.Add(1)
	return gateway.Identity{AccountID: "acct_test", Role: gateway.RoleBuyer}, true, nil
}

type countingIdempotencyStore struct{ calls atomic.Int64 }

func (f *countingIdempotencyStore) Begin(context.Context, string, string, time.Duration) (gateway.IdempotencyEntry, bool, error) {
	f.calls.Add(1)
	return gateway.IdempotencyEntry{}, true, nil
}
func (f *countingIdempotencyStore) Complete(context.Context, string, gateway.IdempotencyEntry, time.Duration) error {
	f.calls.Add(1)
	return nil
}
func (f *countingIdempotencyStore) Delete(context.Context, string) error {
	f.calls.Add(1)
	return nil
}

func TestServeWaitsForInflightRequestDuringShutdown(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		close(started)
		<-release
		w.WriteHeader(http.StatusOK)
	})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler}
	ctx, cancel := context.WithCancel(context.Background())
	serveDone := make(chan error, 1)
	go func() { serveDone <- Serve(ctx, srv, listener, time.Second) }()
	clientDone := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String())
		if err == nil {
			defer response.Body.Close()
			_, err = io.Copy(io.Discard, response.Body)
		}
		clientDone <- err
	}()
	<-started
	cancel()
	select {
	case err := <-serveDone:
		t.Fatalf("Serve returned before in-flight request completed: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
	if err := <-serveDone; err != nil {
		t.Fatal(err)
	}
}

func TestServeDrainsBeforeShutdown(t *testing.T) {
	bulkhead := overload.New(overload.Config{Total: 2, Wait: time.Millisecond, Limits: overload.Limits{Critical: 2, Auth: 2, Write: 2, Read: 2, Heavy: 2}, Timeouts: overload.DefaultConfig().Timeouts})
	started, release := make(chan struct{}), make(chan struct{})
	router := http.NewServeMux()
	router.HandleFunc("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if bulkhead.Draining() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})
	router.HandleFunc("/v1/public/ping", func(w http.ResponseWriter, _ *http.Request) { close(started); <-release; w.WriteHeader(http.StatusOK) })
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() {
		done <- Serve(ctx, &http.Server{Handler: bulkhead.Middleware(router)}, listener, time.Second, struct {
			Drainer
			Delay time.Duration
		}{Drainer: bulkhead, Delay: 50 * time.Millisecond})
	}()
	clientDone := make(chan error, 1)
	go func() {
		response, err := http.Get("http://" + listener.Addr().String() + "/v1/public/ping")
		if err == nil {
			response.Body.Close()
		}
		clientDone <- err
	}()
	<-started
	cancel()
	ready := eventually(t, func() int {
		response, err := http.Get("http://" + listener.Addr().String() + "/readyz")
		if err != nil {
			return 0
		}
		defer response.Body.Close()
		return response.StatusCode
	})
	if ready != http.StatusServiceUnavailable {
		t.Fatalf("ready status=%d", ready)
	}
	response, err := http.Get("http://" + listener.Addr().String() + "/v1/public/ping")
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("new request status=%d", response.StatusCode)
	}
	select {
	case err := <-done:
		t.Fatalf("shutdown completed before inflight request: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func eventually(t *testing.T, read func() int) int {
	t.Helper()
	deadline := time.NewTimer(time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		if value := read(); value != 0 {
			return value
		}
		select {
		case <-deadline.C:
			t.Fatal("condition did not become true")
		case <-tick.C:
		}
	}
}
