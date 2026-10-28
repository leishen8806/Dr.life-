package opsalert

import (
	"context"
	"testing"

	"github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"
)

func TestRedisMergerRestoreDoesNotCreateTTLMember(t *testing.T) {
	ctx := context.Background()
	container, err := tcredis.Run(ctx, "redis:7")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	url, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	options, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	merger := NewRedisMerger(client)
	p := payload{OutboxID: 71}
	if leader, err := merger.Add(ctx, "ledger:recon_failed:v1", p); err != nil || !leader {
		t.Fatalf("Add leader=%v err=%v", leader, err)
	}
	group, ok, err := merger.Take(ctx, "ledger:recon_failed:v1")
	if err != nil || !ok {
		t.Fatalf("Take ok=%v err=%v", ok, err)
	}
	if err := merger.Restore(ctx, "ledger:recon_failed:v1", group); err != nil {
		t.Fatal(err)
	}
	restored, ok, err := merger.Take(ctx, "ledger:recon_failed:v1")
	if err != nil || !ok {
		t.Fatalf("restored Take ok=%v err=%v", ok, err)
	}
	if len(restored.IDs) != 1 {
		t.Fatalf("restored IDs=%v, want only outbox 71", restored.IDs)
	}
	if _, exists := restored.IDs[71]; !exists {
		t.Fatalf("restored IDs=%v missing outbox 71", restored.IDs)
	}
}

func TestRedisMergerAllowsRescheduleUntilFlushIsRecorded(t *testing.T) {
	ctx := context.Background()
	container, err := tcredis.Run(ctx, "redis:7")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = testcontainers.TerminateContainer(container) })
	url, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatal(err)
	}
	options, err := redis.ParseURL(url)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	defer client.Close()
	merger := NewRedisMerger(client)
	const topic = "ledger:recon_failed:v1"
	if leader, err := merger.Add(ctx, topic, payload{OutboxID: 81}); err != nil || !leader {
		t.Fatalf("first Add leader=%v err=%v", leader, err)
	}
	if leader, err := merger.Add(ctx, topic, payload{OutboxID: 82}); err != nil || !leader {
		t.Fatalf("unmarked Add leader=%v err=%v, want true/nil", leader, err)
	}
	if err := merger.MarkScheduled(ctx, topic); err != nil {
		t.Fatal(err)
	}
	if leader, err := merger.Add(ctx, topic, payload{OutboxID: 83}); err != nil || leader {
		t.Fatalf("marked Add leader=%v err=%v, want false/nil", leader, err)
	}
}
