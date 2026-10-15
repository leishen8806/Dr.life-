package gateway

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestGroupedCORSSeparatesClients(t *testing.T) {
	groups := DefaultOriginGroups("prod")
	handler := GroupedCORS(groups)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	tests := []struct {
		path, origin string
		want         int
	}{
		{"/v1/buyer/x", "https://miniapp.settlecore.org", http.StatusNoContent},
		{"/v1/merchant/x", "https://miniapp.settlecore.org", http.StatusForbidden},
		{"/v1/public/ping", "https://settlecore.org", http.StatusNoContent},
		{"/v1/admin/x", "https://settlecore.org", http.StatusForbidden},
	}
	for _, test := range tests {
		request := httptest.NewRequest(http.MethodGet, test.path, nil)
		request.Header.Set("Origin", test.origin)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != test.want || response.Header().Get("Vary") != "Origin" || response.Header().Get("Access-Control-Allow-Origin") == "*" {
			t.Errorf("path=%s origin=%s status=%d headers=%v", test.path, test.origin, response.Code, response.Header())
		}
	}
	request := httptest.NewRequest(http.MethodOptions, "/v1/buyer/x", nil)
	request.Header.Set("Origin", "https://miniapp.settlecore.org")
	request.Header.Set("Access-Control-Request-Method", http.MethodPost)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNoContent || response.Header().Get("Access-Control-Max-Age") != "600" || response.Header().Get("Access-Control-Allow-Credentials") != "" {
		t.Fatalf("preflight status=%d headers=%v", response.Code, response.Header())
	}
	groups.CredentialPaths = []string{"/v1/admin/auth/refresh"}
	handler = GroupedCORS(groups)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request = httptest.NewRequest(http.MethodPost, "/v1/admin/auth/refresh", nil)
	request.Header.Set("Origin", "https://admin.settlecore.org")
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Header().Get("Access-Control-Allow-Credentials") != "true" {
		t.Fatalf("credential header missing: %v", response.Header())
	}
}

func TestSecurityHeaders(t *testing.T) {
	for _, env := range []string{"dev", "prod"} {
		response := httptest.NewRecorder()
		response.Header().Set("Server", "fingerprint")
		SecurityHeaders(env)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Server", "downstream-fingerprint")
			w.WriteHeader(http.StatusServiceUnavailable)
		})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/x", nil))
		for _, header := range []string{"X-Content-Type-Options", "Referrer-Policy", "Content-Security-Policy", "Cross-Origin-Resource-Policy", "Cache-Control"} {
			if response.Header().Get(header) == "" {
				t.Errorf("env=%s missing %s", env, header)
			}
		}
		if response.Header().Get("Server") != "" || (env == "dev") == (response.Header().Get("Strict-Transport-Security") != "") {
			t.Errorf("env=%s headers=%v", env, response.Header())
		}
	}
	response := httptest.NewRecorder()
	SecurityHeaders("prod")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Cache-Control", "public, max-age=60")
		w.WriteHeader(http.StatusOK)
	})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/v1/public/cacheable", nil))
	if response.Header().Get("Cache-Control") != "public, max-age=60" {
		t.Fatalf("public cache declaration was overwritten: %v", response.Header())
	}
}

func TestRequestBodyPolicyAndStrictJSON(t *testing.T) {
	type input struct {
		Name string `json:"name"`
	}
	handler := LimitRequestBody(BodyPolicy{DefaultBytes: 24})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var value input
		if err := httpx.DecodeJSON(r, &value); err != nil {
			httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	for _, test := range []struct {
		name, contentType, body string
		want                    int
	}{
		{"valid", "application/json; charset=utf-8", `{"name":"ok"}`, http.StatusNoContent},
		{"media", "text/plain", `{}`, http.StatusUnsupportedMediaType},
		{"unknown", "application/json", `{"other":1}`, http.StatusBadRequest},
		{"trailing", "application/json", `{ "name":"x" } {}`, http.StatusBadRequest},
		{"large", "application/json", `{"name":"` + strings.Repeat("x", 30) + `"}`, http.StatusRequestEntityTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/v1/buyer/x", strings.NewReader(test.body))
			request.Header.Set("Content-Type", test.contentType)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.want {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}

func idempotencyHandler(store IdempotencyStore, prefixes []string, calls *atomic.Int32) http.Handler {
	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		time.Sleep(10 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"ok":true}`))
	})
	bound := BindIdempotency(IdempotencyOptions{Store: store, FinancialPrefixes: prefixes})(final)
	required := RequireIdempotency(nil)(bound)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		withIdentity(r, Identity{AccountID: r.Header.Get("X-Account"), Role: RoleBuyer})
		required.ServeHTTP(w, r)
	})
}

func TestIdempotencyBindsBodyAndAccount(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	var calls atomic.Int32
	handler := idempotencyHandler(store, nil, &calls)
	request := func(account, body string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/buyer/orders", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		r.Header.Set("Idempotency-Key", "same")
		r.Header.Set("X-Account", account)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	first, replay := request("a", `{"x":1}`), request("a", `{"x":1}`)
	if first.Code != http.StatusCreated || replay.Code != http.StatusCreated || replay.Header().Get("Idempotent-Replayed") != "true" || first.Body.String() != replay.Body.String() || calls.Load() != 1 {
		t.Fatalf("first=%d replay=%d headers=%v calls=%d", first.Code, replay.Code, replay.Header(), calls.Load())
	}
	if conflict := request("a", `{"x":2}`); conflict.Code != http.StatusUnprocessableEntity {
		t.Fatalf("conflict status=%d body=%s", conflict.Code, conflict.Body.String())
	}
	if other := request("b", `{"x":2}`); other.Code != http.StatusCreated || calls.Load() != 2 {
		t.Fatalf("cross-account status=%d calls=%d", other.Code, calls.Load())
	}
}

func TestIdempotencyRouteTemplateStillBindsTargetPath(t *testing.T) {
	var calls atomic.Int32
	router := chi.NewRouter()
	router.Use(RequireIdempotency(nil))
	router.Use(BindIdempotency(IdempotencyOptions{Store: NewMemoryIdempotencyStore()}))
	router.Post("/orders/{id}", func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusCreated)
	})
	call := func(path string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"reason":"cancel"}`))
		r.Header.Set("Idempotency-Key", "cancel_one")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, r)
		return w
	}
	if first, conflict := call("/orders/one"), call("/orders/two"); first.Code != http.StatusCreated || conflict.Code != http.StatusUnprocessableEntity || calls.Load() != 1 {
		t.Fatalf("first=%d conflict=%d calls=%d", first.Code, conflict.Code, calls.Load())
	}
}

func TestIdempotencyConcurrentExecutesOnce(t *testing.T) {
	var calls atomic.Int32
	handler := idempotencyHandler(NewMemoryIdempotencyStore(), nil, &calls)
	statuses := make(chan int, 50)
	var wg sync.WaitGroup
	for range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r := httptest.NewRequest(http.MethodPost, "/v1/buyer/orders", strings.NewReader(`{"x":1}`))
			r.Header.Set("Content-Type", "application/json")
			r.Header.Set("Idempotency-Key", "concurrent")
			r.Header.Set("X-Account", "a")
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, r)
			statuses <- w.Code
		}()
	}
	wg.Wait()
	close(statuses)
	created := 0
	for status := range statuses {
		if status == http.StatusCreated {
			created++
		} else if status != http.StatusConflict {
			t.Errorf("unexpected status %d", status)
		}
	}
	if calls.Load() != 1 || created != 1 {
		t.Fatalf("calls=%d created=%d", calls.Load(), created)
	}
}

type failingIdempotencyStore struct{}

func (failingIdempotencyStore) Begin(context.Context, string, string, time.Duration) (IdempotencyEntry, bool, error) {
	return IdempotencyEntry{}, false, errors.New("unavailable")
}
func (failingIdempotencyStore) Complete(context.Context, string, IdempotencyEntry, time.Duration) error {
	return nil
}
func (failingIdempotencyStore) Delete(context.Context, string) error { return nil }

func TestIdempotencyFailurePolicy(t *testing.T) {
	for _, test := range []struct {
		path string
		want int
	}{
		{"/v1/buyer/withdrawals", http.StatusServiceUnavailable},
		{"/v1/buyer/orders", http.StatusCreated},
	} {
		var calls atomic.Int32
		handler := idempotencyHandler(failingIdempotencyStore{}, []string{"/v1/buyer/withdrawals"}, &calls)
		r := httptest.NewRequest(http.MethodPost, test.path, strings.NewReader(`{}`))
		r.Header.Set("Idempotency-Key", "key")
		r.Header.Set("X-Account", "a")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Errorf("path=%s status=%d", test.path, w.Code)
		}
	}
}

func TestIdempotencyNonFinancialUsesProcessFallback(t *testing.T) {
	var calls, degraded atomic.Int32
	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusCreated)
	})
	handler := RequireIdempotency(nil)(BindIdempotency(IdempotencyOptions{
		Store: failingIdempotencyStore{}, FallbackStore: NewMemoryIdempotencyStore(), OnDegraded: func() { degraded.Add(1) },
	})(final))
	for i := 0; i < 2; i++ {
		r := httptest.NewRequest(http.MethodPost, "/v1/buyer/orders", strings.NewReader(`{}`))
		r.Header.Set("Idempotency-Key", "fallback")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != http.StatusCreated {
			t.Fatalf("request %d status=%d", i, w.Code)
		}
	}
	if calls.Load() != 1 || degraded.Load() != 2 {
		t.Fatalf("calls=%d degraded=%d", calls.Load(), degraded.Load())
	}
}

func TestIdempotencyFiveXXCanRetryAndLargeResponseIsNotReplayed(t *testing.T) {
	store := NewMemoryIdempotencyStore()
	var calls atomic.Int32
	final := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("response-too-large"))
	})
	handler := RequireIdempotency(nil)(BindIdempotency(IdempotencyOptions{Store: store, MaxResponseBytes: 4})(final))
	call := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/buyer/orders", strings.NewReader(`{}`))
		r.Header.Set("Idempotency-Key", "retry")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	if first, second, replay := call(), call(), call(); first.Code != http.StatusInternalServerError || second.Code != http.StatusCreated || replay.Code != http.StatusConflict || replay.Header().Get("Idempotent-Replayed") != "true" || calls.Load() != 2 {
		t.Fatalf("statuses=%d,%d,%d calls=%d", first.Code, second.Code, replay.Code, calls.Load())
	}
}

func TestMemoryIdempotencyExpiry(t *testing.T) {
	now := time.Unix(100, 0)
	store := NewMemoryIdempotencyStore()
	store.now = func() time.Time { return now }
	if _, acquired, err := store.Begin(context.Background(), "key", "hash", time.Second); err != nil || !acquired {
		t.Fatalf("first begin acquired=%v err=%v", acquired, err)
	}
	now = now.Add(2 * time.Second)
	if _, acquired, err := store.Begin(context.Background(), "key", "new-hash", time.Second); err != nil || !acquired {
		t.Fatalf("expired begin acquired=%v err=%v", acquired, err)
	}
}

func TestRedisSecurityStores(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := testcontainers.Run(ctx, "redis:7-alpine", testcontainers.WithExposedPorts("6379/tcp"), testcontainers.WithWaitStrategy(wait.ForListeningPort("6379/tcp")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatal(err)
	}
	url := "redis://" + host + ":" + port.Port() + "/0"
	idem, err := NewRedisIdempotencyStore(url)
	if err != nil {
		t.Fatal(err)
	}
	defer idem.Close()
	entry, acquired, err := idem.Begin(ctx, "key", "hash", time.Minute)
	if err != nil || !acquired {
		t.Fatalf("begin acquired=%v err=%v", acquired, err)
	}
	entry.State, entry.Status, entry.Body = "done", http.StatusCreated, []byte("ok")
	if err := idem.Complete(ctx, "key", entry, time.Minute); err != nil {
		t.Fatal(err)
	}
	replayed, acquired, err := idem.Begin(ctx, "key", "hash", time.Minute)
	if err != nil || acquired || replayed.State != "done" || string(replayed.Body) != "ok" {
		t.Fatalf("replay=%+v acquired=%v err=%v", replayed, acquired, err)
	}
	jti, err := NewRedisJTIStore(url)
	if err != nil {
		t.Fatal(err)
	}
	defer jti.Close()
	if fresh, err := jti.Consume(ctx, "one", time.Minute); err != nil || !fresh {
		t.Fatalf("first consume fresh=%v err=%v", fresh, err)
	}
	if fresh, err := jti.Consume(ctx, "one", time.Minute); err != nil || fresh {
		t.Fatalf("second consume fresh=%v err=%v", fresh, err)
	}
}

type fakeStepUpVerifier struct{}

func (fakeStepUpVerifier) Verify(_ context.Context, token, scope, account string) (string, error) {
	if token != "valid" || scope != "withdraw" || account != "a" {
		return "", errors.New("invalid")
	}
	return "jti_1", nil
}

func TestStepUpCannotBeSpoofedAndJTIIsSingleUse(t *testing.T) {
	used := NewMemoryJTIStore()
	handler := RequireStepUp("withdraw", fakeStepUpVerifier{}, used)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !StepUpVerified(r.Context()) {
			t.Fatal("verified marker missing")
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	call := func(token string, spoof bool) int {
		r := httptest.NewRequest(http.MethodPost, "/v1/buyer/withdrawals", nil)
		withIdentity(r, Identity{AccountID: "a", Role: RoleBuyer})
		r.Header.Set("X-Step-Up-Token", token)
		if spoof {
			r.Header.Set("X-Step-Up-Verified", "true")
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w.Code
	}
	if call("", true) != http.StatusUnauthorized || call("valid", false) != http.StatusNoContent || call("valid", false) != http.StatusUnauthorized {
		t.Fatal("step-up policy failed")
	}
}

func TestIdempotentReplayDoesNotConsumeAnotherStepUp(t *testing.T) {
	used := NewMemoryJTIStore()
	var calls atomic.Int32
	final := RequireStepUp("withdraw", fakeStepUpVerifier{}, used)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		w.WriteHeader(http.StatusCreated)
	}))
	handler := RequireIdempotency(nil)(BindIdempotency(IdempotencyOptions{Store: NewMemoryIdempotencyStore()})(final))
	call := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/v1/buyer/withdrawals", strings.NewReader(`{}`))
		withIdentity(r, Identity{AccountID: "a", Role: RoleBuyer})
		r.Header.Set("Idempotency-Key", "withdraw_one")
		r.Header.Set("X-Step-Up-Token", "valid")
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	first, replay := call(), call()
	if first.Code != http.StatusCreated || replay.Code != http.StatusCreated || replay.Header().Get("Idempotent-Replayed") != "true" || calls.Load() != 1 {
		t.Fatalf("first=%d replay=%d calls=%d", first.Code, replay.Code, calls.Load())
	}
}

func TestAdminAccessFailClosedAndTrustedProxy(t *testing.T) {
	resolver, _ := NewClientIPResolver("CF-Connecting-IP", []string{"10.0.0.0/8"})
	access, _ := NewAdminAccess("prod", resolver, []string{"2001:db8::/32"})
	handler := access.Middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	for _, test := range []struct {
		remote, forwarded string
		want              int
	}{
		{"10.0.0.1:1", "2001:db8::1", http.StatusNoContent},
		{"198.51.100.1:1", "2001:db8::1", http.StatusForbidden},
	} {
		r := httptest.NewRequest(http.MethodGet, "/v1/admin/x", nil)
		r.RemoteAddr = test.remote
		r.Header.Set("CF-Connecting-IP", test.forwarded)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != test.want {
			t.Errorf("status=%d want=%d", w.Code, test.want)
		}
	}
	closed, _ := NewAdminAccess("prod", resolver, nil)
	w := httptest.NewRecorder()
	closed.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/wms/x", nil))
	if w.Code != http.StatusForbidden {
		t.Fatalf("empty production allowlist status=%d", w.Code)
	}
}

func TestWebhookVerifiersAndFailClosed(t *testing.T) {
	secret := []byte("test-secret")
	now := time.Unix(1000, 0)
	hmacVerifier := HMACVerifier{Secret: secret, Nonces: NewMemoryJTIStore(), Now: func() time.Time { return now }}
	bindings := map[string]WebhookVerifier{
		"/webhooks/telegram": HeaderSecretVerifier{Header: "X-Telegram-Bot-Api-Secret-Token", Secret: "telegram-secret"},
		"/webhooks/bank":     hmacVerifier,
	}
	handler := VerifyWebhooks("/webhooks", 32, bindings)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	tg := httptest.NewRequest(http.MethodPost, "/webhooks/telegram", strings.NewReader("{}"))
	tg.Header.Set("X-Telegram-Bot-Api-Secret-Token", "telegram-secret")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, tg)
	if w.Code != http.StatusNoContent {
		t.Fatalf("telegram status=%d", w.Code)
	}
	body, timestamp := []byte(`{"amount":1}`), strconv.FormatInt(now.Unix(), 10)
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write(body)
	_, _ = mac.Write([]byte(timestamp))
	bankRequest := func(nonce string, payload []byte) *http.Request {
		r := httptest.NewRequest(http.MethodPost, "/webhooks/bank", bytes.NewReader(payload))
		r.Header.Set("X-Webhook-Timestamp", timestamp)
		r.Header.Set("X-Webhook-Nonce", nonce)
		r.Header.Set("X-Webhook-Signature", hex.EncodeToString(mac.Sum(nil)))
		return r
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, bankRequest("one", body))
	if w.Code != http.StatusNoContent {
		t.Fatalf("hmac status=%d body=%s", w.Code, w.Body.String())
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, bankRequest("one", body))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("replay status=%d", w.Code)
	}
	for _, request := range []*http.Request{
		bankRequest("tampered", []byte(`{"amount":2}`)),
		httptest.NewRequest(http.MethodPost, "/webhooks/bank", bytes.NewReader(body)),
	} {
		w = httptest.NewRecorder()
		handler.ServeHTTP(w, request)
		if w.Code != http.StatusUnauthorized {
			t.Errorf("invalid webhook status=%d", w.Code)
		}
	}
	expired := bankRequest("expired", body)
	expired.Header.Set("X-Webhook-Timestamp", strconv.FormatInt(now.Add(-10*time.Minute).Unix(), 10))
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, expired)
	if w.Code != http.StatusUnauthorized {
		t.Errorf("expired webhook status=%d", w.Code)
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/webhooks/unbound", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("unbound status=%d", w.Code)
	}
	tooLarge := httptest.NewRequest(http.MethodPost, "/webhooks/telegram", strings.NewReader(strings.Repeat("x", 33)))
	tooLarge.Header.Set("X-Telegram-Bot-Api-Secret-Token", "telegram-secret")
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, tooLarge)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized webhook status=%d", w.Code)
	}
	resolver, _ := NewClientIPResolver("CF-Connecting-IP", []string{"10.0.0.0/8"})
	cidr, _ := NewCIDRVerifier(resolver, []string{"203.0.113.0/24"})
	cidrRequest := httptest.NewRequest(http.MethodPost, "/webhooks/cidr", nil)
	cidrRequest.RemoteAddr = "10.0.0.1:1"
	cidrRequest.Header.Set("CF-Connecting-IP", "203.0.113.4")
	if err := cidr.Verify(cidrRequest, nil); err != nil {
		t.Fatal(err)
	}
}

func BenchmarkSecurityHeaders(b *testing.B) {
	handler := SecurityHeaders("prod")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) }))
	request := httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil)
	b.ReportAllocs()
	for range b.N {
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
}

func BenchmarkGatewayBaseline(b *testing.B) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	request := httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil)
	b.ReportAllocs()
	for range b.N {
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
}

func BenchmarkGatewaySecurityChain(b *testing.B) {
	var handler http.Handler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	handler = BindIdempotency(IdempotencyOptions{Store: NewMemoryIdempotencyStore()})(handler)
	handler = RequireIdempotency(nil)(handler)
	handler = VerifyWebhooks("", 64<<10, nil)(handler)
	handler = AdminAccess{AppEnv: "prod"}.Middleware(handler)
	handler = LimitRequestBody(BodyPolicy{DefaultBytes: 64 << 10})(handler)
	handler = GroupedCORS(DefaultOriginGroups("prod"))(handler)
	handler = SecurityHeaders("prod")(handler)
	request := httptest.NewRequest(http.MethodGet, "/v1/public/ping", nil)
	b.ReportAllocs()
	for range b.N {
		handler.ServeHTTP(httptest.NewRecorder(), request)
	}
}
