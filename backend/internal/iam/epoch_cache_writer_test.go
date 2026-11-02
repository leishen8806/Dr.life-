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
		Retry: func(_ context.Context, write func() error) {
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

type epochWriterTestMetrics struct{ count int }

func (m *epochWriterTestMetrics) SessionEpochCacheWriteFailed(string) { m.count++ }

type epochWriterTestCache struct {
	mu     sync.Mutex
	err    error
	epochs map[string]int64
}

func (c *epochWriterTestCache) Get(context.Context, string, string) (bool, bool, error) {
	return false, false, nil
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
	c.epochs[accountID] = epoch
	return nil
}
func (c *epochWriterTestCache) setError(err error) { c.mu.Lock(); c.err = err; c.mu.Unlock() }
func (c *epochWriterTestCache) epoch(accountID string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.epochs[accountID]
}
