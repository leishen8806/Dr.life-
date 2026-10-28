package outbox_test

import (
	"bytes"
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue/outbox"
	"github.com/pressly/goose/v3"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func TestReconAlertsRelayToDedicatedHandlersEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	database := newRelayPostgres(t, ctx)
	defer database.Close()
	redisURL := newRelayRedis(t, ctx)

	var ids [2]int64
	for i, topic := range []string{"ledger.recon.failed", "ledger.recon.stale_run"} {
		if err := database.QueryRow(ctx, `INSERT INTO ledger.outbox(topic,payload) VALUES($1,'{}'::jsonb) RETURNING id`, topic).Scan(&ids[i]); err != nil {
			t.Fatal(err)
		}
	}
	options, err := asynq.ParseRedisURI(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	client := asynq.NewClient(options)
	defer client.Close()
	relay, err := outbox.New(ledger.New(database, ledger.Options{}), client, outbox.Options{Routes: outbox.LedgerRoutes()})
	if err != nil {
		t.Fatal(err)
	}
	if count, err := relay.RunOnce(ctx); err != nil || count != 2 {
		t.Fatalf("relay count=%d err=%v", count, err)
	}

	registry := queue.NewRegistry()
	if err := outbox.RegisterLedgerTasks(registry); err != nil {
		t.Fatal(err)
	}
	observer := &relayObserver{counts: make(map[string]int)}
	var logs bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&logs, nil))
	worker, err := queue.NewWorker(redisURL, registry, queue.WorkerOptions{Logger: logger, Observer: observer, ShutdownTimeout: time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if err := outbox.RegisterLedgerHandlers(worker, logger); err != nil {
		t.Fatal(err)
	}
	workerCtx, stop := context.WithCancel(ctx)
	defer stop()
	if err := worker.Start(workerCtx); err != nil {
		t.Fatal(err)
	}
	waitForRelayDeliveries(t, ctx, observer, logs.String)

	inspector := asynq.NewInspector(options)
	defer inspector.Close()
	completed, err := inspector.GetQueueInfo(queue.QueueCritical)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Pending != 0 || completed.Active != 0 || completed.Processed != 2 || completed.Failed != 0 {
		t.Fatalf("critical queue pending=%d active=%d processed=%d failed=%d, want 0/0/2/0", completed.Pending, completed.Active, completed.Processed, completed.Failed)
	}
	t.Logf("inspector critical pending=%d active=%d processed=%d failed=%d", completed.Pending, completed.Active, completed.Processed, completed.Failed)
	if got := observer.Count("ops:outbox_unknown:v1"); got != 0 {
		t.Fatalf("unknown outbox tasks=%d, want 0", got)
	}
	t.Logf("worker observed ops:outbox_unknown:v1=%d", observer.Count("ops:outbox_unknown:v1"))
	for _, delivery := range []struct{ topic, taskType string }{
		{"ledger.recon.failed", outbox.TaskReconFailed},
		{"ledger.recon.stale_run", outbox.TaskReconStale},
	} {
		if got := observer.Count(delivery.taskType); got != 1 {
			t.Fatalf("handler %s executions=%d, want 1", delivery.taskType, got)
		}
		t.Logf("handler received topic=%s task_type=%s", delivery.topic, delivery.taskType)
	}
	if logsText := logs.String(); !bytes.Contains([]byte(logsText), []byte("ledger.recon.failed")) || !bytes.Contains([]byte(logsText), []byte("ledger.recon.stale_run")) {
		t.Fatalf("handler logs missing reconciliation topics: %s", logsText)
	}
	var sent int
	if err := database.QueryRow(ctx, `SELECT count(*) FROM ledger.outbox WHERE id=ANY($1) AND sent_at IS NOT NULL`, ids[:]).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if sent != len(ids) {
		t.Fatalf("sent rows=%d, want %d", sent, len(ids))
	}
}

type relayObserver struct {
	mu     sync.Mutex
	counts map[string]int
}

func (o *relayObserver) ObserveTask(_ string, taskType string, _ time.Duration, _ error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.counts[taskType]++
}

func (o *relayObserver) Count(taskType string) int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.counts[taskType]
}

func waitForRelayDeliveries(t *testing.T, ctx context.Context, observer *relayObserver, logs func() string) {
	t.Helper()
	deadline := time.NewTimer(20 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(20 * time.Millisecond)
	defer tick.Stop()
	for {
		if observer.Count(outbox.TaskReconFailed) == 1 && observer.Count(outbox.TaskReconStale) == 1 {
			return
		}
		select {
		case <-ctx.Done():
			t.Fatalf("worker context ended before reconciliation deliveries: %v; logs=%s", ctx.Err(), logs())
		case <-deadline.C:
			t.Fatalf("timed out waiting for reconciliation deliveries; logs=%s", logs())
		case <-tick.C:
		}
	}
}

func newRelayPostgres(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()
	container, err := postgres.Run(ctx, "postgres:16-alpine", postgres.WithDatabase("relay_test"), postgres.WithUsername("test_admin"), postgres.WithPassword("test-only"), postgres.BasicWaitStrategies())
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
	if err := goose.Up(db, relayMigrations(t)); err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	return pool
}

func newRelayRedis(t *testing.T, ctx context.Context) string {
	t.Helper()
	container, err := tcredis.Run(ctx, "redis:7")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })
	url, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return url
}

func relayMigrations(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("locate relay integration test")
	}
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "..", "migrations")
}
