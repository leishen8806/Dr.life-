package iam

import (
	"context"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestRedisAccountEpochNeverMovesBackward(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	container, err := testcontainers.Run(ctx, "redis:7-alpine", testcontainers.WithExposedPorts("6379/tcp"), testcontainers.WithWaitStrategy(wait.ForListeningPort("6379/tcp")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	host, err := container.Host(ctx)
	if err != nil {
		t.Fatal(err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatal(err)
	}
	cache, err := NewRedisSessionCache("redis://" + host + ":" + port.Port() + "/0")
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()

	if err := cache.SetAccountEpoch(ctx, "account-1", 2, time.Hour); err != nil {
		t.Fatal(err)
	}
	if err := cache.SetAccountEpoch(ctx, "account-1", 1, time.Hour); err != nil {
		t.Fatal(err)
	}
	if got, err := cache.client.Get(ctx, epochCacheKey("account-1")).Int64(); err != nil || got != 2 {
		t.Fatalf("epoch=%d err=%v, want 2", got, err)
	}
}
