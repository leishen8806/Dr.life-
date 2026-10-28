package cache

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

type memoryStore struct {
	mu    sync.Mutex
	items map[string]Entry
}

func newMemoryStore() *memoryStore { return &memoryStore{items: make(map[string]Entry)} }

func (s *memoryStore) Get(_ context.Context, key string) (Entry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.items[key]
	return copyEntry(entry), ok, nil
}

func (s *memoryStore) Set(_ context.Context, key string, entry Entry, _ time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[key] = copyEntry(entry)
	return nil
}

func (s *memoryStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
	return nil
}

// This contract test is intentionally added before the implementation.
func TestRejectsRestrictedAndUnknownNamespaces(t *testing.T) {
	cache := NewMemory(Options{Environment: "test", Version: "v1"})
	for _, key := range []string{"ledger:balance", "wallet:1", "order:1", "stock:sku", "withdraw:1", "unknown:x", "hot:session:abc123", "hot:user:42:phone", "hot:chat:1001:messages", "hot:token:refresh", "config:secret-jwt", "config:session-abc123", "catalog:user-42-phone", "i18n:ledger-balance-acct-7", "i18n:withdraw-req-9"} {
		if err := cache.Set(context.Background(), key, []byte("bad"), time.Minute); !errors.Is(err, ErrNamespace) {
			t.Errorf("Set(%q) error = %v, want ErrNamespace", key, err)
		}
		if _, _, err := cache.Get(context.Background(), key); !errors.Is(err, ErrNamespace) {
			t.Errorf("Get(%q) error = %v, want ErrNamespace", key, err)
		}
	}
}

func TestGetDoesNotExposeNegativeCacheAsAnEmptyHit(t *testing.T) {
	cache := NewMemory(Options{Environment: "test", NegativeTTL: time.Minute})
	if _, err := cache.Remember(context.Background(), "catalog:missing", time.Minute, func(context.Context) ([]byte, error) {
		return nil, ErrNotFound
	}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Remember error = %v, want ErrNotFound", err)
	}
	if entry, ok, err := cache.Get(context.Background(), "catalog:missing"); err != nil || ok || entry.Value != nil {
		t.Fatalf("Get = (%q, %v, %v), want (nil, false, nil)", entry.Value, ok, err)
	}
}

func TestRememberStoreHitPopulatesMemory(t *testing.T) {
	store := newMemoryStore()
	cache := New(Options{Environment: "test"}, store)
	if err := store.Set(context.Background(), cache.key("catalog:popular"), Entry{Value: []byte("cached")}, time.Minute); err != nil {
		t.Fatal(err)
	}
	if got, err := cache.Remember(context.Background(), "catalog:popular", time.Minute, func(context.Context) ([]byte, error) {
		return nil, errors.New("source should not run")
	}); err != nil || string(got) != "cached" {
		t.Fatalf("Remember = %q, %v", got, err)
	}
	cache.mu.Lock()
	_, present := cache.items[cache.key("catalog:popular")]
	cache.mu.Unlock()
	if !present {
		t.Fatal("store hit did not populate memory")
	}
}

func TestExpiredRecordsArePrunedInBoundedBatches(t *testing.T) {
	cache := NewMemory(Options{Environment: "test", StaleTTL: time.Second})
	now := time.Unix(100, 0)
	cache.now = func() time.Time { return now }
	for i := 0; i < 100; i++ {
		if err := cache.Set(context.Background(), "catalog:item-"+strconv.Itoa(i), []byte("value"), time.Second); err != nil {
			t.Fatal(err)
		}
	}
	now = now.Add(3 * time.Second)
	if err := cache.Set(context.Background(), "catalog:fresh", []byte("value"), time.Second); err != nil {
		t.Fatal(err)
	}
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if len(cache.items) >= 101 {
		t.Fatalf("items = %d, want an expired entry removed", len(cache.items))
	}
	if len(cache.items) <= 1 {
		t.Fatalf("items = %d, want bounded cleanup instead of a full scan", len(cache.items))
	}
}

func TestMemoryLimitEvictsLeastRecentlyUsedAndSkipsOversizeValues(t *testing.T) {
	var skipped atomic.Int32
	cache := NewMemory(Options{Environment: "test", MaxEntries: 2, OnOversize: func() { skipped.Add(1) }})
	if err := cache.Set(context.Background(), "catalog:a", []byte("a"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if err := cache.Set(context.Background(), "catalog:b", []byte("b"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := cache.Get(context.Background(), "catalog:a"); err != nil || !ok {
		t.Fatalf("Get(a) = %v, %v", ok, err)
	}
	if err := cache.Set(context.Background(), "catalog:c", []byte("c"), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cache.Get(context.Background(), "catalog:b"); ok {
		t.Fatal("least recently used item retained")
	}
	if err := cache.Set(context.Background(), "catalog:large", make([]byte, 64<<10+1), time.Minute); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := cache.Get(context.Background(), "catalog:large"); ok {
		t.Fatal("oversize value cached")
	}
	if skipped.Load() != 1 {
		t.Fatalf("oversize skips = %d, want 1", skipped.Load())
	}
}

// Store data is untrusted for the in-process memory budget: both read paths
// must return an oversize value without retaining it in c.items.
func TestStoreSourcedOversizeValuesDoNotPopulateMemory(t *testing.T) {
	for _, path := range []struct {
		name string
		read func(*Client) error
	}{
		{
			name: "Get",
			read: func(cache *Client) error {
				_, _, err := cache.Get(context.Background(), "catalog:large")
				return err
			},
		},
		{
			name: "RememberFetch",
			read: func(cache *Client) error {
				_, err := cache.Remember(context.Background(), "catalog:large", time.Minute, func(context.Context) ([]byte, error) {
					return nil, errors.New("source should not run")
				})
				return err
			},
		},
	} {
		t.Run(path.name, func(t *testing.T) {
			store := newMemoryStore()
			var skipped atomic.Int32
			cache := New(Options{Environment: "test", OnOversize: func() { skipped.Add(1) }}, store)
			if err := store.Set(context.Background(), cache.key("catalog:large"), Entry{Value: make([]byte, maxValueBytes+1)}, time.Minute); err != nil {
				t.Fatal(err)
			}
			if err := path.read(cache); err != nil {
				t.Fatal(err)
			}
			cache.mu.Lock()
			_, retained := cache.items[cache.key("catalog:large")]
			cache.mu.Unlock()
			if retained {
				t.Fatal("oversize store value was retained in memory")
			}
			if skipped.Load() != 1 {
				t.Fatalf("oversize skips = %d, want 1", skipped.Load())
			}
		})
	}
}

func TestWriteCostDoesNotGrowWithCacheSize(t *testing.T) {
	var smallest time.Duration
	for _, size := range []int{5_000, 10_000, 20_000, 40_000} {
		cache := NewMemory(Options{Environment: "test", MaxEntries: 50_000})
		now := time.Now()
		cache.now = func() time.Time { return now }
		cache.mu.Lock()
		for i := 0; i < size; i++ {
			key := cache.key("catalog:item-" + strconv.Itoa(i))
			cache.items[key] = record{entry: Entry{Value: []byte("value")}, freshUntil: now.Add(time.Minute), staleUntil: now.Add(time.Minute)}
			cache.lruPos[key] = cache.lru.PushBack(key)
		}
		cache.mu.Unlock()

		started := time.Now()
		if err := cache.Set(context.Background(), "catalog:write", []byte("value"), time.Minute); err != nil {
			t.Fatal(err)
		}
		elapsed := time.Since(started)
		if size == 5_000 {
			smallest = elapsed
		}
		limit := 10 * time.Millisecond
		if relative := 8 * smallest; relative > limit {
			limit = relative
		}
		if elapsed > limit {
			t.Fatalf("Set with %d entries took %s, want bounded operation below %s", size, elapsed, limit)
		}
	}
}

func TestRememberSingleflight100Concurrent(t *testing.T) {
	cache := NewMemory(Options{Environment: "test"})
	var calls atomic.Int32
	start := make(chan struct{})
	var group sync.WaitGroup
	for range 100 {
		group.Add(1)
		go func() {
			defer group.Done()
			<-start
			got, err := cache.Remember(context.Background(), "catalog:popular", time.Minute, func(context.Context) ([]byte, error) { calls.Add(1); return []byte("fresh"), nil })
			if err != nil || string(got) != "fresh" {
				t.Errorf("got=%q err=%v", got, err)
			}
		}()
	}
	close(start)
	group.Wait()
	if calls.Load() != 1 {
		t.Fatalf("source calls=%d, want 1", calls.Load())
	}
}

func TestRememberLeaderCancellationDoesNotCancelFollowers(t *testing.T) {
	cache := NewMemory(Options{Environment: "test", RefreshTimeout: time.Second})
	leaderCtx, cancelLeader := context.WithCancel(context.Background())
	defer cancelLeader()
	started := make(chan struct{})
	release := make(chan struct{})
	source := func(ctx context.Context) ([]byte, error) {
		close(started)
		select {
		case <-release:
			return []byte("fresh"), nil
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	leaderResult := make(chan error, 1)
	go func() {
		_, err := cache.Remember(leaderCtx, "catalog:popular", time.Minute, source)
		leaderResult <- err
	}()
	<-started
	followerResult := make(chan error, 1)
	go func() {
		got, err := cache.Remember(context.Background(), "catalog:popular", time.Minute, source)
		if string(got) != "fresh" {
			followerResult <- errors.New("follower did not receive fresh value")
			return
		}
		followerResult <- err
	}()
	cancelLeader()
	if err := <-leaderResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("leader error = %v, want context.Canceled", err)
	}
	close(release)
	if err := <-followerResult; err != nil {
		t.Fatalf("follower error = %v", err)
	}
}

func TestRememberTimedOutLoadReleasesFlight(t *testing.T) {
	cache := NewMemory(Options{Environment: "test", RefreshTimeout: 20 * time.Millisecond})
	if _, err := cache.Remember(context.Background(), "catalog:slow", time.Minute, func(context.Context) ([]byte, error) { select {} }); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("hung source error = %v, want deadline exceeded", err)
	}
	got, err := cache.Remember(context.Background(), "catalog:slow", time.Minute, func(context.Context) ([]byte, error) { return []byte("retry"), nil })
	if err != nil || string(got) != "retry" {
		t.Fatalf("retry = %q, %v", got, err)
	}
}

func TestRememberSWRAndNegativeCache(t *testing.T) {
	cache := NewMemory(Options{Environment: "test", StaleTTL: time.Minute, NegativeTTL: time.Minute})
	now := time.Unix(100, 0)
	cache.now = func() time.Time { return now }
	if err := cache.Set(context.Background(), "catalog:popular", []byte("old"), time.Millisecond); err != nil {
		t.Fatal(err)
	}
	now = now.Add(2 * time.Millisecond)
	refreshed := make(chan struct{})
	got, err := cache.Remember(context.Background(), "catalog:popular", time.Minute, func(context.Context) ([]byte, error) { close(refreshed); return []byte("new"), nil })
	if err != nil || string(got) != "old" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	<-refreshed
	var calls atomic.Int32
	source := func(context.Context) ([]byte, error) { calls.Add(1); return nil, ErrNotFound }
	for range 2 {
		if _, err := cache.Remember(context.Background(), "catalog:missing", time.Minute, source); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err=%v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("negative-cache source calls=%d, want 1", calls.Load())
	}
}

func TestRedisStoreWithContainer(t *testing.T) {
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
	store, err := NewRedisStore("redis://" + host + ":" + port.Port() + "/0")
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	cache := New(Options{Environment: "test"}, store)
	if err := cache.Set(ctx, "catalog:popular", []byte("redis"), time.Minute); err != nil {
		t.Fatal(err)
	}
	got, err := New(Options{Environment: "test"}, store).Remember(ctx, "catalog:popular", time.Minute, func(context.Context) ([]byte, error) { return nil, errors.New("source should not run") })
	if err != nil || string(got) != "redis" {
		t.Fatalf("got=%q err=%v", got, err)
	}
}

func TestRedisPolicyWithContainer(t *testing.T) {
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
	rawURL := "redis://" + host + ":" + port.Port() + "/0"
	store, err := NewRedisStore(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.client.ConfigSet(ctx, "maxmemory", "1048576").Err(); err != nil {
		t.Fatal(err)
	}
	if err := store.client.ConfigSet(ctx, "maxmemory-policy", "allkeys-lru").Err(); err != nil {
		t.Fatal(err)
	}
	if err := RedisPolicy(ctx, rawURL); err != nil {
		t.Fatalf("allkeys-lru policy error = %v", err)
	}
	if err := store.client.ConfigSet(ctx, "maxmemory-policy", "noeviction").Err(); err != nil {
		t.Fatal(err)
	}
	if err := RedisPolicy(ctx, rawURL); !errors.Is(err, ErrRedisEviction) {
		t.Fatalf("noeviction policy error = %v, want ErrRedisEviction", err)
	}
	if err := store.client.ConfigSet(ctx, "maxmemory", "0").Err(); err != nil {
		t.Fatal(err)
	}
	if err := RedisPolicy(ctx, rawURL); !errors.Is(err, ErrRedisEviction) {
		t.Fatalf("unbounded cache policy error = %v, want ErrRedisEviction", err)
	}
}

func TestStateRedisPolicyWithContainer(t *testing.T) {
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
	rawURL := "redis://" + host + ":" + port.Port() + "/0"
	store, err := NewRedisStore(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if err := store.client.ConfigSet(ctx, "maxmemory", "1048576").Err(); err != nil {
		t.Fatal(err)
	}
	if err := store.client.ConfigSet(ctx, "maxmemory-policy", "allkeys-lru").Err(); err != nil {
		t.Fatal(err)
	}
	if err := StateRedisPolicy(ctx, rawURL); !errors.Is(err, ErrRedisEviction) {
		t.Fatalf("allkeys-lru state policy error = %v, want ErrRedisEviction", err)
	}
	if err := store.client.ConfigSet(ctx, "maxmemory-policy", "noeviction").Err(); err != nil {
		t.Fatal(err)
	}
	if err := StateRedisPolicy(ctx, rawURL); err != nil {
		t.Fatalf("noeviction state policy error = %v", err)
	}
	if err := store.client.ConfigSet(ctx, "maxmemory", "0").Err(); err != nil {
		t.Fatal(err)
	}
	if err := store.client.ConfigSet(ctx, "maxmemory-policy", "allkeys-lru").Err(); err != nil {
		t.Fatal(err)
	}
	if err := StateRedisPolicy(ctx, rawURL); err != nil {
		t.Fatalf("unbounded state policy error = %v", err)
	}
}
