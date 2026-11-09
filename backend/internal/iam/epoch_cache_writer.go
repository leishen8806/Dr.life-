package iam

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

type EpochCacheMetrics interface {
	SessionEpochCacheWriteFailed(string)
	SessionEpochCacheRetryDropped(string, string)
}

type EpochCacheRetry func(context.Context, func() error, func(string))

// EpochCacheWriter keeps cache failures outside the committed IAM transaction.
type EpochCacheWriter struct {
	Cache   SessionCache
	Logger  *slog.Logger
	Metrics EpochCacheMetrics
	Retry   EpochCacheRetry
}

func (w EpochCacheWriter) Write(ctx context.Context, accountID string, epoch int64, ttl time.Duration, source string) {
	if w.Cache == nil {
		return
	}
	write := func() error { return w.Cache.SetAccountEpoch(context.Background(), accountID, epoch, ttl) }
	if err := write(); err == nil {
		return
	}
	w.retry(ctx, accountID, source, write)
}

func (w EpochCacheWriter) retry(ctx context.Context, accountID, source string, write func() error) {
	w.failed(ctx, accountID, source)
	retry := w.Retry
	if retry == nil {
		retry = RetryEpochCacheWrite
	}
	retry(ctx, write, func(reason string) { w.dropped(accountID, source, reason) })
}

func (w EpochCacheWriter) failed(ctx context.Context, accountID, source string) {
	if w.Logger != nil {
		w.Logger.WarnContext(ctx, "session epoch cache write failed", "account_id", accountID, "source", source)
	}
	if w.Metrics != nil {
		w.Metrics.SessionEpochCacheWriteFailed(source)
	}
}

func (w EpochCacheWriter) dropped(accountID, source, reason string) {
	if w.Logger != nil {
		w.Logger.Warn("session epoch cache retry dropped", "account_id", accountID, "source", source, "reason", reason)
	}
	if w.Metrics != nil {
		w.Metrics.SessionEpochCacheRetryDropped(source, reason)
	}
}

const (
	epochRetryQueueSize   = 256
	maxEpochRetryAttempts = 5
)

type epochRetryJob struct {
	write   func() error
	dropped func(string)
}

var (
	epochRetryQueue = make(chan epochRetryJob, epochRetryQueueSize)
	epochRetryOnce  sync.Once
)

// RetryEpochCacheWrite queues cache invalidation on a fixed worker pool, so a
// Redis outage cannot create an unbounded goroutine per failed request.
func RetryEpochCacheWrite(ctx context.Context, write func() error, dropped func(string)) {
	epochRetryOnce.Do(func() {
		for range 2 {
			go epochRetryWorker(epochRetryQueue)
		}
	})
	if reason := enqueueEpochRetry(ctx, epochRetryQueue, epochRetryJob{write: write, dropped: dropped}); reason != "" && dropped != nil {
		dropped(reason)
	}
}

func enqueueEpochRetry(ctx context.Context, queue chan<- epochRetryJob, job epochRetryJob) string {
	select {
	case <-ctx.Done():
		return "context_cancelled"
	default:
	}
	select {
	case queue <- job:
		return ""
	default:
		return "queue_full"
	}
}

func epochRetryWorker(queue <-chan epochRetryJob) {
	for job := range queue {
		if retryEpochCacheWrite(job.write, waitEpochRetry) && job.dropped != nil {
			job.dropped("retry_exhausted")
		}
	}
}

func retryEpochCacheWrite(write func() error, wait func(time.Duration)) bool {
	delay := time.Second
	for attempt := 0; attempt < maxEpochRetryAttempts; attempt++ {
		jitter := time.Duration(rand.Int63n(int64(delay/4) + 1))
		wait(delay + jitter)
		if write() == nil {
			return false
		}
		delay *= 2
	}
	return true
}

func waitEpochRetry(delay time.Duration) { time.Sleep(delay) }
