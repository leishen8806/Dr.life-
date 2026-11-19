package iam

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/ids"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
	"github.com/pquerna/otp/totp"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestBootstrapIntegrationAtomicConcurrencyAndSecretBoundaries(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("drlife_bootstrap_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	_, migrationFile, _, _ := runtime.Caller(0)
	if err := goose.Up(database, filepath.Join(filepath.Dir(migrationFile), "..", "..", "migrations")); err != nil {
		t.Fatal(err)
	}
	// Run the service with the same least-privilege login shape used in
	// production: iam_rw for IAM tables and audit_writer for append/function
	// execution. The migration user remains the function owner, so the
	// SECURITY DEFINER guard sees distinct current_user and session_user.
	const login = "app_iam_bootstrap_test"
	if _, err := database.Exec(`CREATE ROLE app_iam_bootstrap_test LOGIN PASSWORD 'test-only'; GRANT iam_rw TO app_iam_bootstrap_test; GRANT audit_writer TO app_iam_bootstrap_test`); err != nil {
		t.Fatal(err)
	}
	loginURL, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	loginURL.User = url.UserPassword(login, "test-only")
	pool, err := pgxpool.New(ctx, loginURL.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	key := make([]byte, secrets.KeySize)
	ring, err := secrets.NewKeyring(map[string][]byte{"test": key}, "test")
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := secrets.NewEnvelope(ring)
	if err != nil {
		t.Fatal(err)
	}
	service := &BootstrapService{DB: pool, Cipher: envelope, Audit: &platformaudit.Store{DB: pool}}
	if _, err := pool.Exec(ctx, `UPDATE audit.chain_head SET last_id=last_id WHERE singleton`); err == nil {
		t.Fatal("bootstrap login can directly update audit chain head")
	}

	type bootstrapCall struct {
		result BootstrapAdminResult
		err    error
	}
	results := make(chan bootstrapCall, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			result, callErr := service.CreateFirstSuper(ctx, "bootstrap-owner", "Owner@Example.test")
			results <- bootstrapCall{result: result, err: callErr}
		}()
	}
	group.Wait()
	close(results)
	var successes int
	var bootstrapResult BootstrapAdminResult
	for call := range results {
		if call.err != nil {
			t.Logf("bootstrap call failed: %v", call.err)
		}
		if call.err == nil {
			successes++
			bootstrapResult = call.result
		}
	}
	if successes != 1 {
		t.Fatalf("bootstrap successes=%d, want exactly one", successes)
	}
	var adminID int64
	var storedHash string
	var storedTOTP []byte
	if err := pool.QueryRow(ctx, `SELECT id,password_hash,totp_secret_enc FROM iam.admin_user WHERE name=$1`, "bootstrap-owner").Scan(&adminID, &storedHash, &storedTOTP); err != nil {
		t.Fatal(err)
	}
	if storedHash == "" || len(storedTOTP) == 0 {
		t.Fatal("bootstrap credentials were not persisted as hash and ciphertext")
	}
	var storedEmail string
	if err := pool.QueryRow(ctx, `SELECT email FROM iam.admin_user WHERE id=$1`, adminID).Scan(&storedEmail); err != nil {
		t.Fatal(err)
	}
	if storedEmail != "owner@example.test" || bootstrapResult.Admin.Email != storedEmail {
		t.Fatalf("bootstrap email=%q result=%q, want normalized owner@example.test", storedEmail, bootstrapResult.Admin.Email)
	}
	var action, detail string
	if err := pool.QueryRow(ctx, `SELECT action,detail::text FROM audit.log WHERE actor_id=$1 ORDER BY id DESC LIMIT 1`, adminID).Scan(&action, &detail); err != nil {
		t.Fatal(err)
	}
	if action != string(platformaudit.ActionAdminBootstrap) {
		t.Fatalf("bootstrap audit action=%q", action)
	}
	var fields map[string]any
	if err := json.Unmarshal([]byte(detail), &fields); err != nil {
		t.Fatal(err)
	}
	if _, ok := fields["password"]; ok {
		t.Fatal("bootstrap audit detail contains password")
	}

	// Exercise the legacy repair path, including duplicate rejection and the
	// end-to-end password/TOTP login using the repaired address.
	legacyEmail := "legacy-owner@bootstrap.invalid"
	if _, err := pool.Exec(ctx, `UPDATE iam.admin_user SET email=$1 WHERE id=$2`, legacyEmail, adminID); err != nil {
		t.Fatal(err)
	}
	otherID, err := ids.NewULID()
	if err != nil {
		t.Fatal(err)
	}
	otherHash, err := HashPassword("unused-test-password")
	if err != nil {
		t.Fatal(err)
	}
	otherSecret, err := EncryptTOTPSecret(ctx, envelope, otherID, "JBSWY3DPEHPK3PXP")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO iam.admin_user(public_id,name,email,password_hash,totp_secret_enc,role,status) VALUES($1,'Other Super','other@example.test',$2,$3,'super','active')`, otherID, otherHash, otherSecret); err != nil {
		t.Fatal(err)
	}
	if err := service.FixSuperEmail(ctx, "bootstrap-owner", "other@example.test"); err == nil {
		t.Fatal("repair accepted an email already used by another administrator")
	}
	if err := service.FixSuperEmail(ctx, "bootstrap-owner", "Owner@Example.test"); err != nil {
		t.Fatal(err)
	}
	if err := service.FixSuperEmail(ctx, "bootstrap-owner", "owner@example.test"); err != nil {
		t.Fatal("idempotent repair failed: ", err)
	}
	var repairedEmail string
	if err := pool.QueryRow(ctx, `SELECT email FROM iam.admin_user WHERE id=$1`, adminID).Scan(&repairedEmail); err != nil {
		t.Fatal(err)
	}
	if repairedEmail != "owner@example.test" {
		t.Fatalf("repaired email=%q", repairedEmail)
	}
	var repairDetail string
	if err := pool.QueryRow(ctx, `SELECT detail::text FROM audit.log WHERE target=$1 ORDER BY id DESC LIMIT 1`, "admin:"+bootstrapResult.Admin.PublicID).Scan(&repairDetail); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(repairDetail, "email 已修正") || strings.Contains(repairDetail, "owner@example.test") {
		t.Fatalf("repair audit detail=%q", repairDetail)
	}
	secret, err := envelope.Decrypt(ctx, storedTOTP, fmt.Sprintf("admin_totp:%s", bootstrapResult.Admin.PublicID))
	if err != nil {
		t.Fatal(err)
	}
	code, err := totp.GenerateCode(string(secret), time.Now())
	if err != nil {
		t.Fatal(err)
	}
	adminLogin := &AdminLoginService{DB: pool, Tokens: TokenManager{Keys: ring, AccessTTL: 10 * time.Minute}, Cipher: envelope, RefreshTTL: 8 * time.Hour, IdleTTL: 15 * time.Minute, Security: &testAdminSecurity{}}
	loginResult, err := adminLogin.Login(ctx, AdminLoginInput{Email: "owner@example.test", Password: bootstrapResult.InitialPassword, TOTPCode: code, IP: "198.51.100.40", DeviceID: "web:bootstrap", RequestID: "bootstrap-login-test"})
	if err != nil || loginResult.AccessToken == "" || loginResult.RefreshToken == "" {
		t.Fatalf("repaired bootstrap login result=%+v err=%v", loginResult, err)
	}

	if err := service.RegisterBot(ctx, BootstrapBotInput{End: "buyer", Username: "bootstrap-buyer", Actor: "", Token: "test-bot-token-1234"}); err == nil {
		t.Fatal("bot registration without actor succeeded")
	}
	var actorPublicID string
	if err := pool.QueryRow(ctx, `SELECT public_id FROM iam.admin_user WHERE id=$1`, adminID).Scan(&actorPublicID); err != nil {
		t.Fatal(err)
	}
	if err := service.RegisterBot(ctx, BootstrapBotInput{End: "buyer", Username: "bootstrap-buyer", Actor: actorPublicID, Token: "test-bot-token-1234"}); err != nil {
		t.Fatal(err)
	}
	var tokenLast4 string
	var tokenCipher []byte
	if err := pool.QueryRow(ctx, `SELECT token_last4,token_enc FROM iam.bot WHERE username=$1`, "bootstrap-buyer").Scan(&tokenLast4, &tokenCipher); err != nil {
		t.Fatal(err)
	}
	if tokenLast4 != "1234" || string(tokenCipher) == "test-bot-token-1234" {
		t.Fatalf("bot secret boundary last4=%q plaintext=%q", tokenLast4, tokenCipher)
	}
	var botID int64
	var webhookCipher []byte
	if err := pool.QueryRow(ctx, `SELECT id,webhook_secret_enc FROM iam.bot WHERE username=$1`, "bootstrap-buyer").Scan(&botID, &webhookCipher); err != nil {
		t.Fatal(err)
	}
	if _, err := envelope.Decrypt(ctx, webhookCipher, fmt.Sprintf("bot_token:%d", botID)); err == nil {
		t.Fatal("webhook secret decrypted with token AAD")
	}
	if _, err := envelope.Decrypt(ctx, webhookCipher, fmt.Sprintf("bot_webhook_secret:%d", botID)); err != nil {
		t.Fatalf("webhook secret did not decrypt with dedicated AAD: %v", err)
	}
	var actionRole string
	if err := pool.QueryRow(ctx, `SELECT actor_role FROM audit.log WHERE target LIKE 'bot:%' ORDER BY id DESC LIMIT 1`).Scan(&actionRole); err != nil {
		t.Fatal(err)
	}
	if actionRole != "system_bootstrap" {
		t.Fatalf("bot registration audit actor_role=%q", actionRole)
	}
	if err := service.RegisterBot(ctx, BootstrapBotInput{End: "buyer", Username: "bootstrap-buyer", Actor: actorPublicID, Token: "test-bot-token-5678"}); err == nil {
		t.Fatal("duplicate bot username succeeded")
	}
}
