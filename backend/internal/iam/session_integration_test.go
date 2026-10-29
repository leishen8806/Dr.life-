package iam

import (
	"context"
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/iam/telegram"
)

type cachedTestSession struct {
	account string
	epoch   int64
	revoked bool
}

type testSessionCache struct {
	mu          sync.Mutex
	sessions    map[string]cachedTestSession
	epochs      map[string]int64
	epochTTL    time.Duration
	unavailable bool
}

func newTestSessionCache() *testSessionCache {
	return &testSessionCache{sessions: make(map[string]cachedTestSession), epochs: make(map[string]int64)}
}

func (c *testSessionCache) Get(_ context.Context, accountID, sessionID string) (bool, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unavailable {
		return false, false, ErrSessionStoreUnavailable
	}
	item, ok := c.sessions[sessionID]
	return ok && !item.revoked && item.account == accountID && item.epoch == c.epochs[accountID], ok, nil
}
func (c *testSessionCache) Put(_ context.Context, accountID, sessionID string, epoch int64, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unavailable {
		return ErrSessionStoreUnavailable
	}
	if c.epochs[accountID] <= epoch {
		c.epochs[accountID] = epoch
	}
	c.sessions[sessionID] = cachedTestSession{account: accountID, epoch: epoch}
	return nil
}
func (c *testSessionCache) Revoke(_ context.Context, sessionID string, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unavailable {
		return ErrSessionStoreUnavailable
	}
	item := c.sessions[sessionID]
	item.revoked = true
	c.sessions[sessionID] = item
	return nil
}
func (c *testSessionCache) SetAccountEpoch(_ context.Context, accountID string, epoch int64, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.unavailable {
		return ErrSessionStoreUnavailable
	}
	c.epochs[accountID] = epoch
	c.epochTTL = ttl
	return nil
}

type testSessionMetrics struct {
	mu      sync.Mutex
	results map[string]int
}

func (m *testSessionMetrics) SessionRefresh(endpoint, result string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.results == nil {
		m.results = make(map[string]int)
	}
	m.results[endpoint+":"+result]++
}

type testAlerter struct{ count int }

func (a *testAlerter) SessionReuseDetected(context.Context, string, string) { a.count++ }

func (f *loginFixture) testSessionLifecycle(t *testing.T) {
	ctx := context.Background()
	login, err := f.service.Login(ctx, LoginInput{End: telegram.Buyer, InitData: signInit(f.now, 9300, "buyer-standby-token", ""), DeviceID: "device_refresh", IP: "203.0.113.42"})
	if err != nil {
		t.Fatalf("login: %#v", err)
	}
	metrics, alert := &testSessionMetrics{}, &testAlerter{}
	sessions := &SessionService{DB: f.pool, Tokens: f.service.Tokens, Cache: f.cache, Grace: 10 * time.Second, IdleTTL: 24 * time.Hour, AbsoluteTTL: 7 * 24 * time.Hour, Metrics: metrics, Alerter: alert, Now: func() time.Time { return f.now }}

	const count = 50
	results := make(chan RefreshResult, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := sessions.Refresh(ctx, login.RefreshToken, "203.0.113.43")
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Errorf("concurrent refresh: %v", err)
	}
	var first RefreshResult
	for result := range results {
		if first.RefreshToken == "" {
			first = result
		}
		if result.RefreshToken != first.RefreshToken || result.AccessToken != first.AccessToken {
			t.Fatal("concurrent refresh forked the token pair")
		}
	}
	if metrics.results["buyer:success"] != 1 || metrics.results["buyer:replayed"] != count-1 {
		t.Fatalf("refresh metrics=%v", metrics.results)
	}
	var rotation int64
	var sessionCount int
	if err := f.pool.QueryRow(ctx, `SELECT rotation FROM iam.session WHERE refresh_token_hash=$1`, sha256Bytes(first.RefreshToken)).Scan(&rotation); err != nil || rotation != 1 {
		t.Fatalf("rotation=%d err=%v", rotation, err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM iam.session s JOIN iam.account a ON a.id=s.account_id WHERE a.public_id=$1 AND s.device_id='device_refresh'`, login.Account.PublicID).Scan(&sessionCount); err != nil || sessionCount != 1 {
		t.Fatalf("sessions=%d err=%v", sessionCount, err)
	}

	sessions.Now = func() time.Time { return f.now.Add(11 * time.Second) }
	if _, err := sessions.Refresh(ctx, login.RefreshToken, "203.0.113.43"); err == nil {
		t.Fatal("refresh token reuse outside grace was accepted")
	}
	var revoked, events int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM iam.session WHERE family_id=(SELECT family_id FROM iam.session WHERE device_id='device_refresh' LIMIT 1) AND revoked_at IS NOT NULL`).Scan(&revoked); err != nil || revoked != 1 {
		t.Fatalf("revoked=%d err=%v", revoked, err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM iam.security_event WHERE event='refresh_reuse' AND account_id=(SELECT id FROM iam.account WHERE public_id=$1)`, login.Account.PublicID).Scan(&events); err != nil || events != 1 || alert.count != 1 {
		t.Fatalf("events=%d alerts=%d err=%v", events, alert.count, err)
	}
	if metrics.results["buyer:reuse"] != 1 {
		t.Fatalf("reuse metric=%v", metrics.results)
	}
}

func (f *loginFixture) testSessionAccess(t *testing.T) {
	ctx := context.Background()
	login, err := f.service.Login(ctx, LoginInput{End: telegram.Buyer, InitData: signInit(f.now, 9400, "buyer-standby-token", ""), DeviceID: "device_access", IP: "2001:db8:1:2::99"})
	if err != nil {
		t.Fatalf("login: %#v", err)
	}
	sessions := &SessionService{DB: f.pool, Tokens: f.service.Tokens, Cache: f.cache, IdleTTL: 24 * time.Hour, AbsoluteTTL: 7 * 24 * time.Hour, Now: func() time.Time { return f.now }}
	items, err := sessions.ListSessions(ctx, login.Account.PublicID)
	if err != nil || len(items) != 1 || items[0].IPPrefix == nil || *items[0].IPPrefix != "2001:db8:1:2::/64" || items[0].DeviceID == nil || *items[0].DeviceID != "device_access" {
		t.Fatalf("sessions=%+v err=%v", items, err)
	}
	other, err := f.service.Login(ctx, LoginInput{End: telegram.Buyer, InitData: signInit(f.now, 9401, "buyer-standby-token", ""), IP: "192.0.2.1"})
	if err != nil {
		t.Fatal(err)
	}
	if err := sessions.RevokeSession(ctx, other.Account.PublicID, items[0].PublicID, "user_revoked"); err == nil {
		t.Fatal("another account revoked a session")
	}

	f.cache.mu.Lock()
	f.cache.sessions = make(map[string]cachedTestSession)
	f.cache.epochs = make(map[string]int64)
	f.cache.mu.Unlock()
	if err := sessions.RebuildCache(ctx); err != nil {
		t.Fatal(err)
	}
	checker := DBSessionChecker{DB: f.pool, Cache: f.cache, IdleTTL: 24 * time.Hour, Now: func() time.Time { return f.now }}
	if active, err := checker.Active(ctx, login.Account.PublicID, items[0].PublicID); err != nil || !active {
		t.Fatalf("rebuilt session active=%v err=%v", active, err)
	}
	manager := f.service.Tokens
	manager.Checker = checker
	request := httptest.NewRequest(http.MethodGet, "/v1/buyer/orders", nil)
	request.Header.Set("Authorization", "Bearer "+login.AccessToken)
	if _, ok, _ := (Authenticator{Tokens: manager}).Authenticate(request); !ok {
		t.Fatal("active account access token was rejected before freeze")
	}
	if _, err := f.pool.Exec(ctx, `UPDATE iam.account SET status='frozen',status_reason='test' WHERE public_id=$1`, login.Account.PublicID); err != nil {
		t.Fatal(err)
	}
	if err := sessions.RevokeAllSessions(ctx, login.Account.PublicID, "account_frozen"); err != nil {
		t.Fatal(err)
	}
	request = httptest.NewRequest(http.MethodGet, "/v1/buyer/orders", nil)
	request.Header.Set("Authorization", "Bearer "+login.AccessToken)
	if _, ok, _ := (Authenticator{Tokens: manager}).Authenticate(request); ok {
		t.Fatal("frozen account access token remained active")
	}

	activeLogin, err := f.service.Login(ctx, LoginInput{End: telegram.Buyer, InitData: signInit(f.now, 9500, "buyer-standby-token", ""), IP: "192.0.2.5"})
	if err != nil {
		t.Fatal(err)
	}
	f.cache.mu.Lock()
	f.cache.unavailable = true
	f.cache.mu.Unlock()
	manager.Checker = checker
	for _, test := range []struct {
		method, path string
		want         bool
	}{
		{http.MethodGet, "/v1/buyer/orders", true},
		{http.MethodPost, "/v1/buyer/withdrawals", false},
		{http.MethodGet, "/v1/admin/users", false},
	} {
		req := httptest.NewRequest(test.method, test.path, nil)
		req.Header.Set("Authorization", "Bearer "+activeLogin.AccessToken)
		_, ok, _ := (Authenticator{Tokens: manager}).Authenticate(req)
		if ok != test.want {
			t.Errorf("%s %s authenticated=%v want %v", test.method, test.path, ok, test.want)
		}
	}
	f.cache.mu.Lock()
	f.cache.unavailable = false
	f.cache.mu.Unlock()
}

func sha256Bytes(value string) []byte {
	digest := sha256.Sum256([]byte(value))
	return digest[:]
}

func TestSessionResponsesNeverExposeTokenFields(t *testing.T) {
	value := SessionInfo{PublicID: "01ARZ3NDEKTSV4RRFFQ69G5FAV", LastUsed: time.Now(), Expires: time.Now()}
	serialized := fmt.Sprintf("%+v", value)
	if strings.Contains(strings.ToLower(serialized), "token") || strings.Contains(strings.ToLower(serialized), "hash") {
		t.Fatalf("session response exposes token material: %s", serialized)
	}
}
