package queue_test

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

func TestIdempotentCommitsBusinessOnceAndRollsBackFailure(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("queue_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	defer db.Close()
	if err := goose.SetDialect("postgres"); err != nil {
		t.Fatal(err)
	}
	if err := goose.Up(db, migrations(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `CREATE TABLE ops.effect(event_id text PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}

	var calls atomic.Int32
	handler := queue.Idempotent("queue.test", queue.TransactionalHandler{DB: pool, Handle: func(ctx context.Context, tx pgx.Tx, eventID string) error {
		calls.Add(1)
		_, err := tx.Exec(ctx, `INSERT INTO ops.effect(event_id) VALUES($1)`, eventID)
		return err
	}})
	if err := handler(ctx, "evt-1"); err != nil {
		t.Fatal(err)
	}
	if err := handler(ctx, "evt-1"); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 {
		t.Fatalf("business calls=%d, want 1", calls.Load())
	}
	assertCount(t, ctx, pool, `SELECT count(*) FROM ops.processed_event`, 1)
	assertCount(t, ctx, pool, `SELECT count(*) FROM ops.effect`, 1)

	failing := queue.Idempotent("queue.test", queue.TransactionalHandler{DB: pool, Handle: func(context.Context, pgx.Tx, string) error { return errors.New("business failure") }})
	if err := failing(ctx, "evt-2"); err == nil {
		t.Fatal("failure handler succeeded")
	}
	assertCount(t, ctx, pool, `SELECT count(*) FROM ops.processed_event WHERE event_id='evt-2'`, 0)
}

func assertCount(t *testing.T, ctx context.Context, db *pgxpool.Pool, query string, want int) {
	t.Helper()
	var got int
	if err := db.QueryRow(ctx, query).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("count=%d, want %d", got, want)
	}
}

func migrations(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate test")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "migrations")
}
