package iam

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	platformaudit "github.com/leishen8806/Dr.life-/backend/internal/platform/audit"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/secrets"
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
	// Production owns the SECURITY DEFINER chain-head function with the
	// dedicated audit role. The disposable migration user owns it by default,
	// which makes current_user == session_user and trips the fail-closed guard.
	if _, err := database.Exec(`ALTER FUNCTION audit.advance_chain_head(bytea,bigint,bytea) OWNER TO audit_writer`); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
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

	results := make(chan error, 2)
	var group sync.WaitGroup
	for range 2 {
		group.Add(1)
		go func() {
			defer group.Done()
			_, callErr := service.CreateFirstSuper(ctx, "bootstrap-owner")
			results <- callErr
		}()
	}
	group.Wait()
	close(results)
	var successes int
	for callErr := range results {
		if callErr != nil {
			t.Logf("bootstrap call failed: %v", callErr)
		}
		if callErr == nil {
			successes++
		}
	}
	if successes != 1 {
		t.Fatalf("bootstrap successes=%d, want exactly one", successes)
	}
	var adminID int64
	var storedHash, storedTOTP string
	if err := pool.QueryRow(ctx, `SELECT id,password_hash,encode(totp_secret_enc,'hex') FROM iam.admin_user WHERE name=$1`, "bootstrap-owner").Scan(&adminID, &storedHash, &storedTOTP); err != nil {
		t.Fatal(err)
	}
	if storedHash == "" || storedTOTP == "" {
		t.Fatal("bootstrap credentials were not persisted as hash and ciphertext")
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
