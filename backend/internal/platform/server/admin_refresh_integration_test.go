package server_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pquerna/otp/totp"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/leishen8806/Dr.life-/backend/internal/iam"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
	server "github.com/leishen8806/Dr.life-/backend/internal/platform/server"
)

type memorySessionCache struct {
	mu      sync.Mutex
	entries map[string]bool
}

func (c *memorySessionCache) Get(_ context.Context, _, sessionID string) (bool, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	active, ok := c.entries[sessionID]
	return active, ok, nil
}
func (c *memorySessionCache) Put(_ context.Context, _, sessionID string, _ int64, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]bool)
	}
	c.entries[sessionID] = true
	return nil
}
func (c *memorySessionCache) Revoke(_ context.Context, sessionID string, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries[sessionID] = false
	return nil
}
func (c *memorySessionCache) SetAccountEpoch(context.Context, string, int64, time.Duration) error {
	return nil
}

func TestRestrictedAdminRefreshHTTPAndReplayRemainRestricted(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("drlife_admin_refresh_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	_, file, _, _ := runtime.Caller(0)
	if err := goose.Up(db, filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	master, signing := make([]byte, 32), make([]byte, 32)
	_, _ = rand.Read(master)
	_, _ = rand.Read(signing)
	masterKeys, _ := secrets.NewKeyring(map[string][]byte{"test": master}, "test")
	jwtKeys, _ := secrets.NewKeyring(map[string][]byte{"test": signing}, "test")
	cipher, _ := secrets.NewEnvelope(masterKeys)
	now := time.Unix(1_800_000_000, 0)
	tokens := iam.TokenManager{Keys: jwtKeys, AccessTTL: 10 * time.Minute, Now: func() time.Time { return now }}
	cache := &memorySessionCache{}
	adminID, err := ids.NewULID()
	if err != nil {
		t.Fatal(err)
	}
	password, secret := "restricted admin password", "JBSWY3DPEHPK3PXP"
	hash, err := iam.HashPassword(password)
	if err != nil {
		t.Fatal(err)
	}
	secretEnc, err := iam.EncryptTOTPSecret(ctx, cipher, adminID, secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO iam.admin_user(public_id,name,email,password_hash,totp_secret_enc,role,status,password_change_required) VALUES($1,'Restricted Admin','restricted-admin@example.com',$2,$3,'super','active',true)`, adminID, hash, secretEnc); err != nil {
		t.Fatal(err)
	}
	login := &iam.AdminLoginService{DB: pool, Tokens: tokens, Cipher: cipher, RefreshTTL: time.Hour, IdleTTL: 15 * time.Minute, SessionCache: cache, Now: func() time.Time { return now }}
	sessions := &iam.SessionService{DB: pool, Tokens: iam.TokenManager{Keys: jwtKeys, AccessTTL: 15 * time.Minute, Now: func() time.Time { return now }}, AdminTokens: tokens, Cache: cache, Grace: time.Minute, IdleTTL: 15 * time.Minute, AbsoluteTTL: time.Hour, Now: func() time.Time { return now }}
	tokens.Checker = iam.DBSessionChecker{DB: pool, Cache: cache, IdleTTL: 15 * time.Minute, Now: func() time.Time { return now }}
	rule := gateway.Rule{Limit: 1000, Window: time.Minute}
	handler := server.NewHandlerWithGateway(nil, server.GatewayOptions{
		AppEnv: "dev", OriginGroups: gateway.DefaultOriginGroups("dev"), Limiter: gateway.NewMemoryLimiter(), Authenticator: iam.Authenticator{Tokens: tokens},
		IPRules: gateway.RuleSet{Default: rule, Login: rule, Admin: rule}, AccountRules: gateway.RuleSet{Default: rule, Login: rule, Admin: rule},
		IdempotencyExempt: func(r *http.Request) bool {
			return r.URL.Path == "/v1/admin/auth/login" || r.URL.Path == "/v1/admin/auth/refresh"
		},
		AdminLogin: login, Sessions: sessions,
	})
	httpServer := httptest.NewServer(handler)
	t.Cleanup(httpServer.Close)
	code, err := totp.GenerateCode(secret, now)
	if err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(map[string]string{"email": "restricted-admin@example.com", "password": password, "totp_code": code})
	loginRequest, err := http.NewRequestWithContext(ctx, http.MethodPost, httpServer.URL+"/v1/admin/auth/login", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	loginRequest.Header.Set("Content-Type", "application/json")
	loginResponse, err := http.DefaultClient.Do(loginRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer loginResponse.Body.Close()
	if loginResponse.StatusCode != http.StatusOK {
		responseBody, _ := io.ReadAll(loginResponse.Body)
		t.Fatalf("login status=%d body=%s", loginResponse.StatusCode, responseBody)
	}
	cookies := loginResponse.Cookies()
	if len(cookies) != 1 {
		t.Fatalf("login cookies=%d", len(cookies))
	}
	refreshCookie := cookies[0]

	refresh := func(cookie *http.Cookie) string {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, httpServer.URL+"/v1/admin/auth/refresh", bytes.NewReader([]byte(`{}`)))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Origin", "http://localhost:5173")
		req.Header.Set("X-Requested-With", "drlife")
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(cookie)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusOK {
			t.Fatalf("refresh status=%d", response.StatusCode)
		}
		var result struct {
			Data struct {
				AccessToken string `json:"access_token"`
			} `json:"data"`
		}
		if err := json.NewDecoder(response.Body).Decode(&result); err != nil {
			t.Fatal(err)
		}
		if result.Data.AccessToken == "" {
			t.Fatal("refresh response omitted access token")
		}
		return result.Data.AccessToken
	}
	assertRestricted := func(access string) {
		claims := new(iam.AccessClaims)
		_, _, err := new(jwt.Parser).ParseUnverified(access, claims)
		if err != nil || !claims.PasswordChangeOnly || claims.ExpiresAt.Time.Sub(claims.IssuedAt.Time) != 10*time.Minute {
			t.Fatalf("claims=%+v err=%v, want restricted 600-second access", claims, err)
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, httpServer.URL+"/v1/admin/admins", nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+access)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		if response.StatusCode != http.StatusForbidden {
			t.Fatalf("restricted token /v1/admin/admins status=%d, want 403", response.StatusCode)
		}
	}

	assertRestricted(refresh(refreshCookie))
	assertRestricted(refresh(refreshCookie))
}
