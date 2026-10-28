package outbox_test

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/leishen8806/Dr.life-/backend/internal/comms/telegram"
	"github.com/leishen8806/Dr.life-/backend/internal/ledger"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/opsalert"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue/outbox"
	"github.com/pressly/goose/v3"
	"github.com/redis/go-redis/v9"
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
	for i, event := range []struct{ topic, payload string }{
		{"ledger.recon.failed", `{"run_id":7,"day":"2026-10-27","counts":{"BALANCE_CACHE":2},"account_ids":[9988],"amount":12345}`},
		{"ledger.recon.stale_run", `{"run_id":8,"day":"2026-10-27","account_ids":[9989]}`},
	} {
		if err := database.QueryRow(ctx, `INSERT INTO ledger.outbox(topic,payload) VALUES($1,$2::jsonb) RETURNING id`, event.topic, event.payload).Scan(&ids[i]); err != nil {
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
	telegramServer, messages := relayTelegramServer(t)
	defer telegramServer.Close()
	alertRedisOptions, err := redis.ParseURL(redisURL)
	if err != nil {
		t.Fatal(err)
	}
	alertRedis := redis.NewClient(alertRedisOptions)
	defer alertRedis.Close()
	alerts := opsalert.NewReconciliationHandler(opsalert.Options{
		Sender: telegram.NewHTTPSender(telegramServer.URL, telegram.StaticTokenProvider{"ops": "test-token"}, telegram.SenderOptions{}),
		BotID:  "ops", ChatID: -1001, Environment: "test", MergeWindow: time.Nanosecond, Logger: logger, Deduper: opsalert.NewRedisDeduper(alertRedis),
	})
	if err := outbox.RegisterLedgerHandlersWithAlerts(worker, logger, alerts); err != nil {
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
	var sent int
	if err := database.QueryRow(ctx, `SELECT count(*) FROM ledger.outbox WHERE id=ANY($1) AND sent_at IS NOT NULL`, ids[:]).Scan(&sent); err != nil {
		t.Fatal(err)
	}
	if sent != len(ids) {
		t.Fatalf("sent rows=%d, want %d", sent, len(ids))
	}
	if got := messages(); len(got) != 2 {
		t.Fatalf("Telegram messages=%d, want 2: %v", len(got), got)
	} else if strings.Contains(strings.Join(got, " "), "9988") || strings.Contains(strings.Join(got, " "), "12345") {
		t.Fatalf("Telegram alert leaked queue-sensitive payload: %v", got)
	}
}

func relayTelegramServer(t *testing.T) (*httptest.Server, func() []string) {
	t.Helper()
	var mu sync.Mutex
	var messages []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var body struct {
			Text string `json:"text"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode Telegram request: %v", err)
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		mu.Lock()
		messages = append(messages, body.Text)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	return server, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), messages...)
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
