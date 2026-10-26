package iam

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
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
	f := &loginFixture{pool: pool, envelope: envelope, keys: jwtRing, now: now, tokens: map[telegram.End][]string{
		telegram.Buyer: {"buyer-primary-token", "buyer-standby-token"}, telegram.Merchant: {"merchant-primary-token"}, telegram.Driver: {"driver-primary-token"},
	}}
	f.seedBots(t, ctx)
	f.source = &telegram.DBPoolSource{DB: pool, Envelope: envelope, TTL: 5 * time.Minute, Now: func() time.Time { return now }}
	manager := TokenManager{Keys: jwtRing, AccessTTL: 15 * time.Minute, Now: func() time.Time { return now }}
	f.cache = newTestSessionCache()
	f.service = &Service{DB: pool, Verifier: telegram.PoolVerifier{Validator: validatorAt(now), Source: f.source}, Tokens: manager, RefreshTTL: 7 * 24 * time.Hour, IdleTTL: 24 * time.Hour, MaxSessions: 5, SessionCache: f.cache, Now: func() time.Time { return now }}

	t.Run("pool isolation status and fallback", f.testPool)
	t.Run("one hundred concurrent first logins", f.testConcurrentLogin)
	t.Run("blocked account and safe events", f.testBlockedAndEvents)
	t.Run("validation failures are classified", f.testFailureEvents)
	t.Run("failure ban expires", f.testFailureBan)
	t.Run("refresh plaintext is absent", f.testRefreshNotStored)
	t.Run("refresh rotation concurrency reuse and revocation", f.testSessionLifecycle)
	t.Run("session ownership cache rebuild and fail policy", f.testSessionAccess)
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
