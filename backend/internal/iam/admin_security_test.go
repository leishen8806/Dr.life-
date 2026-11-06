package iam

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

func TestRedisAdminSecurityStateIsShared(t *testing.T) {
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
	url := "redis://" + host + ":" + port.Port() + "/0"
	first, err := NewRedisAdminSecurityStore(url)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()
	second, err := NewRedisAdminSecurityStore(url)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	var allowed atomic.Int32
	var wg sync.WaitGroup
	for range adminLoginFailureLimit * 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ok, reserveErr := first.ReserveAttempt(ctx, "admin@example.test", "192.0.2.1")
			if reserveErr != nil {
				t.Errorf("reserve attempt: %v", reserveErr)
				return
			}
			if ok {
				allowed.Add(1)
			}
		}()
	}
	wg.Wait()
	if got := allowed.Load(); got != adminLoginFailureLimit {
		t.Fatalf("allowed attempts=%d, want %d", got, adminLoginFailureLimit)
	}
	if locked, err := second.Locked(ctx, "admin@example.test", "192.0.2.2"); err != nil || !locked {
		t.Fatalf("shared account lock locked=%v err=%v", locked, err)
	}
	if err := second.ClearFailures(ctx, "admin@example.test", "192.0.2.1"); err != nil {
		t.Fatal(err)
	}
	if locked, err := first.Locked(ctx, "admin@example.test", "192.0.2.1"); err != nil || locked {
		t.Fatalf("cleared lock locked=%v err=%v", locked, err)
	}
	if fresh, err := first.ConsumeTOTP(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FB0", 123, time.Minute); err != nil || !fresh {
		t.Fatalf("first TOTP consume fresh=%v err=%v", fresh, err)
	}
	if fresh, err := second.ConsumeTOTP(ctx, "01ARZ3NDEKTSV4RRFFQ69G5FB0", 123, time.Minute); err != nil || fresh {
		t.Fatalf("replayed TOTP fresh=%v err=%v", fresh, err)
	}
}
