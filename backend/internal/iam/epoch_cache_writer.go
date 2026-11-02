package iam

import (
	"context"
	"log/slog"
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
	w.failed(ctx, accountID, source)
	if w.Retry != nil {
		retryCtx := w.Context
		if retryCtx == nil {
			retryCtx = context.Background()
		}
		w.Retry(retryCtx, write)
	}
}

func (w EpochCacheWriter) failed(ctx context.Context, accountID, source string) {
	if w.Logger != nil {
		w.Logger.WarnContext(ctx, "session epoch cache write failed", "account_id", accountID, "source", source)
	}
	if w.Metrics != nil {
		w.Metrics.SessionEpochCacheWriteFailed(source)
	}
}

// RetryEpochCacheWrite retries a failed cache invalidation without holding the
// request or database transaction open. It stops only after Redis accepts it.
func RetryEpochCacheWrite(ctx context.Context, write func() error) {
	go func() {
		delay := time.Second
		for {
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return
			case <-timer.C:
			}
			if write() == nil {
				return
			}
			if delay < time.Minute {
				delay *= 2
				if delay > time.Minute {
					delay = time.Minute
				}
			}
		}
	}()
}
