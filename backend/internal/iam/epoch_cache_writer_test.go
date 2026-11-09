package iam

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestEpochCacheWriterReportsFailureAndRetries(t *testing.T) {
	cache := &epochWriterTestCache{err: errors.New("redis unavailable")}
	metrics := &epochWriterTestMetrics{}
	var logs bytes.Buffer
	writer := EpochCacheWriter{
		Cache:   cache,
		Logger:  slog.New(slog.NewTextHandler(&logs, nil)),
		Metrics: metrics,
		Retry: func(_ context.Context, write func() error, _ func(string)) {
			cache.setError(nil)
			if err := write(); err != nil {
				t.Errorf("retry write: %v", err)
			}
		},
	}

	writer.Write(context.Background(), "account-1", 2, time.Hour, "account_status")

	if got := metrics.count; got != 1 {
		t.Fatalf("failure metric=%d, want 1", got)
	}
	if !strings.Contains(logs.String(), "account_id=account-1") || !strings.Contains(logs.String(), "source=account_status") {
		t.Fatalf("warning log=%q", logs.String())
	}
	if got := cache.epoch("account-1"); got != 2 {
		t.Fatalf("cached epoch=%d, want 2 after retry", got)
	}
}

func TestEpochRetryQueueFullReturnsImmediately(t *testing.T) {
	queue := make(chan epochRetryJob, 1)
	queue <- epochRetryJob{}
	start := time.Now()
	if got := enqueueEpochRetry(context.Background(), queue, epochRetryJob{}); got != "queue_full" {
		t.Fatalf("enqueue reason=%q, want queue_full", got)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("full retry queue blocked for %v", elapsed)
	}
}

func TestEpochRetryStopsAfterFiveAttempts(t *testing.T) {
	attempts := 0
	exhausted := retryEpochCacheWrite(func() error {
		attempts++
		return errors.New("redis unavailable")
	}, func(time.Duration) {})
	if !exhausted || attempts != maxEpochRetryAttempts {
		t.Fatalf("exhausted=%v attempts=%d, want true and %d", exhausted, attempts, maxEpochRetryAttempts)
	}
}

func TestEpochCacheWriterReportsDroppedRetry(t *testing.T) {
	cache := &epochWriterTestCache{err: errors.New("redis unavailable")}
	metrics := &epochWriterTestMetrics{}
	var logs bytes.Buffer
	writer := EpochCacheWriter{
		Cache:   cache,
		Logger:  slog.New(slog.NewTextHandler(&logs, nil)),
		Metrics: metrics,
		Retry: func(_ context.Context, _ func() error, dropped func(string)) {
			dropped("queue_full")
		},
	}

	writer.Write(context.Background(), "account-1", 2, time.Hour, "revoke_session")

	if metrics.dropped["queue_full"] != 1 || !strings.Contains(logs.String(), "reason=queue_full") {
		t.Fatalf("dropped metrics=%v log=%q", metrics.dropped, logs.String())
	}
}

func TestEpochCacheWriterDoesNotLowerEpoch(t *testing.T) {
	cache := &epochWriterTestCache{}
	writer := EpochCacheWriter{Cache: cache}
	writer.Write(context.Background(), "account-1", 2, time.Hour, "test")
	writer.Write(context.Background(), "account-1", 1, time.Hour, "test")
	if got := cache.epoch("account-1"); got != 2 {
		t.Fatalf("cached epoch=%d, want 2", got)
	}
}

func TestDBSessionCheckerFailsClosedWhenCacheUnavailable(t *testing.T) {
	cache := &epochWriterTestCache{getErr: ErrSessionStoreUnavailable}
	active, err := (DBSessionChecker{Cache: cache}).Active(context.Background(), "account-1", "session-1")
	if active || !errors.Is(err, ErrSessionStoreUnavailable) {
		t.Fatalf("active=%v err=%v, want false and ErrSessionStoreUnavailable", active, err)
	}
}

type epochWriterTestMetrics struct {
	count   int
	dropped map[string]int
}

func (m *epochWriterTestMetrics) SessionEpochCacheWriteFailed(string) { m.count++ }
func (m *epochWriterTestMetrics) SessionEpochCacheRetryDropped(_, reason string) {
	if m.dropped == nil {
		m.dropped = make(map[string]int)
	}
	m.dropped[reason]++
}

type epochWriterTestCache struct {
	mu     sync.Mutex
	err    error
	getErr error
	epochs map[string]int64
}

func (c *epochWriterTestCache) Get(context.Context, string, string) (bool, bool, error) {
	return false, false, c.getErr
}
func (c *epochWriterTestCache) Put(context.Context, string, string, int64, time.Duration) error {
	return nil
}
func (c *epochWriterTestCache) Revoke(context.Context, string, time.Duration) error { return nil }
func (c *epochWriterTestCache) SetAccountEpoch(_ context.Context, accountID string, epoch int64, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	if c.epochs == nil {
		c.epochs = make(map[string]int64)
	}
	if c.epochs[accountID] <= epoch {
		c.epochs[accountID] = epoch
	}
	return nil
}
func (c *epochWriterTestCache) setError(err error) { c.mu.Lock(); c.err = err; c.mu.Unlock() }
func (c *epochWriterTestCache) epoch(accountID string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.epochs[accountID]
}
