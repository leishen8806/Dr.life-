package queue

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
)

// AllQueues is the fixed registry order used by metrics and queuectl.
var AllQueues = []string{QueueCritical, QueueDefault, QueueNotify, QueueAI, QueueBatch, QueueScheduled}

const metricsTimeout = 2 * time.Second

type OutboxLagFunc func(context.Context) (int64, time.Duration, error)

// MetricsCollector exposes queue and queue-Redis state on scrape. Labels stay
// at queue/type/result granularity; task IDs and payloads never become labels.
type MetricsCollector struct {
	inspector *asynq.Inspector
	redis     *redis.Client
	queues    []string
	outbox    OutboxLagFunc
	scheduler func() time.Time

	depth, oldest, retry, dead, paused, collectorUp, tasks *prometheus.Desc
	redisUsed, redisRatio, outboxPending, outboxOldest     *prometheus.Desc
	schedulerLastRun                                       *prometheus.Desc
	taskDuration                                           *prometheus.HistogramVec
}

func NewMetricsCollector(rawURL string, queues []string, outbox OutboxLagFunc) (*MetricsCollector, error) {
	redisOptions, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, errors.New("invalid queue Redis URL")
	}
	asynqOptions, err := asynq.ParseRedisURI(rawURL)
	if err != nil {
		return nil, errors.New("invalid queue Redis URL")
	}
	if len(queues) == 0 {
		queues = AllQueues
	}
	return &MetricsCollector{
		inspector: asynq.NewInspector(asynqOptions),
		redis:     redis.NewClient(redisOptions),
		queues:    queues,
		outbox:    outbox,
		depth: prometheus.NewDesc("drlife_queue_depth",
			"Tasks pending, active, scheduled, or retrying per queue.", []string{"queue"}, nil),
		oldest: prometheus.NewDesc("drlife_queue_oldest_task_seconds",
			"Age of the oldest pending task per queue.", []string{"queue"}, nil),
		retry: prometheus.NewDesc("drlife_queue_retry_tasks",
			"Tasks currently waiting for a retry per queue.", []string{"queue"}, nil),
		dead: prometheus.NewDesc("drlife_queue_dead_letters",
			"Archived tasks per queue.", []string{"queue"}, nil),
		paused: prometheus.NewDesc("drlife_queue_paused",
			"Whether a queue is paused (1) or running (0).", []string{"queue"}, nil),
		collectorUp: prometheus.NewDesc("drlife_queue_collector_up",
			"Whether the queue metrics scrape succeeded per queue.", []string{"queue"}, nil),
		tasks: prometheus.NewDesc("drlife_queue_tasks_total",
			"Cumulative processed and failed tasks per queue.", []string{"queue", "result"}, nil),
		redisUsed: prometheus.NewDesc("drlife_queue_redis_memory_used_bytes",
			"Queue Redis memory currently used.", nil, nil),
		redisRatio: prometheus.NewDesc("drlife_queue_redis_memory_ratio",
			"Queue Redis used memory divided by maxmemory (0 when maxmemory is unset).", nil, nil),
		outboxPending: prometheus.NewDesc("drlife_outbox_pending",
			"Unsent outbox events.", nil, nil),
		outboxOldest: prometheus.NewDesc("drlife_outbox_oldest_seconds",
			"Age of the oldest unsent outbox event.", nil, nil),
		schedulerLastRun: prometheus.NewDesc("drlife_scheduler_last_run_timestamp_seconds",
			"Unix timestamp of the last scheduler tick.", nil, nil),
		taskDuration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "drlife_queue_task_duration_seconds",
			Help:    "Task handler duration by queue and registered task type.",
			Buckets: []float64{.01, .025, .05, .1, .25, .5, 1, 2.5, 5, 10, 30, 60},
		}, []string{"queue", "type"}),
	}, nil
}

// WithScheduler attaches a scheduler last-run source. It returns the collector
// so callers can chain it after NewMetricsCollector.
func (c *MetricsCollector) WithScheduler(lastRun func() time.Time) *MetricsCollector {
	c.scheduler = lastRun
	return c
}

// ObserveTask records one task execution. Only queue and registered task type
// are labels; payloads and task IDs are never observed.
func (c *MetricsCollector) ObserveTask(queueName, taskType string, duration time.Duration, _ error) {
	if c == nil || c.taskDuration == nil {
		return
	}
	c.taskDuration.WithLabelValues(queueName, taskType).Observe(duration.Seconds())
}

func (c *MetricsCollector) Describe(ch chan<- *prometheus.Desc) {
	for _, desc := range []*prometheus.Desc{
		c.depth, c.oldest, c.retry, c.dead, c.paused, c.collectorUp, c.tasks,
		c.redisUsed, c.redisRatio, c.outboxPending, c.outboxOldest, c.schedulerLastRun,
	} {
		ch <- desc
	}
	c.taskDuration.Describe(ch)
}

func (c *MetricsCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), metricsTimeout)
	defer cancel()
	existing, listErr := c.inspector.Queues()
	known := make(map[string]struct{}, len(existing))
	for _, name := range existing {
		known[name] = struct{}{}
	}
	for _, name := range c.queues {
		up := 1.0
		var info *asynq.QueueInfo
		if listErr != nil {
			up = 0
		} else if _, ok := known[name]; !ok {
			info = &asynq.QueueInfo{}
		} else {
			var err error
			info, err = c.inspector.GetQueueInfo(name)
			if err != nil {
				up = 0
			}
		}
		ch <- prometheus.MustNewConstMetric(c.collectorUp, prometheus.GaugeValue, up, name)
		if up == 0 {
			continue
		}
		ch <- prometheus.MustNewConstMetric(c.depth, prometheus.GaugeValue,
			float64(info.Pending+info.Active+info.Scheduled+info.Retry), name)
		ch <- prometheus.MustNewConstMetric(c.oldest, prometheus.GaugeValue, info.Latency.Seconds(), name)
		ch <- prometheus.MustNewConstMetric(c.retry, prometheus.GaugeValue, float64(info.Retry), name)
		ch <- prometheus.MustNewConstMetric(c.dead, prometheus.GaugeValue, float64(info.Archived), name)
		ch <- prometheus.MustNewConstMetric(c.paused, prometheus.GaugeValue, boolValue(info.Paused), name)
		ch <- prometheus.MustNewConstMetric(c.tasks, prometheus.CounterValue, float64(info.ProcessedTotal), name, "processed")
		ch <- prometheus.MustNewConstMetric(c.tasks, prometheus.CounterValue, float64(info.FailedTotal), name, "failed")
	}
	if used, ratio, ok := c.redisMemory(ctx); ok {
		ch <- prometheus.MustNewConstMetric(c.redisUsed, prometheus.GaugeValue, float64(used))
		ch <- prometheus.MustNewConstMetric(c.redisRatio, prometheus.GaugeValue, ratio)
	}
	if c.outbox != nil {
		if pending, oldest, err := c.outbox(ctx); err == nil {
			ch <- prometheus.MustNewConstMetric(c.outboxPending, prometheus.GaugeValue, float64(pending))
			ch <- prometheus.MustNewConstMetric(c.outboxOldest, prometheus.GaugeValue, oldest.Seconds())
		}
	}
	if c.scheduler != nil {
		lastRun := 0.0
		if last := c.scheduler(); !last.IsZero() {
			lastRun = float64(last.Unix())
		}
		ch <- prometheus.MustNewConstMetric(c.schedulerLastRun, prometheus.GaugeValue, lastRun)
	}
	c.taskDuration.Collect(ch)
}

func (c *MetricsCollector) Close() error {
	inspectorErr := c.inspector.Close()
	redisErr := c.redis.Close()
	return errors.Join(inspectorErr, redisErr)
}

func (c *MetricsCollector) redisMemory(ctx context.Context) (used int64, ratio float64, ok bool) {
	info, err := c.redis.Info(ctx, "memory").Result()
	if err != nil {
		return 0, 0, false
	}
	for _, line := range strings.Split(info, "\n") {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), "used_memory:"); found {
			used, _ = strconv.ParseInt(strings.TrimSpace(value), 10, 64)
			break
		}
	}
	config, err := c.redis.ConfigGet(ctx, "maxmemory").Result()
	if err == nil {
		if max, parseErr := strconv.ParseInt(config["maxmemory"], 10, 64); parseErr == nil && max > 0 {
			ratio = float64(used) / float64(max)
		}
	}
	return used, ratio, true
}

func boolValue(value bool) float64 {
	if value {
		return 1
	}
	return 0
}

type Querier interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

// OutboxLag is read-only. shortcut: ops reads ledger.outbox directly because a
// ledger read API does not exist yet; move this behind ledger when it does.
func OutboxLag(ctx context.Context, db Querier) (int64, time.Duration, error) {
	var pending int64
	var seconds float64
	err := db.QueryRow(ctx, `
		SELECT count(*), COALESCE(EXTRACT(EPOCH FROM now()-min(created_at)), 0)
		FROM ledger.outbox
		WHERE sent_at IS NULL`).Scan(&pending, &seconds)
	if err != nil {
		return 0, 0, err
	}
	return pending, time.Duration(seconds * float64(time.Second)), nil
}
