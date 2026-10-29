package iam

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/leishen8806/Dr.life-/backend/internal/iam/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/gateway"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
)

type loginFixture struct {
	pool     *pgxpool.Pool
	dsn      string
	envelope *secrets.Envelope
	keys     *secrets.Keyring
	now      time.Time
	source   *telegram.DBPoolSource
	service  *Service
	tokens   map[telegram.End][]string
	cache    *testSessionCache
}

func TestTelegramLoginIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("drlife_login_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate: %v", err)
		}
	})
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	_, file, _, _ := runtime.Caller(0)
	if err := goose.Up(db, filepath.Join(filepath.Dir(file), "..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()
	adminPool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(adminPool.Close)
	if _, err := adminPool.Exec(ctx, `CREATE ROLE app_iam_login_test LOGIN PASSWORD 'test-only'`); err != nil {
		t.Fatal(err)
	}
	if _, err := adminPool.Exec(ctx, `GRANT iam_rw TO app_iam_login_test`); err != nil {
		t.Fatal(err)
	}
	iamURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	iamURL.User = url.UserPassword("app_iam_login_test", "test-only")
	pool, err := pgxpool.New(ctx, iamURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	master := make([]byte, 32)
	signing := make([]byte, 32)
	_, _ = rand.Read(master)
	_, _ = rand.Read(signing)
	masterRing, _ := secrets.NewKeyring(map[string][]byte{"test": master}, "test")
	jwtRing, _ := secrets.NewKeyring(map[string][]byte{"test": signing}, "test")
	envelope, _ := secrets.NewEnvelope(masterRing)
	now := time.Unix(1_800_000_000, 0)
	f := &loginFixture{pool: pool, dsn: iamURL.String(), envelope: envelope, keys: jwtRing, now: now, tokens: map[telegram.End][]string{
		telegram.Buyer: {"buyer-primary-token", "buyer-standby-token"}, telegram.Merchant: {"merchant-primary-token"}, telegram.Driver: {"driver-primary-token"},
	}}
	f.seedBots(t, ctx)
	f.source = &telegram.DBPoolSource{DB: pool, Envelope: envelope, TTL: 5 * time.Minute, Now: func() time.Time { return now }}
	manager := TokenManager{Keys: jwtRing, AccessTTL: 15 * time.Minute, Now: func() time.Time { return now }}
	f.cache = newTestSessionCache()
	f.service = &Service{DB: pool, Verifier: telegram.PoolVerifier{Validator: validatorAt(now), Source: f.source}, Tokens: manager, RefreshTTL: 7 * 24 * time.Hour, IdleTTL: 24 * time.Hour, MaxSessions: 5, SessionCache: f.cache, Now: func() time.Time { return now }}

	t.Run("pool isolation status and fallback", f.testPool)
	t.Run("one hundred concurrent first logins", f.testConcurrentLogin)
	t.Run("merchant owner account reuse and ambiguity", f.testMerchantOwnerReuseAndAmbiguity)
	t.Run("ambiguous login audits with small pools", f.testAmbiguousLoginSmallPools)
	t.Run("blocked account and safe events", f.testBlockedAndEvents)
	t.Run("validation failures are classified", f.testFailureEvents)
	t.Run("audit write failure is observable", f.testAuditWriteFailure)
	t.Run("failure ban expires", f.testFailureBan)
	t.Run("refresh plaintext is absent", f.testRefreshNotStored)
	t.Run("refresh rotation concurrency reuse and revocation", f.testSessionLifecycle)
	t.Run("session ownership cache rebuild and fail policy", f.testSessionAccess)
	t.Run("account language profile and safe devices", f.testAccountLanguageProfileAndSafeDevices)
}

func (f *loginFixture) testAccountLanguageProfileAndSafeDevices(t *testing.T) {
	ctx := context.Background()
	first, err := f.service.Login(ctx, LoginInput{End: telegram.Buyer, InitData: signInit(f.now, 9101, "buyer-standby-token", ""), DeviceID: "device_one", IP: "198.51.100.21"})
	if err != nil {
		t.Fatal(err)
	}

	profiles := AccountProfileService{DB: f.pool}
	for _, language := range []string{"fr", "", "ZH"} {
		if err := profiles.SetLanguage(ctx, first.Account.PublicID, language); err == nil {
			t.Fatalf("SetLanguage(%q) succeeded", language)
		}
	}
	before, err := profiles.Me(ctx, first.Account.PublicID)
	if err != nil || !before.NeedsLanguage || before.Lang != nil || before.Role != "buyer" || before.Status != "active" {
		t.Fatalf("unregistered profile=%+v err=%v", before, err)
	}
	if err := profiles.SetLanguage(ctx, first.Account.PublicID, "zh"); err != nil {
		t.Fatal(err)
	}
	registered, err := profiles.Me(ctx, first.Account.PublicID)
	if err != nil || registered.NeedsLanguage || registered.Lang == nil || *registered.Lang != "zh" || registered.LangRegisteredAt == nil {
		t.Fatalf("registered profile=%+v err=%v", registered, err)
	}
	registeredAt := *registered.LangRegisteredAt
	if err := profiles.SetLanguage(ctx, first.Account.PublicID, "en"); err != nil {
		t.Fatal(err)
	}
	changed, err := profiles.Me(ctx, first.Account.PublicID)
	if err != nil || changed.Lang == nil || *changed.Lang != "en" || changed.LangRegisteredAt == nil || !changed.LangRegisteredAt.Equal(registeredAt) {
		t.Fatalf("changed profile=%+v err=%v registered_at=%v", changed, err, registeredAt)
	}
	lang, isRegistered, err := profiles.GetAccountLanguage(ctx, first.Account.PublicID)
	if err != nil || !isRegistered || lang != "en" {
		t.Fatalf("GetAccountLanguage=%q/%t/%v", lang, isRegistered, err)
	}

	if _, err := f.service.Login(ctx, LoginInput{End: telegram.Buyer, InitData: signInit(f.now, 9101, "buyer-standby-token", ""), DeviceID: "device_two", IP: "198.51.100.22"}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE iam.account_device SET last_seen_at=CASE WHEN device_id='device_one' THEN clock_timestamp()-interval '1 hour' ELSE clock_timestamp() END WHERE account_id=(SELECT id FROM iam.account WHERE public_id=$1)`, first.Account.PublicID); err != nil {
		t.Fatal(err)
	}
	var currentSession string
	if err := f.pool.QueryRow(ctx, `SELECT s.public_id FROM iam.session s JOIN iam.account a ON a.id=s.account_id WHERE a.public_id=$1 AND s.device_id='device_two' ORDER BY s.created_at DESC LIMIT 1`, first.Account.PublicID).Scan(&currentSession); err != nil {
		t.Fatal(err)
	}
	if _, err := f.service.Login(ctx, LoginInput{End: telegram.Buyer, InitData: signInit(f.now, 9102, "buyer-standby-token", ""), DeviceID: "other_account_device", IP: "198.51.100.23"}); err != nil {
		t.Fatal(err)
	}
	devices, err := profiles.Devices(ctx, first.Account.PublicID, currentSession)
	if err != nil || len(devices) != 2 || devices[0].PublicID == "" || !devices[0].IsCurrent || devices[1].IsCurrent || !devices[0].LastSeenAt.After(devices[1].LastSeenAt) {
		t.Fatalf("devices=%+v err=%v", devices, err)
	}
	noDeviceLogin, err := f.service.Login(ctx, LoginInput{End: telegram.Buyer, InitData: signInit(f.now, 9101, "buyer-standby-token", ""), IP: "198.51.100.24"})
	if err != nil {
		t.Fatal(err)
	}
	var noDeviceSession string
	if err := f.pool.QueryRow(ctx, `SELECT s.public_id FROM iam.session s JOIN iam.account a ON a.id=s.account_id WHERE a.public_id=$1 AND s.public_id<>$2 AND s.device_id IS NULL ORDER BY s.created_at DESC LIMIT 1`, noDeviceLogin.Account.PublicID, currentSession).Scan(&noDeviceSession); err != nil {
		t.Fatal(err)
	}
	devices, err = profiles.Devices(ctx, first.Account.PublicID, noDeviceSession)
	if err != nil || len(devices) != 2 || devices[0].IsCurrent || devices[1].IsCurrent {
		t.Fatalf("no-device session devices=%+v err=%v", devices, err)
	}
}

func (f *loginFixture) testMerchantOwnerReuseAndAmbiguity(t *testing.T) {
	ctx := context.Background()
	raw := signInit(f.now, 8003, "merchant-primary-token", "")
	first, err := f.service.Login(ctx, LoginInput{End: telegram.Merchant, InitData: raw, IP: "198.51.100.13"})
	if err != nil {
		t.Fatal(err)
	}
	bindTestMerchant(t, ctx, f.pool, first.Account.PublicID)
	again, err := f.service.Login(ctx, LoginInput{End: telegram.Merchant, InitData: raw, IP: "198.51.100.13"})
	if err != nil || again.Account.PublicID != first.Account.PublicID {
		t.Fatalf("bound owner login=%+v err=%v", again, err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM iam.account a JOIN iam.person p ON p.id=a.person_id WHERE p.tg_user_id=8003 AND a.role='merchant_owner'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("merchant owner accounts=%d err=%v", count, err)
	}
	secondMerchantID := insertTestMerchant(t, ctx, f.pool)
	if _, err := f.pool.Exec(ctx, `INSERT INTO iam.account(public_id,person_id,role,merchant_id) SELECT $1,p.id,'merchant_owner',$2 FROM iam.person p WHERE p.tg_user_id=8003`, mustULID(t), secondMerchantID); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.Login(ctx, LoginInput{End: telegram.Merchant, InitData: raw, IP: "198.51.100.13"})
	var appErr *httpx.AppError
	if !errorsAs(err, &appErr) || appErr.Code != httpx.CodeInternalError {
		t.Fatalf("ambiguous login error=%v", err)
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM iam.login_event e JOIN iam.person p ON p.id=e.person_id WHERE p.tg_user_id=8003 AND e.result='ambiguous_account'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("ambiguous audit events=%d err=%v", count, err)
	}
}

func (f *loginFixture) testAmbiguousLoginSmallPools(t *testing.T) {
	ctx := context.Background()
	raw := signInit(f.now, 8003, "merchant-primary-token", "")
	var wantCount int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM iam.login_event e JOIN iam.person p ON p.id=e.person_id WHERE p.tg_user_id=8003 AND e.result='ambiguous_account'`).Scan(&wantCount); err != nil {
		t.Fatal(err)
	}
	for _, maxConns := range []int32{1, 4} {
		t.Run(fmt.Sprintf("max conns %d", maxConns), func(t *testing.T) {
			config, err := pgxpool.ParseConfig(f.dsn)
			if err != nil {
				t.Fatal(err)
			}
			config.MaxConns = maxConns
			pool, err := pgxpool.NewWithConfig(ctx, config)
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			service := *f.service
			service.DB = pool
			service.Verifier.Source = &telegram.DBPoolSource{DB: pool, Envelope: f.envelope, TTL: 5 * time.Minute, Now: func() time.Time { return f.now }}
			const requests = 4
			errs := make(chan error, requests)
			var wg sync.WaitGroup
			for range requests {
				wg.Add(1)
				go func() {
					defer wg.Done()
					requestCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					defer cancel()
					_, err := service.Login(requestCtx, LoginInput{End: telegram.Merchant, InitData: raw, IP: "198.51.100.13"})
					errs <- err
				}()
			}
			wg.Wait()
			close(errs)
			for err := range errs {
				var appErr *httpx.AppError
				if !errorsAs(err, &appErr) || appErr.Code != httpx.CodeInternalError {
					t.Fatalf("ambiguous login max conns %d error=%v", maxConns, err)
				}
			}
			wantCount += requests
			var count int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM iam.login_event e JOIN iam.person p ON p.id=e.person_id WHERE p.tg_user_id=8003 AND e.result='ambiguous_account'`).Scan(&count); err != nil || count != wantCount {
				t.Fatalf("max conns %d ambiguous audit events=%d, want %d, err=%v", maxConns, count, wantCount, err)
			}
		})
	}
}

type auditFailureMetrics struct{ count int }

func (*auditFailureMetrics) LoginValidationFailed(string, string) {}
func (m *auditFailureMetrics) LoginAuditWriteFailed(string)       { m.count++ }

func (f *loginFixture) testAuditWriteFailure(t *testing.T) {
	closed, err := pgxpool.New(context.Background(), f.dsn)
	if err != nil {
		t.Fatal(err)
	}
	closed.Close()
	metrics := &auditFailureMetrics{}
	var logs bytes.Buffer
	service := *f.service
	service.DB = closed
	service.Metrics = metrics
	service.Logger = slog.New(slog.NewTextHandler(&logs, nil))
	_, err = service.Login(context.Background(), LoginInput{End: telegram.Driver, InitData: "%zz", IP: "198.51.100.14"})
	var appErr *httpx.AppError
	if !errorsAs(err, &appErr) || appErr.Code != httpx.CodeTelegramLoginFailed {
		t.Fatalf("response changed after audit write failure: %v", err)
	}
	if metrics.count != 1 {
		t.Fatalf("audit write failure metric=%d, want 1", metrics.count)
	}
	if !strings.Contains(logs.String(), "login audit write failed") {
		t.Fatalf("audit write failure was not logged: %q", logs.String())
	}
}

func bindTestMerchant(t *testing.T, ctx context.Context, pool *pgxpool.Pool, accountPublicID string) int64 {
	t.Helper()
	merchantID := insertTestMerchant(t, ctx, pool)
	if _, err := pool.Exec(ctx, `UPDATE iam.account SET merchant_id=$1 WHERE public_id=$2`, merchantID, accountPublicID); err != nil {
		t.Fatal(err)
	}
	return merchantID
}

func insertTestMerchant(t *testing.T, ctx context.Context, pool *pgxpool.Pool) int64 {
	t.Helper()
	var merchantID int64
	if err := pool.QueryRow(ctx, `INSERT INTO iam.merchant(public_id,name) VALUES($1,'{}') RETURNING id`, mustULID(t)).Scan(&merchantID); err != nil {
		t.Fatal(err)
	}
	return merchantID
}

func mustULID(t *testing.T) string {
	t.Helper()
	value, err := ids.NewULID()
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func validatorAt(now time.Time) telegram.Validator {
	v := telegram.NewValidator(24 * time.Hour)
	v.Now = func() time.Time { return now }
	return v
}

func (f *loginFixture) seedBots(t *testing.T, ctx context.Context) {
	t.Helper()
	for end, tokens := range f.tokens {
		for i, token := range tokens {
			role, status := "primary", "active"
			if i > 0 {
				role, status = "standby", "banned"
			}
			publicID, _ := ids.NewULID()
			username := fmt.Sprintf("%s_%d", end, i)
			var id int64
			if err := f.pool.QueryRow(ctx, `INSERT INTO iam.bot(public_id,"end",username,role_in_pool,status,token_enc,webhook_secret_enc,priority) VALUES($1,$2,$3,$4,$5,'\\x01','\\x02',$6) RETURNING id`, publicID, string(end), username, role, status, i).Scan(&id); err != nil {
				t.Fatal(err)
			}
			encrypted, err := f.envelope.Encrypt(ctx, []byte(token), fmt.Sprintf("bot_token:%d", id))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := f.pool.Exec(ctx, `UPDATE iam.bot SET token_enc=$1 WHERE id=$2`, encrypted, id); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func (f *loginFixture) testPool(t *testing.T) {
	ctx := context.Background()
	verifier := f.service.Verifier
	rawBuyer := signInit(f.now, 7001, "buyer-primary-token", "channel_1")
	result, err := verifier.Verify(ctx, telegram.Buyer, rawBuyer)
	if err != nil || result.TriedCount != 1 || result.ValidByBotID == nil {
		t.Fatalf("buyer verify=%+v %v", result, err)
	}
	for _, end := range []telegram.End{telegram.Merchant, telegram.Driver} {
		if _, err := verifier.Verify(ctx, end, rawBuyer); err == nil {
			t.Fatalf("buyer token accepted by %s", end)
		}
	}
	standby := signInit(f.now, 7002, "buyer-standby-token", "fallback")
	result, err = verifier.Verify(ctx, telegram.Buyer, standby)
	if err != nil || result.TriedCount != 2 || result.ValidByBotID == nil {
		t.Fatalf("standby verify=%+v %v", result, err)
	}
	if _, err := f.pool.Exec(ctx, `DELETE FROM iam.bot WHERE "end"='buyer' AND role_in_pool='primary'`); err != nil {
		t.Fatal(err)
	}
	f.source.Invalidate(telegram.Buyer)
	result, err = verifier.Verify(ctx, telegram.Buyer, standby)
	if err != nil || result.TriedCount != 1 || result.ValidByBotID == nil {
		t.Fatalf("banned standby verify=%+v %v", result, err)
	}
	// A limited token is also retained as a valid old entry.
	if _, err := f.pool.Exec(ctx, `UPDATE iam.bot SET status='limited' WHERE id=$1`, *result.ValidByBotID); err != nil {
		t.Fatal(err)
	}
	f.source.Invalidate(telegram.Buyer)
	if _, err := verifier.Verify(ctx, telegram.Buyer, standby); err != nil {
		t.Fatalf("limited bot rejected: %v", err)
	}
}

func (f *loginFixture) testConcurrentLogin(t *testing.T) {
	raw := signInit(f.now, 8001, "buyer-standby-token", "campaign")
	const count = 100
	results := make(chan LoginResult, count)
	errs := make(chan error, count)
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := f.service.Login(context.Background(), LoginInput{End: telegram.Buyer, InitData: raw, DeviceID: "device_1", IP: "198.51.100.10"})
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
		t.Errorf("login: %v", err)
	}
	var account string
	newCount := 0
	for result := range results {
		if account == "" {
			account = result.Account.PublicID
		}
		if result.Account.PublicID != account {
			t.Errorf("different account %q", result.Account.PublicID)
		}
		if result.IsNewAccount {
			newCount++
		}
	}
	if newCount != 1 {
		t.Fatalf("new account responses=%d", newCount)
	}
	for query, want := range map[string]int{`SELECT count(*) FROM iam.person WHERE tg_user_id=8001`: 1, `SELECT count(*) FROM iam.account a JOIN iam.person p ON p.id=a.person_id WHERE p.tg_user_id=8001`: 1, `SELECT count(*) FROM iam.login_event e JOIN iam.person p ON p.id=e.person_id WHERE p.tg_user_id=8001 AND e.result='ok'`: count} {
		var got int
		if err := f.pool.QueryRow(context.Background(), query).Scan(&got); err != nil || got != want {
			t.Fatalf("%s = %d/%v want %d", query, got, err, want)
		}
	}
	var deviceCount int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM iam.account_device d JOIN iam.account a ON a.id=d.account_id WHERE a.public_id=$1 AND d.device_id='device_1'`, account).Scan(&deviceCount); err != nil || deviceCount != 1 {
		t.Fatalf("device count=%d err=%v", deviceCount, err)
	}
	var writeAccess bool
	if err := f.pool.QueryRow(context.Background(), `SELECT bool_or(write_access) FROM iam.account_bot b JOIN iam.account a ON a.id=b.account_id WHERE a.public_id=$1`, account).Scan(&writeAccess); err != nil || !writeAccess {
		t.Fatalf("write access=%v err=%v", writeAccess, err)
	}
	if _, err := f.pool.Exec(context.Background(), `UPDATE iam.account SET lang='km',lang_registered_at=now() WHERE public_id=$1`, account); err != nil {
		t.Fatal(err)
	}
	again, err := f.service.Login(context.Background(), LoginInput{End: telegram.Buyer, InitData: raw, IP: "198.51.100.10"})
	if err != nil || again.Account.Lang == nil || *again.Account.Lang != "km" {
		t.Fatalf("registered language overwritten: %+v %v", again.Account, err)
	}
	var activeSessions int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM iam.session s JOIN iam.account a ON a.id=s.account_id WHERE a.public_id=$1 AND s.revoked_at IS NULL`, account).Scan(&activeSessions); err != nil || activeSessions != 5 {
		t.Fatalf("active sessions=%d err=%v, want 5", activeSessions, err)
	}
	var kyc *string
	driverRaw := signInit(f.now, 8002, "driver-primary-token", "")
	if _, err := f.service.Login(context.Background(), LoginInput{End: telegram.Driver, InitData: driverRaw, IP: "198.51.100.11"}); err != nil {
		t.Fatal(err)
	}
	if err := f.pool.QueryRow(context.Background(), `SELECT a.kyc_status FROM iam.account a JOIN iam.person p ON p.id=a.person_id WHERE p.tg_user_id=8002`).Scan(&kyc); err != nil || kyc == nil || *kyc != "pending" {
		t.Fatalf("driver kyc=%v %v", kyc, err)
	}
}

func (f *loginFixture) testFailureEvents(t *testing.T) {
	ctx := context.Background()
	cases := []struct{ name, raw, result string }{
		{"invalid signature", signInit(f.now, 9051, "not-a-pool-token", ""), "invalid_signature"},
		{"expired", signInit(f.now.Add(-25*time.Hour), 9052, "driver-primary-token", ""), "expired"},
		{"malformed", "%zz", "malformed"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := f.service.Login(ctx, LoginInput{End: telegram.Driver, InitData: tc.raw, IP: "203.0.113.50"}); err == nil {
				t.Fatal("failure accepted")
			}
			var count int
			if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM iam.login_event WHERE result=$1`, tc.result).Scan(&count); err != nil || count < 1 {
				t.Fatalf("result %s count=%d err=%v", tc.result, count, err)
			}
		})
	}
}

func (f *loginFixture) testBlockedAndEvents(t *testing.T) {
	ctx := context.Background()
	raw := signInit(f.now, 9001, "merchant-primary-token", "invite_1")
	first, err := f.service.Login(ctx, LoginInput{End: telegram.Merchant, InitData: raw, IP: "203.0.113.9"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `UPDATE iam.account SET status='frozen',status_reason='test' WHERE public_id=$1`, first.Account.PublicID); err != nil {
		t.Fatal(err)
	}
	_, err = f.service.Login(ctx, LoginInput{End: telegram.Merchant, InitData: raw, IP: "203.0.113.9"})
	var appErr *httpx.AppError
	if !errorsAs(err, &appErr) || appErr.Code != httpx.CodeAccountBlocked {
		t.Fatalf("blocked error=%v", err)
	}
	var count int
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM iam.login_event WHERE result='account_blocked' AND start_param='invite_1'`).Scan(&count); err != nil || count != 1 {
		t.Fatalf("blocked events=%d %v", count, err)
	}
	secretParts := []string{raw, "merchant-primary-token", first.AccessToken, first.RefreshToken}
	rows, err := f.pool.Query(ctx, `SELECT row_to_json(e)::text FROM iam.login_event e`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var value string
		_ = rows.Scan(&value)
		for _, secret := range secretParts {
			if secret != "" && strings.Contains(value, secret) {
				t.Fatal("login_event leaked secret")
			}
		}
	}
	if strings.Contains(errString(err), "merchant-primary-token") || strings.Contains(errString(err), raw) {
		t.Fatal("error leaked secret")
	}
}

func (f *loginFixture) testFailureBan(t *testing.T) {
	guard := &FailureGuard{Limiter: gateway.NewMemoryLimiter(), Rule: gateway.Rule{Limit: 2, Window: 30 * time.Millisecond}}
	original := f.service.Failures
	f.service.Failures = guard
	defer func() { f.service.Failures = original }()
	input := LoginInput{End: telegram.Driver, InitData: signInit(f.now, 9100, "wrong-token", ""), IP: "192.0.2.88"}
	for i := 0; i < 2; i++ {
		_, err := f.service.Login(context.Background(), input)
		var appErr *httpx.AppError
		if !errorsAs(err, &appErr) || appErr.Code != httpx.CodeTelegramLoginFailed {
			t.Fatalf("failure %d=%v", i, err)
		}
	}
	_, err := f.service.Login(context.Background(), input)
	var appErr *httpx.AppError
	if !errorsAs(err, &appErr) || appErr.Code != httpx.CodeLoginTooManyAttempts {
		t.Fatalf("ban=%v", err)
	}
	correct := LoginInput{End: telegram.Driver, InitData: signInit(f.now, 9100, "driver-primary-token", ""), IP: input.IP}
	if _, err := f.service.Login(context.Background(), correct); !errorsAs(err, &appErr) || appErr.Code != httpx.CodeLoginTooManyAttempts {
		t.Fatalf("correct during ban=%v", err)
	}
	time.Sleep(40 * time.Millisecond)
	if _, err := f.service.Login(context.Background(), correct); err != nil {
		t.Fatalf("after expiry=%v", err)
	}
	var n int
	if err := f.pool.QueryRow(context.Background(), `SELECT count(*) FROM iam.login_event WHERE result='rate_limited'`).Scan(&n); err != nil || n < 2 {
		t.Fatalf("rate events=%d %v", n, err)
	}
}

func (f *loginFixture) testRefreshNotStored(t *testing.T) {
	result, err := f.service.Login(context.Background(), LoginInput{End: telegram.Driver, InitData: signInit(f.now, 9200, "driver-primary-token", ""), IP: "192.0.2.92"})
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	if err := f.pool.QueryRow(context.Background(), `SELECT EXISTS(SELECT 1 FROM iam.session WHERE encode(refresh_token_hash,'escape') LIKE '%'||$1||'%')`, result.RefreshToken).Scan(&found); err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("refresh plaintext stored")
	}
}

func signInit(at time.Time, userID int64, token, start string) string {
	values := url.Values{"auth_date": {strconv.FormatInt(at.Unix(), 10)}, "query_id": {"q"}, "user": {fmt.Sprintf(`{"id":%d,"first_name":"Test","username":"user","language_code":"en","allows_write_to_pm":true}`, userID)}}
	if start != "" {
		values.Set("start_param", start)
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	lines := make([]string, 0, len(keys))
	for _, key := range keys {
		lines = append(lines, key+"="+values.Get(key))
	}
	secret := hmac.New(sha256.New, []byte("WebAppData"))
	_, _ = secret.Write([]byte(token))
	digest := hmac.New(sha256.New, secret.Sum(nil))
	_, _ = digest.Write([]byte(strings.Join(lines, "\n")))
	values.Set("hash", hex.EncodeToString(digest.Sum(nil)))
	return values.Encode()
}

func errorsAs(err error, target any) bool { return errors.As(err, target) }
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
