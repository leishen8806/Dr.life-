package iam

import (
	"context"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

type EpochCacheMetrics interface{ SessionEpochCacheWriteFailed(string) }

// EpochCacheWriter keeps cache failures outside the committed IAM transaction.
type EpochCacheWriter struct {
	Cache   SessionCache
	Logger  *slog.Logger
	Metrics EpochCacheMetrics
	Retry   func(context.Context, func() error)
	Context context.Context
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
	retryCtx := w.Context
	if retryCtx == nil {
		retryCtx = context.Background()
	}
	retry(retryCtx, write)
}

func (w EpochCacheWriter) failed(ctx context.Context, accountID, source string) {
	if w.Logger != nil {
		w.Logger.WarnContext(ctx, "session epoch cache write failed", "account_id", accountID, "source", source)
	}
	if w.Metrics != nil {
		w.Metrics.SessionEpochCacheWriteFailed(source)
	}
}

const epochRetryQueueSize = 256

type epochRetryJob struct {
	ctx   context.Context
	write func() error
}

var (
	epochRetryQueue = make(chan epochRetryJob, epochRetryQueueSize)
	epochRetryOnce  sync.Once
)

// RetryEpochCacheWrite queues cache invalidation on a fixed worker pool, so a
// Redis outage cannot create an unbounded goroutine per failed request.
func RetryEpochCacheWrite(ctx context.Context, write func() error) {
	epochRetryOnce.Do(func() {
		for range 2 {
			go epochRetryWorker()
		}
	})
	select {
	case epochRetryQueue <- epochRetryJob{ctx: ctx, write: write}:
	case <-ctx.Done():
	}
}

func epochRetryWorker() {
	for job := range epochRetryQueue {
		delay := time.Second
		for {
			jitter := time.Duration(rand.Int63n(int64(delay/4) + 1))
			timer := time.NewTimer(delay + jitter)
			select {
			case <-job.ctx.Done():
				timer.Stop()
				goto next
			case <-timer.C:
			}
			if job.write() == nil {
				break
			}
			if delay < time.Minute {
				delay *= 2
				if delay > time.Minute {
					delay = time.Minute
				}
			}
		}
	next:
	}
}
