package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/queue"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

const testPayload = `{"phone":"+85510000001","secret":"payload-secret"}`

func startRedis(t *testing.T) string {
	t.Helper()
	ctx := context.Background()
	container, err := tcredis.Run(ctx, "redis:7")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testcontainers.TerminateContainer(container); err != nil {
			t.Errorf("terminate redis: %v", err)
		}
	})
	url, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	return url
}

func redisOption(t *testing.T, url string) asynq.RedisConnOpt {
	t.Helper()
	opt, err := asynq.ParseRedisURI(url)
	if err != nil {
		t.Fatal(err)
	}
	return opt
}

func startWorker(t *testing.T, url, queueName string, mux *asynq.ServeMux) {
	t.Helper()
	server := asynq.NewServer(redisOption(t, url), asynq.Config{Concurrency: 1, Queues: map[string]int{queueName: 1}})
	if err := server.Start(mux); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(server.Shutdown)
}

func waitArchived(t *testing.T, inspector *asynq.Inspector, queueName string, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		info, err := inspector.GetQueueInfo(queueName)
		if err != nil {
			t.Fatal(err)
		}
		if info.Archived == want {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	info, err := inspector.GetQueueInfo(queueName)
	if err != nil {
		t.Fatal(err)
	}
	t.Fatalf("archived = %d, want %d", info.Archived, want)
}

func TestStatsReportsDepthAndRedactsPayload(t *testing.T) {
	url := startRedis(t)
	opt := redisOption(t, url)
	client := asynq.NewClient(opt)
	defer client.Close()
	for i := 0; i < 2; i++ {
		if _, err := client.Enqueue(asynq.NewTask("test:stats:v1", []byte(testPayload)), asynq.Queue(queue.QueueDefault)); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(20 * time.Millisecond)

	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"stats"}, &out, &errOut, cliConfig{queueRedisURL: url})
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut.String())
	}
	var rows []queueStats
	if err := json.Unmarshal(out.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, row := range rows {
		if row.Queue == queue.QueueDefault {
			found = true
			if row.Pending != 2 {
				t.Fatalf("pending = %d, want 2", row.Pending)
			}
			if row.OldestWaitSeconds <= 0 {
				t.Fatalf("oldest wait = %v, want > 0", row.OldestWaitSeconds)
			}
		}
	}
	if !found {
		t.Fatal("default queue missing from stats")
	}
	if strings.Contains(out.String(), "payload-secret") || strings.Contains(out.String(), "+85510000001") {
		t.Fatalf("payload leaked: %s", out.String())
	}
}

func TestPauseAndResume(t *testing.T) {
	url := startRedis(t)
	received := make(chan struct{}, 1)
	mux := asynq.NewServeMux()
	mux.HandleFunc("test:run:v1", func(context.Context, *asynq.Task) error {
		received <- struct{}{}
		return nil
	})
	startWorker(t, url, queue.QueueDefault, mux)
	client := asynq.NewClient(redisOption(t, url))
	defer client.Close()

	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"pause", "--queue", queue.QueueDefault}, &out, &errOut, cliConfig{queueRedisURL: url})
	if code != 0 {
		t.Fatalf("pause exit = %d, stderr = %s", code, errOut.String())
	}
	if _, err := client.Enqueue(asynq.NewTask("test:run:v1", nil), asynq.Queue(queue.QueueDefault)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-received:
		t.Fatal("paused queue consumed a task")
	case <-time.After(400 * time.Millisecond):
	}

	out.Reset()
	errOut.Reset()
	code = run(context.Background(), []string{"resume", "--queue", queue.QueueDefault}, &out, &errOut, cliConfig{queueRedisURL: url})
	if code != 0 {
		t.Fatalf("resume exit = %d, stderr = %s", code, errOut.String())
	}
	select {
	case <-received:
	case <-time.After(5 * time.Second):
		t.Fatal("resumed queue did not consume the task")
	}
}

func TestDeadReplayRequiresYes(t *testing.T) {
	url := startRedis(t)
	inspector := asynq.NewInspector(redisOption(t, url))
	defer inspector.Close()

	var calls atomic.Int32
	mux := asynq.NewServeMux()
	mux.HandleFunc("test:dead:v1", func(context.Context, *asynq.Task) error {
		if calls.Add(1) == 1 {
			return errors.New("transient boom")
		}
		return nil
	})
	startWorker(t, url, queue.QueueDefault, mux)
	client := asynq.NewClient(redisOption(t, url))
	defer client.Close()
	info, err := client.Enqueue(asynq.NewTask("test:dead:v1", []byte(testPayload)), asynq.Queue(queue.QueueDefault), asynq.MaxRetry(0))
	if err != nil {
		t.Fatal(err)
	}
	waitArchived(t, inspector, queue.QueueDefault, 1)

	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"dead", "list", "--queue", queue.QueueDefault}, &out, &errOut, cliConfig{queueRedisURL: url})
	if code != 0 {
		t.Fatalf("list exit = %d, stderr = %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), info.ID) || !strings.Contains(out.String(), "test:dead:v1") {
		t.Fatalf("dead list missing task identity: %s", out.String())
	}
	if strings.Contains(out.String(), "payload-secret") || strings.Contains(out.String(), "+85510000001") || strings.Contains(out.String(), "transient boom") {
		t.Fatalf("dead list leaked task content: %s", out.String())
	}

	out.Reset()
	errOut.Reset()
	code = run(context.Background(), []string{"dead", "replay", "--queue", queue.QueueDefault}, &out, &errOut, cliConfig{queueRedisURL: url})
	if code != exitUsage {
		t.Fatalf("replay without --yes exit = %d, want %d", code, exitUsage)
	}
	waitArchived(t, inspector, queue.QueueDefault, 1)

	out.Reset()
	errOut.Reset()
	code = run(context.Background(), []string{"dead", "replay", "--queue", queue.QueueDefault, "--yes"}, &out, &errOut, cliConfig{queueRedisURL: url})
	if code != 0 {
		t.Fatalf("replay exit = %d, stderr = %s", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "warning:") {
		t.Fatalf("replay did not print a warning: %s", errOut.String())
	}
	waitArchived(t, inspector, queue.QueueDefault, 0)
	if calls.Load() < 2 {
		t.Fatalf("handler calls = %d, want at least 2", calls.Load())
	}
}

func TestOutboxLag(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	container, err := postgres.Run(ctx, "postgres:16-alpine",
		postgres.WithDatabase("drlife_test"),
		postgres.WithUsername("test_admin"),
		postgres.WithPassword("test-only"),
		postgres.BasicWaitStrategies(),
	)
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
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `CREATE SCHEMA ledger;
		CREATE TABLE ledger.outbox (id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY, created_at timestamptz DEFAULT now(), sent_at timestamptz);
		INSERT INTO ledger.outbox(created_at) VALUES (now() - interval '90 seconds'), (now() - interval '30 seconds');
		INSERT INTO ledger.outbox(created_at, sent_at) VALUES (now() - interval '900 seconds', now());`); err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	code := run(ctx, []string{"outbox", "lag"}, &out, &errOut, cliConfig{databaseURL: dsn})
	if code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, errOut.String())
	}
	var lag outboxLag
	if err := json.Unmarshal(out.Bytes(), &lag); err != nil {
		t.Fatal(err)
	}
	if lag.Pending != 2 || lag.OldestAgeSeconds < 89 {
		t.Fatalf("lag = %+v, want 2 pending and >= 89s", lag)
	}
}
