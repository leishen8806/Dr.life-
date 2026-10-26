package queue

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func TestSchedulerEnqueuesOneTaskPerWindowAcrossInstances(t *testing.T) {
	ctx := context.Background()
	container, err := tcredis.Run(ctx, "redis:7")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = container.Terminate(ctx) })
	url, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	registry := NewRegistry()
	if err := registry.Register(TypeSpec{Name: "ledger:daily_reconciliation:v1", Queue: QueueScheduled, Timeout: time.Second, MaxRetry: 1}); err != nil {
		t.Fatal(err)
	}
	client := newRedisClient(t, url)
	defer client.Close()
	window := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	task := ScheduledTask{Name: "daily-reconciliation", Cron: "0 0 * * *", Type: "ledger:daily_reconciliation:v1", Payload: func(time.Time) []byte { return []byte("{}") }}
	first, err := NewScheduler(NewEnqueuer(client, registry, EnqueueOptions{}), []ScheduledTask{task})
	if err != nil {
		t.Fatal(err)
	}
	second, err := NewScheduler(NewEnqueuer(client, registry, EnqueueOptions{}), []ScheduledTask{task})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, scheduler := range []*Scheduler{first, second} {
		wg.Add(1)
		go func(s *Scheduler) { defer wg.Done(); _ = s.EnqueueWindow(ctx, task.Name, window) }(scheduler)
	}
	wg.Wait()
	inspector := newInspector(t, url)
	defer inspector.Close()
	info, err := inspector.GetQueueInfo(QueueScheduled)
	if err != nil {
		t.Fatal(err)
	}
	if info.Pending != 1 {
		t.Fatalf("scheduled pending = %d, want 1", info.Pending)
	}
}

func TestSchedulerBackfillsMissingDailyWindows(t *testing.T) {
	registry := NewRegistry()
	if err := registry.Register(TypeSpec{Name: "ledger:daily_reconciliation:v1", Queue: QueueScheduled, Timeout: time.Second, MaxRetry: 1}); err != nil {
		t.Fatal(err)
	}
	client := &recordingClient{}
	task := ScheduledTask{Name: "daily-reconciliation", Cron: "0 0 * * *", Type: "ledger:daily_reconciliation:v1", Payload: func(time.Time) []byte { return []byte("{}") }, Daily: true}
	scheduler, err := NewScheduler(NewEnqueuer(client, registry, EnqueueOptions{}), []ScheduledTask{task})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if err := scheduler.Backfill(context.Background(), now, 3); err != nil {
		t.Fatal(err)
	}
	if client.calls != 3 {
		t.Fatalf("backfilled %d tasks, want 3", client.calls)
	}
}

func newRedisClient(t *testing.T, rawURL string) *asynq.Client {
	t.Helper()
	options, err := asynqRedisOptions(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return asynq.NewClient(options)
}

func newInspector(t *testing.T, rawURL string) *asynq.Inspector {
	t.Helper()
	options, err := asynqRedisOptions(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	return asynq.NewInspector(options)
}

type recordingClient struct{ calls int }

func (c *recordingClient) EnqueueContext(_ context.Context, _ *asynq.Task, _ ...asynq.Option) (*asynq.TaskInfo, error) {
	c.calls++
	return &asynq.TaskInfo{}, nil
}
