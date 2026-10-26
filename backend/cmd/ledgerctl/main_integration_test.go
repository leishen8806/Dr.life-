package main

import (
	"bytes"
	"context"
	"database/sql"
	"net/url"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestLedgerctlCommandsIntegration(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("ledgerctl_test"), postgres.WithUsername("test_admin"),
		postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate postgres: %v", err)
		}
	})
	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatal(err)
	}
	database, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(database, "../../migrations"); err != nil {
		t.Fatal(err)
	}
	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, `CREATE ROLE app_ledger LOGIN PASSWORD 'test-only'; GRANT ledger_rw TO app_ledger`); err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	parsed.User = url.UserPassword("app_ledger", "test-only")
	pool, err := pgxpool.New(ctx, parsed.String())
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	service := ledger.New(pool, ledger.Options{})
	day := time.Now().AddDate(0, 0, -1)
	var stdout, stderr bytes.Buffer
	if code := execute(ctx, command{name: "snapshot", day: day}, service, &stdout, &stderr); code != 0 {
		t.Fatalf("snapshot exit=%d stderr=%s", code, stderr.String())
	}
	if _, err := admin.Exec(ctx, `UPDATE ledger.balance SET balance=1 WHERE account_id=(SELECT id FROM ledger.account WHERE kind='BANK_DEDICATED')`); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	stderr.Reset()
	if code := execute(ctx, command{name: "recon", day: day}, service, &stdout, &stderr); code != 1 {
		t.Fatalf("failed recon exit=%d stdout=%s stderr=%s", code, stdout.String(), stderr.String())
	}
	stdout.Reset()
	if code := execute(ctx, command{name: "freeze-list"}, service, &stdout, &stderr); code != 0 {
		t.Fatalf("freeze list exit=%d stderr=%s", code, stderr.String())
	}
	freezes, err := service.ListPayoutFreezes(ctx, true)
	if err != nil || len(freezes) == 0 {
		t.Fatalf("freezes=%+v err=%v", freezes, err)
	}
	stdout.Reset()
	if code := execute(ctx, command{name: "freeze-release", freezeID: freezes[0].ID, releasedBy: 99, reason: "integration verified"}, service, &stdout, &stderr); code != 0 {
		t.Fatalf("freeze release exit=%d stderr=%s", code, stderr.String())
	}
}
