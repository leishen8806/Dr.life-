package queue

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/hibiken/asynq"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/testcontainers/testcontainers-go"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func TestMetricsCollectorLowCardinality(t *testing.T) {
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
	opt, err := asynq.ParseRedisURI(url)
	if err != nil {
		t.Fatal(err)
	}
	client := asynq.NewClient(opt)
	defer client.Close()
	const tasks = 1000
	ids := make([]string, 0, 10)
	for i := 0; i < tasks; i++ {
		info, err := client.Enqueue(asynq.NewTask("test:cardinality:v1", []byte(fmt.Sprintf(`{"customer":%d}`, i))), asynq.Queue(QueueDefault))
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) < 10 {
			ids = append(ids, info.ID)
		}
	}

	collector, err := NewMetricsCollector(url, []string{QueueDefault, QueueCritical}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer collector.Close()
	collector.WithScheduler(func() time.Time { return time.Time{} })
	registry := prometheus.NewRegistry()
	if err := registry.Register(collector); err != nil {
		t.Fatal(err)
	}
	families, err := registry.Gather()
	if err != nil {
		t.Fatal(err)
	}
	var series, taskSeries int
	for _, family := range families {
		series += len(family.Metric)
		if family.GetName() == "drlife_queue_tasks_total" {
			taskSeries += len(family.Metric)
		}
		for _, metric := range family.Metric {
			for _, label := range metric.Label {
				for _, id := range ids {
					if label.GetValue() == id {
						t.Fatalf("task id %q used as a metric label", id)
					}
				}
			}
		}
	}
	if taskSeries != 4 {
		t.Fatalf("drlife_queue_tasks_total series = %d, want 4 (two queues x processed/failed)", taskSeries)
	}
	if series > 20 {
		t.Fatalf("metric series = %d after %d tasks, labels are not low-cardinality", series, tasks)
	}
	var depth float64
	var schedulerSeen, criticalQueueUp bool
	for _, family := range families {
		switch family.GetName() {
		case "drlife_queue_depth":
			for _, metric := range family.Metric {
				if metric.GetGauge().GetValue() > depth {
					depth = metric.GetGauge().GetValue()
				}
			}
		case "drlife_scheduler_last_run_timestamp_seconds":
			schedulerSeen = true
			if got := family.Metric[0].GetGauge().GetValue(); got != 0 {
				t.Fatalf("scheduler last run = %v, want 0 before first tick", got)
			}
		case "drlife_queue_collector_up":
			for _, metric := range family.Metric {
				for _, label := range metric.Label {
					if label.GetName() == "queue" && label.GetValue() == QueueCritical {
						criticalQueueUp = metric.GetGauge().GetValue() == 1
					}
				}
			}
		}
	}
	if depth != tasks {
		t.Fatalf("depth = %v, want %d", depth, tasks)
	}
	if !schedulerSeen {
		t.Fatal("scheduler last-run metric missing before first tick")
	}
	if !criticalQueueUp {
		t.Fatal("unused queue reported collector down")
	}
}
