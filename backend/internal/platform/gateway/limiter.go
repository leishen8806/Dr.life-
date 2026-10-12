package gateway

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/redis/go-redis/v9"
)

type Rule struct {
	Limit  int
	Window time.Duration
}

type RuleSet struct {
	Default    Rule
	Login      Rule
	Withdrawal Rule
	AI         Rule
}

type Limiter interface {
	Allow(context.Context, string, Rule) (bool, time.Duration, error)
}

type counter struct {
	count int
	until time.Time
}

type MemoryLimiter struct {
	mu        sync.Mutex
	counters  map[string]counter
	now       func() time.Time
	nextSweep time.Time
}

func NewMemoryLimiter() *MemoryLimiter {
	return &MemoryLimiter{counters: make(map[string]counter), now: time.Now}
}

func (l *MemoryLimiter) Allow(_ context.Context, key string, rule Rule) (bool, time.Duration, error) {
	if err := validateRule(rule); err != nil {
		return false, 0, err
	}
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if !now.Before(l.nextSweep) {
		for itemKey, item := range l.counters {
			if !now.Before(item.until) {
				delete(l.counters, itemKey)
			}
		}
		l.nextSweep = now.Add(time.Minute)
	}
	current := l.counters[key]
	if !now.Before(current.until) {
		current = counter{until: now.Add(rule.Window)}
	}
	current.count++
	l.counters[key] = current
	if current.count <= rule.Limit {
		return true, 0, nil
	}
	return false, current.until.Sub(now), nil
}

type RedisLimiter struct {
	client *redis.Client
}

var incrementWindow = redis.NewScript(`
local count = redis.call('INCR', KEYS[1])
if count == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[1]) end
return count
`)

func NewRedisLimiter(rawURL string) (*RedisLimiter, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, errors.New("invalid Redis URL")
	}
	options.DialTimeout = 500 * time.Millisecond
	options.ReadTimeout = 500 * time.Millisecond
	options.WriteTimeout = 500 * time.Millisecond
	options.MaxRetries = -1
	return &RedisLimiter{client: redis.NewClient(options)}, nil
}

func (l *RedisLimiter) Allow(ctx context.Context, key string, rule Rule) (bool, time.Duration, error) {
	if err := validateRule(rule); err != nil {
		return false, 0, err
	}
	bucket := time.Now().UnixNano() / rule.Window.Nanoseconds()
	redisKey := "drlife:rate:" + key + ":" + strconv.FormatInt(bucket, 10)
	count, err := incrementWindow.Run(ctx, l.client, []string{redisKey}, rule.Window.Milliseconds()).Int64()
	if err != nil {
		return false, 0, err
	}
	if count <= int64(rule.Limit) {
		return true, 0, nil
	}
	retry, err := l.client.PTTL(ctx, redisKey).Result()
	if err != nil || retry <= 0 {
		retry = rule.Window
	}
	return false, retry, nil
}

func validateRule(rule Rule) error {
	if rule.Limit <= 0 || rule.Window <= 0 {
		return errors.New("invalid rate-limit rule")
	}
	return nil
}

func (l *RedisLimiter) Close() error { return l.client.Close() }

type FallbackLimiter struct {
	Primary  Limiter
	Fallback Limiter
	Logger   *slog.Logger
	warnOnce sync.Once
}

func (l *FallbackLimiter) Allow(ctx context.Context, key string, rule Rule) (bool, time.Duration, error) {
	allowed, retry, err := l.Primary.Allow(ctx, key, rule)
	if err == nil {
		return allowed, retry, nil
	}
	if l.Logger != nil {
		l.warnOnce.Do(func() {
			l.Logger.WarnContext(ctx, "rate limiter unavailable; using process-local fallback")
		})
	}
	return l.Fallback.Allow(ctx, key, rule)
}

type ClientIPResolver struct {
	Header  string
	Trusted []*net.IPNet
}

func NewClientIPResolver(header string, cidrs []string) (ClientIPResolver, error) {
	resolver := ClientIPResolver{Header: header}
	for _, value := range cidrs {
		_, network, err := net.ParseCIDR(value)
		if err != nil {
			return ClientIPResolver{}, fmt.Errorf("invalid trusted proxy CIDR")
		}
		resolver.Trusted = append(resolver.Trusted, network)
	}
	return resolver, nil
}

func (r ClientIPResolver) Resolve(request *http.Request) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	direct := net.ParseIP(host)
	if r.Header != "" && direct != nil && r.isTrusted(direct) {
		if forwarded := net.ParseIP(strings.TrimSpace(request.Header.Get(r.Header))); forwarded != nil {
			return rateLimitAddress(forwarded)
		}
	}
	if direct == nil {
		return "unknown"
	}
	return rateLimitAddress(direct)
}

func rateLimitAddress(ip net.IP) string {
	if ipv4 := ip.To4(); ipv4 != nil {
		return ipv4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String() + "/64"
}

func (r ClientIPResolver) isTrusted(ip net.IP) bool {
	for _, network := range r.Trusted {
		if network.Contains(ip) {
			return true
		}
	}
	return false
}

func LimitByIP(limiter Limiter, resolver ClientIPResolver, rules RuleSet) func(http.Handler) http.Handler {
	return limit(limiter, rules, func(r *http.Request) (string, bool) {
		return "ip:" + resolver.Resolve(r), true
	})
}

func LimitByAccount(limiter Limiter, rules RuleSet) func(http.Handler) http.Handler {
	return limit(limiter, rules, func(r *http.Request) (string, bool) {
		identity, ok := IdentityFromContext(r.Context())
		return "account:" + identity.AccountID, ok
	})
}

func limit(limiter Limiter, rules RuleSet, subject func(*http.Request) (string, bool)) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path == "/healthz" || r.URL.Path == "/readyz" {
				next.ServeHTTP(w, r)
				return
			}
			key, ok := subject(r)
			if limiter == nil || !ok {
				next.ServeHTTP(w, r)
				return
			}
			rule, group := selectRule(r.URL.Path, rules)
			allowed, retry, err := limiter.Allow(r.Context(), key+":"+group, rule)
			if err != nil {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
				return
			}
			if !allowed {
				seconds := int64((retry + time.Second - 1) / time.Second)
				if seconds < 1 {
					seconds = 1
				}
				w.Header().Set("Retry-After", strconv.FormatInt(seconds, 10))
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeTooManyRequests))
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func selectRule(path string, rules RuleSet) (Rule, string) {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for _, part := range parts {
		switch part {
		case "auth", "login":
			return rules.Login, "login"
		case "withdraw", "withdrawals":
			return rules.Withdrawal, "withdrawal"
		case "ai":
			return rules.AI, "ai"
		}
	}
	if len(parts) >= 2 {
		return rules.Default, strings.Join(parts[:2], "/")
	}
	return rules.Default, "other"
}
