package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/redis/go-redis/v9"
)

type idempotencyKey struct{}

func IdempotencyKeyFromContext(ctx context.Context) string {
	value, _ := ctx.Value(idempotencyKey{}).(string)
	return value
}

func RequireIdempotency(exempt func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !writeMethod(r.Method) || exempt != nil && exempt(r) {
				next.ServeHTTP(w, r)
				return
			}
			key := r.Header.Get("Idempotency-Key")
			if !validOpaqueID(key) {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
				return
			}
			*r = *r.WithContext(context.WithValue(r.Context(), idempotencyKey{}, key))
			next.ServeHTTP(w, r)
		})
	}
}

type IdempotencyEntry struct {
	BodyHash    string    `json:"body_hash"`
	State       string    `json:"state"`
	Status      int       `json:"status,omitempty"`
	Body        []byte    `json:"body,omitempty"`
	ContentType string    `json:"content_type,omitempty"`
	Truncated   bool      `json:"truncated,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
}

type IdempotencyStore interface {
	Begin(context.Context, string, string, time.Duration) (IdempotencyEntry, bool, error)
	Complete(context.Context, string, IdempotencyEntry, time.Duration) error
	Delete(context.Context, string) error
}

type memoryIdempotencyItem struct {
	entry   IdempotencyEntry
	expires time.Time
}

type MemoryIdempotencyStore struct {
	mu    sync.Mutex
	items map[string]memoryIdempotencyItem
	now   func() time.Time
}

func NewMemoryIdempotencyStore() *MemoryIdempotencyStore {
	return &MemoryIdempotencyStore{items: make(map[string]memoryIdempotencyItem), now: time.Now}
}

func (s *MemoryIdempotencyStore) Begin(_ context.Context, key, hash string, ttl time.Duration) (IdempotencyEntry, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if item, ok := s.items[key]; ok && now.Before(item.expires) {
		return item.entry, false, nil
	}
	entry := IdempotencyEntry{BodyHash: hash, State: "processing", CreatedAt: now}
	s.items[key] = memoryIdempotencyItem{entry: entry, expires: now.Add(ttl)}
	return entry, true, nil
}

func (s *MemoryIdempotencyStore) Complete(_ context.Context, key string, entry IdempotencyEntry, ttl time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.items[key] = memoryIdempotencyItem{entry: entry, expires: s.now().Add(ttl)}
	return nil
}

func (s *MemoryIdempotencyStore) Delete(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.items, key)
	return nil
}

type RedisIdempotencyStore struct{ client *redis.Client }

func NewRedisIdempotencyStore(rawURL string) (*RedisIdempotencyStore, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, errors.New("invalid Redis URL")
	}
	options.DialTimeout, options.ReadTimeout, options.WriteTimeout = 500*time.Millisecond, 500*time.Millisecond, 500*time.Millisecond
	options.MaxRetries = -1
	return &RedisIdempotencyStore{client: redis.NewClient(options)}, nil
}

func (s *RedisIdempotencyStore) Begin(ctx context.Context, key, hash string, ttl time.Duration) (IdempotencyEntry, bool, error) {
	entry := IdempotencyEntry{BodyHash: hash, State: "processing", CreatedAt: time.Now().UTC()}
	encoded, _ := json.Marshal(entry)
	redisKey := "drlife:idem:" + key
	created, err := s.client.SetNX(ctx, redisKey, encoded, ttl).Result()
	if err != nil {
		return IdempotencyEntry{}, false, err
	}
	if created {
		return entry, true, nil
	}
	raw, err := s.client.Get(ctx, redisKey).Bytes()
	if err != nil {
		return IdempotencyEntry{}, false, err
	}
	if err := json.Unmarshal(raw, &entry); err != nil {
		return IdempotencyEntry{}, false, err
	}
	return entry, false, nil
}

func (s *RedisIdempotencyStore) Complete(ctx context.Context, key string, entry IdempotencyEntry, ttl time.Duration) error {
	encoded, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return s.client.Set(ctx, "drlife:idem:"+key, encoded, ttl).Err()
}

func (s *RedisIdempotencyStore) Delete(ctx context.Context, key string) error {
	return s.client.Del(ctx, "drlife:idem:"+key).Err()
}

func (s *RedisIdempotencyStore) Close() error { return s.client.Close() }

type IdempotencyOptions struct {
	Store             IdempotencyStore
	FallbackStore     IdempotencyStore
	TTL               time.Duration
	MaxResponseBytes  int
	FinancialPrefixes []string
	OnDegraded        func()
}

func BindIdempotency(options IdempotencyOptions) func(http.Handler) http.Handler {
	if options.TTL <= 0 {
		options.TTL = 24 * time.Hour
	}
	if options.MaxResponseBytes <= 0 {
		options.MaxResponseBytes = 64 << 10
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			key := IdempotencyKeyFromContext(r.Context())
			if key == "" || !writeMethod(r.Method) {
				next.ServeHTTP(w, r)
				return
			}
			body, err := io.ReadAll(r.Body)
			if err != nil {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeInvalidArgument))
				return
			}
			r.Body = io.NopCloser(bytes.NewReader(body))
			sum := sha256.Sum256(body)
			hash := hex.EncodeToString(sum[:])
			identity, _ := IdentityFromContext(r.Context())
			scope := identity.AccountID
			if scope == "" {
				scope = "anonymous"
			}
			route := chi.RouteContext(r.Context()).RoutePattern()
			if route == "" {
				route = r.URL.Path
			}
			storeKey := scope + ":" + r.Method + ":" + route + ":" + key
			activeStore := options.Store
			usingFallback := false
			if activeStore == nil {
				if financialPath(r.URL.Path, options.FinancialPrefixes) {
					httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
					return
				}
				if options.OnDegraded != nil {
					options.OnDegraded()
				}
				activeStore = options.FallbackStore
				usingFallback = true
				if activeStore == nil {
					next.ServeHTTP(w, r)
					return
				}
			}
			entry, acquired, err := activeStore.Begin(r.Context(), storeKey, hash, options.TTL)
			if err != nil {
				if financialPath(r.URL.Path, options.FinancialPrefixes) {
					httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeServiceUnavailable))
					return
				}
				if options.OnDegraded != nil {
					options.OnDegraded()
				}
				activeStore = options.FallbackStore
				if activeStore == nil || usingFallback {
					next.ServeHTTP(w, r)
					return
				}
				usingFallback = true
				entry, acquired, err = activeStore.Begin(r.Context(), storeKey, hash, options.TTL)
				if err != nil {
					next.ServeHTTP(w, r)
					return
				}
			}
			if !acquired {
				if entry.BodyHash != hash {
					httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeIdempotencyConflict))
					return
				}
				if entry.State == "processing" {
					w.Header().Set("Retry-After", "1")
					httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeIdempotencyPending))
					return
				}
				w.Header().Set("Idempotent-Replayed", "true")
				if entry.Truncated {
					httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeAlreadyProcessed))
					return
				}
				if entry.ContentType != "" {
					w.Header().Set("Content-Type", entry.ContentType)
				}
				w.WriteHeader(entry.Status)
				_, _ = w.Write(entry.Body)
				return
			}
			recorder := &idempotencyWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(recorder, r)
			if recorder.status >= 500 {
				_ = activeStore.Delete(r.Context(), storeKey)
				return
			}
			entry.State, entry.Status = "done", recorder.status
			entry.ContentType = recorder.Header().Get("Content-Type")
			if recorder.body.Len() <= options.MaxResponseBytes {
				entry.Body = append([]byte(nil), recorder.body.Bytes()...)
			} else {
				entry.Truncated = true
			}
			_ = activeStore.Complete(r.Context(), storeKey, entry, options.TTL)
		})
	}
}

type idempotencyWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
	body        bytes.Buffer
}

func (w *idempotencyWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.wroteHeader = true
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func (w *idempotencyWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	w.body.Write(body)
	return w.ResponseWriter.Write(body)
}

func (w *idempotencyWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func financialPath(path string, prefixes []string) bool {
	for _, prefix := range prefixes {
		if path == prefix || strings.HasPrefix(path, strings.TrimSuffix(prefix, "/")+"/") {
			return true
		}
	}
	return false
}

func writeMethod(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func validOpaqueID(value string) bool {
	if len(value) < 1 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if (char < 'a' || char > 'z') && (char < 'A' || char > 'Z') && (char < '0' || char > '9') && char != '-' && char != '_' {
			return false
		}
	}
	return true
}
