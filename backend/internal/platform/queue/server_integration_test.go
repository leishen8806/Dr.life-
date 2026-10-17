package queue

import (
	"context"
	"errors"
	"testing"

	"github.com/redis/go-redis/v9"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func TestQueueRedisPolicyRejectsEviction(t *testing.T) {
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
	if err := QueueRedisPolicy(ctx, url, false); err != nil {
		t.Fatalf("default noeviction policy rejected: %v", err)
	}
	options, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	t.Cleanup(func() { _ = client.Close() })
	if err := client.ConfigSet(ctx, "maxmemory-policy", "allkeys-lru").Err(); err != nil {
		t.Fatal(err)
	}
	if err := QueueRedisPolicy(ctx, url, false); !errors.Is(err, ErrQueueRedisEviction) {
		t.Fatalf("policy error = %v, want %v", err, ErrQueueRedisEviction)
	}
}
