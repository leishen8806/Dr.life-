package gateway

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/leishen8806/Dr.life-/backend/internal/platform/httpx"
	"github.com/redis/go-redis/v9"
)

type StepUpVerifier interface {
	Verify(context.Context, string, string, string) (string, error)
}

type JTIStore interface {
	Consume(context.Context, string, time.Duration) (bool, error)
}

type stepUpVerifiedKey struct{}

func StepUpVerified(ctx context.Context) bool {
	verified, _ := ctx.Value(stepUpVerifiedKey{}).(bool)
	return verified
}

func RequireStepUp(scope string, verifier StepUpVerifier, used JTIStore) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := r.Header.Get("X-Step-Up-Token")
			if token == "" {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeStepUpRequired))
				return
			}
			identity, ok := IdentityFromContext(r.Context())
			if !ok || verifier == nil || used == nil {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeStepUpInvalid))
				return
			}
			jti, err := verifier.Verify(r.Context(), token, scope, identity.AccountID)
			if err != nil || !validOpaqueID(jti) {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeStepUpInvalid))
				return
			}
			fresh, err := used.Consume(r.Context(), jti, 5*time.Minute)
			if err != nil || !fresh {
				httpx.WriteError(w, r, httpx.NewAppError(httpx.CodeStepUpInvalid))
				return
			}
			*r = *r.WithContext(context.WithValue(r.Context(), stepUpVerifiedKey{}, true))
			next.ServeHTTP(w, r)
		})
	}
}

type MemoryJTIStore struct {
	mu    sync.Mutex
	items map[string]time.Time
	now   func() time.Time
}

func NewMemoryJTIStore() *MemoryJTIStore {
	return &MemoryJTIStore{items: make(map[string]time.Time), now: time.Now}
}

func (s *MemoryJTIStore) Consume(_ context.Context, jti string, ttl time.Duration) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if expires, exists := s.items[jti]; exists && now.Before(expires) {
		return false, nil
	}
	s.items[jti] = now.Add(ttl)
	return true, nil
}

type RedisJTIStore struct{ client *redis.Client }

func NewRedisJTIStore(rawURL string) (*RedisJTIStore, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, errors.New("invalid Redis URL")
	}
	options.DialTimeout, options.ReadTimeout, options.WriteTimeout = 500*time.Millisecond, 500*time.Millisecond, 500*time.Millisecond
	options.MaxRetries = -1
	return &RedisJTIStore{client: redis.NewClient(options)}, nil
}

func (s *RedisJTIStore) Consume(ctx context.Context, jti string, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, "drlife:stepup:"+jti, "1", ttl).Result()
}

func (s *RedisJTIStore) Close() error { return s.client.Close() }
