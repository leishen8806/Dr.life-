// Package cache caches only explicitly public, shared data.
package cache

import (
	"container/list"
	"context"
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrNotFound      = errors.New("cache: source not found")
	ErrNamespace     = errors.New("cache: namespace is not cacheable")
	ErrRedisEviction = errors.New("cache: Redis eviction policy is unsafe")
)

type Entry struct{ Value []byte }

type Store interface {
	Get(context.Context, string) (Entry, bool, error)
	Set(context.Context, string, Entry, time.Duration) error
	Delete(context.Context, string) error
}

type Options struct {
	Environment, Version                                      string
	StaleTTL, NegativeTTL                                     time.Duration
	RefreshTimeout                                            time.Duration
	MaxEntries                                                int
	OnHit, OnMiss, OnRejected, OnLoad, OnDegraded, OnOversize func()
}

type record struct {
	entry                  Entry
	freshUntil, staleUntil time.Time
	negative               bool
}
type flight struct {
	done  chan struct{}
	value []byte
	err   error
}

type Client struct {
	options Options
	store   Store
	mu      sync.Mutex
	items   map[string]record
	flights map[string]*flight
	lru     *list.List
	lruPos  map[string]*list.Element
	now     func() time.Time
}

func NewMemory(options Options) *Client { return New(options, nil) }
func New(options Options, store Store) *Client {
	if options.Environment == "" {
		options.Environment = "dev"
	}
	if options.Version == "" {
		options.Version = "v1"
	}
	if options.StaleTTL <= 0 {
		options.StaleTTL = 30 * time.Second
	}
	if options.NegativeTTL <= 0 {
		options.NegativeTTL = 30 * time.Second
	}
	if options.RefreshTimeout <= 0 {
		options.RefreshTimeout = 2 * time.Second
	}
	if options.MaxEntries <= 0 {
		options.MaxEntries = 10000
	}
	return &Client{options: options, store: store, items: make(map[string]record), flights: make(map[string]*flight), lru: list.New(), lruPos: make(map[string]*list.Element), now: time.Now}
}

func (c *Client) MaxEntries() int { return c.options.MaxEntries }

const (
	maxValueBytes = 64 << 10
	pruneBatch    = 32
)

func (c *Client) touchLocked(key string) {
	if e := c.lruPos[key]; e != nil {
		c.lru.MoveToBack(e)
		return
	}
	c.lruPos[key] = c.lru.PushBack(key)
	for c.lru.Len() > c.options.MaxEntries {
		e := c.lru.Front()
		c.removeLocked(e.Value.(string))
	}
}

func (c *Client) removeLocked(key string) {
	delete(c.items, key)
	if e := c.lruPos[key]; e != nil {
		c.lru.Remove(e)
		delete(c.lruPos, key)
	}
}

var publicNamespaces = map[string]struct{}{"catalog": {}, "config": {}, "i18n": {}}
var cacheID = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,63}$`)
var sensitiveKeyParts = map[string]struct{}{"session": {}, "token": {}, "secret": {}, "key": {}, "user": {}, "phone": {}, "email": {}, "kyc": {}, "chat": {}, "ledger": {}, "balance": {}, "withdraw": {}, "order": {}, "stock": {}, "wallet": {}, "otp": {}, "password": {}}

func permitted(key string) bool {
	parts := strings.Split(key, ":")
	if len(parts) != 2 || !cacheID.MatchString(parts[1]) {
		return false
	}
	if _, ok := publicNamespaces[parts[0]]; !ok || sensitive(parts[0]) || sensitive(parts[1]) {
		return false
	}
	return true
}

func sensitive(value string) bool {
	for _, part := range strings.FieldsFunc(value, func(r rune) bool { return r == ':' || r == '-' || r == '_' || r == '.' }) {
		if _, blocked := sensitiveKeyParts[part]; blocked {
			return true
		}
	}
	return false
}

func (c *Client) pruneLocked(now time.Time) {
	for range pruneBatch {
		e := c.lru.Front()
		if e == nil {
			return
		}
		key := e.Value.(string)
		item, ok := c.items[key]
		if ok && now.Before(item.staleUntil) {
			return
		}
		c.removeLocked(key)
	}
}
func (c *Client) key(key string) string {
	return "drlife:" + c.options.Environment + ":" + c.options.Version + ":" + key
}
func (c *Client) rejected() {
	if c.options.OnRejected != nil {
		c.options.OnRejected()
	}
}
func (c *Client) hit() {
	if c.options.OnHit != nil {
		c.options.OnHit()
	}
}
func (c *Client) miss() {
	if c.options.OnMiss != nil {
		c.options.OnMiss()
	}
}
func (c *Client) load() {
	if c.options.OnLoad != nil {
		c.options.OnLoad()
	}
}
func (c *Client) degraded() {
	if c.options.OnDegraded != nil {
		c.options.OnDegraded()
	}
}
func (c *Client) oversize() {
	if c.options.OnOversize != nil {
		c.options.OnOversize()
	}
}
func (c *Client) check(key string) error {
	if !permitted(key) {
		c.rejected()
		return ErrNamespace
	}
	return nil
}

func (c *Client) Get(ctx context.Context, key string) (Entry, bool, error) {
	if err := c.check(key); err != nil {
		return Entry{}, false, err
	}
	c.mu.Lock()
	now := c.now()
	c.pruneLocked(now)
	item, ok := c.items[c.key(key)]
	c.mu.Unlock()
	if ok && now.Before(item.staleUntil) {
		c.mu.Lock()
		c.touchLocked(c.key(key))
		c.mu.Unlock()
		c.hit()
		if item.negative {
			return Entry{}, false, nil
		}
		return copyEntry(item.entry), true, nil
	}
	if c.store == nil {
		c.miss()
		return Entry{}, false, nil
	}
	entry, ok, err := c.store.Get(ctx, c.key(key))
	if err != nil {
		c.degraded()
		return Entry{}, false, err
	}
	if ok {
		now := c.now()
		if len(entry.Value) > maxValueBytes {
			c.oversize()
		} else {
			c.mu.Lock()
			c.pruneLocked(now)
			cacheKey := c.key(key)
			c.items[cacheKey] = record{entry: copyEntry(entry), freshUntil: now.Add(c.options.StaleTTL), staleUntil: now.Add(2 * c.options.StaleTTL)}
			c.touchLocked(cacheKey)
			c.mu.Unlock()
		}
		c.hit()
	} else {
		c.miss()
	}
	return copyEntry(entry), ok, nil
}
func (c *Client) Set(ctx context.Context, key string, value []byte, ttl time.Duration) error {
	if err := c.check(key); err != nil {
		return err
	}
	if ttl <= 0 {
		return errors.New("cache: ttl must be positive")
	}
	now := c.now()
	entry := Entry{Value: append([]byte(nil), value...)}
	if len(value) > maxValueBytes {
		c.oversize()
		return nil
	}
	c.mu.Lock()
	c.pruneLocked(now)
	c.items[c.key(key)] = record{entry: entry, freshUntil: now.Add(ttl), staleUntil: now.Add(ttl + c.options.StaleTTL)}
	c.touchLocked(c.key(key))
	c.mu.Unlock()
	if c.store != nil {
		if err := c.store.Set(ctx, c.key(key), entry, ttl+c.options.StaleTTL); err != nil {
			c.degraded()
		}
	}
	return nil
}
func (c *Client) Delete(ctx context.Context, key string) error {
	if err := c.check(key); err != nil {
		return err
	}
	c.mu.Lock()
	c.removeLocked(c.key(key))
	c.mu.Unlock()
	if c.store != nil {
		if err := c.store.Delete(ctx, c.key(key)); err != nil {
			c.degraded()
		}
	}
	return nil
}
func (c *Client) Remember(ctx context.Context, key string, ttl time.Duration, source func(context.Context) ([]byte, error)) ([]byte, error) {
	if err := c.check(key); err != nil {
		return nil, err
	}
	c.mu.Lock()
	now := c.now()
	c.pruneLocked(now)
	item, ok := c.items[c.key(key)]
	if ok && now.Before(item.freshUntil) {
		c.touchLocked(c.key(key))
		c.mu.Unlock()
		c.hit()
		if item.negative {
			return nil, ErrNotFound
		}
		return append([]byte(nil), item.entry.Value...), nil
	}
	if ok && now.Before(item.staleUntil) {
		c.touchLocked(c.key(key))
		c.mu.Unlock()
		c.refresh(key, ttl, source)
		c.hit()
		if item.negative {
			return nil, ErrNotFound
		}
		return append([]byte(nil), item.entry.Value...), nil
	}
	if f := c.flights[c.key(key)]; f != nil {
		c.mu.Unlock()
		return waitForFlight(ctx, f)
	}
	f := &flight{done: make(chan struct{})}
	c.flights[c.key(key)] = f
	c.mu.Unlock()
	c.startFlight(ctx, key, ttl, source, f)
	return waitForFlight(ctx, f)
}
func (c *Client) refresh(key string, ttl time.Duration, source func(context.Context) ([]byte, error)) {
	if source == nil {
		return
	}
	c.mu.Lock()
	if c.flights[c.key(key)] != nil {
		c.mu.Unlock()
		return
	}
	f := &flight{done: make(chan struct{})}
	c.flights[c.key(key)] = f
	c.mu.Unlock()
	c.startFlight(context.Background(), key, ttl, source, f)
}

func waitForFlight(ctx context.Context, f *flight) ([]byte, error) {
	select {
	case <-f.done:
		return append([]byte(nil), f.value...), f.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (c *Client) startFlight(parent context.Context, key string, ttl time.Duration, source func(context.Context) ([]byte, error), f *flight) {
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				f.err = fmt.Errorf("cache: source panic: %v", recovered)
			}
			c.mu.Lock()
			delete(c.flights, c.key(key))
			close(f.done)
			c.mu.Unlock()
		}()
		ctx, cancel := context.WithTimeout(context.WithoutCancel(parent), c.options.RefreshTimeout)
		defer cancel()
		f.value, f.err = c.fetch(ctx, key, ttl, source)
	}()
}
func (c *Client) fetch(ctx context.Context, key string, ttl time.Duration, source func(context.Context) ([]byte, error)) ([]byte, error) {
	c.miss()
	if c.store != nil {
		entry, ok, err := c.store.Get(ctx, c.key(key))
		if err != nil {
			c.degraded()
		} else if ok {
			now := c.now()
			if len(entry.Value) > maxValueBytes {
				c.oversize()
			} else {
				c.mu.Lock()
				c.pruneLocked(now)
				cacheKey := c.key(key)
				c.items[cacheKey] = record{entry: copyEntry(entry), freshUntil: now.Add(ttl), staleUntil: now.Add(ttl + c.options.StaleTTL)}
				c.touchLocked(cacheKey)
				c.mu.Unlock()
			}
			c.hit()
			return append([]byte(nil), entry.Value...), nil
		}
	}
	if source == nil {
		return nil, errors.New("cache: source is required")
	}
	c.load()
	value, err := callSource(ctx, source)
	if errors.Is(err, ErrNotFound) {
		now := c.now()
		c.mu.Lock()
		c.pruneLocked(now)
		cacheKey := c.key(key)
		c.items[cacheKey] = record{negative: true, freshUntil: now.Add(c.options.NegativeTTL), staleUntil: now.Add(c.options.NegativeTTL)}
		c.touchLocked(cacheKey)
		c.mu.Unlock()
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return value, c.Set(ctx, key, value, ttl)
}

func callSource(ctx context.Context, source func(context.Context) ([]byte, error)) ([]byte, error) {
	type result struct {
		value []byte
		err   error
	}
	resultCh := make(chan result, 1)
	go func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				resultCh <- result{err: fmt.Errorf("cache: source panic: %v", recovered)}
			}
		}()
		value, err := source(ctx)
		resultCh <- result{value: value, err: err}
	}()
	select {
	case result := <-resultCh:
		return result.value, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
func copyEntry(entry Entry) Entry { return Entry{Value: append([]byte(nil), entry.Value...)} }

type RedisStore struct{ client *redis.Client }

func NewRedisStore(rawURL string) (*RedisStore, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, errors.New("invalid Redis URL")
	}
	options.DialTimeout = 500 * time.Millisecond
	options.ReadTimeout = 500 * time.Millisecond
	options.WriteTimeout = 500 * time.Millisecond
	options.MaxRetries = -1
	return &RedisStore{client: redis.NewClient(options)}, nil
}

// RedisPolicy accepts the bounded allkeys-lru policy dedicated to public data.
// It must never be run against REDIS_URL, which serves identity and financial
// safety controls and is configured separately with noeviction.
func RedisPolicy(ctx context.Context, rawURL string) error {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return errors.New("invalid Redis URL")
	}
	client := redis.NewClient(options)
	defer client.Close()
	maxmemory, err := client.ConfigGet(ctx, "maxmemory").Result()
	if err != nil {
		return fmt.Errorf("read Redis maxmemory: %w", err)
	}
	limit, err := strconv.ParseInt(maxmemory["maxmemory"], 10, 64)
	if err != nil {
		return fmt.Errorf("parse Redis maxmemory: %w", err)
	}
	if limit <= 0 {
		return ErrRedisEviction
	}
	policy, err := client.ConfigGet(ctx, "maxmemory-policy").Result()
	if err != nil {
		return fmt.Errorf("read Redis maxmemory-policy: %w", err)
	}
	if policy["maxmemory-policy"] != "allkeys-lru" {
		return ErrRedisEviction
	}
	return nil
}

func (s *RedisStore) Get(ctx context.Context, key string) (Entry, bool, error) {
	value, err := s.client.Get(ctx, key).Bytes()
	if errors.Is(err, redis.Nil) {
		return Entry{}, false, nil
	}
	return Entry{Value: value}, err == nil, err
}
func (s *RedisStore) Set(ctx context.Context, key string, entry Entry, ttl time.Duration) error {
	return s.client.Set(ctx, key, entry.Value, ttl).Err()
}
func (s *RedisStore) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, key).Err()
}
func (s *RedisStore) Close() error { return s.client.Close() }
