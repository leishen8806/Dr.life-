package iam

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

type SessionCache interface {
	Get(context.Context, string, string) (bool, bool, error)
	Put(context.Context, string, string, int64, time.Duration) error
	Revoke(context.Context, string, time.Duration) error
	SetAccountEpoch(context.Context, string, int64, time.Duration) error
}

type redisSessionValue struct {
	AccountID string `json:"account_id"`
	Epoch     int64  `json:"epoch"`
}

type RedisSessionCache struct{ client *redis.Client }

func NewRedisSessionCache(rawURL string) (*RedisSessionCache, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, errors.New("invalid Redis URL")
	}
	options.DialTimeout, options.ReadTimeout, options.WriteTimeout = 500*time.Millisecond, 500*time.Millisecond, 500*time.Millisecond
	options.MaxRetries = -1
	return &RedisSessionCache{client: redis.NewClient(options)}, nil
}

func (c *RedisSessionCache) Get(ctx context.Context, accountID, sessionID string) (bool, bool, error) {
	values, err := c.client.MGet(ctx, sessionCacheKey(sessionID), revokedCacheKey(sessionID), epochCacheKey(accountID)).Result()
	if err != nil {
		return false, false, ErrSessionStoreUnavailable
	}
	if values[0] == nil {
		return false, false, nil
	}
	var session redisSessionValue
	if err := json.Unmarshal([]byte(values[0].(string)), &session); err != nil || session.AccountID != accountID {
		return false, false, nil
	}
	epoch := int64(0)
	if values[2] != nil {
		epoch, err = strconv.ParseInt(values[2].(string), 10, 64)
		if err != nil {
			return false, false, ErrSessionStoreUnavailable
		}
	}
	return values[1] == nil && session.Epoch == epoch, true, nil
}

var putSessionScript = redis.NewScript(`
local current = redis.call('GET', KEYS[2])
if not current or tonumber(current) <= tonumber(ARGV[2]) then
  redis.call('SET', KEYS[2], ARGV[2], 'PX', ARGV[3])
end
redis.call('SET', KEYS[1], ARGV[1], 'PX', ARGV[3])
return 1
`)

func (c *RedisSessionCache) Put(ctx context.Context, accountID, sessionID string, epoch int64, ttl time.Duration) error {
	value, _ := json.Marshal(redisSessionValue{AccountID: accountID, Epoch: epoch})
	if err := putSessionScript.Run(ctx, c.client, []string{sessionCacheKey(sessionID), epochCacheKey(accountID)}, value, epoch, ttl.Milliseconds()).Err(); err != nil {
		return ErrSessionStoreUnavailable
	}
	return nil
}

func (c *RedisSessionCache) Revoke(ctx context.Context, sessionID string, ttl time.Duration) error {
	if err := c.client.Set(ctx, revokedCacheKey(sessionID), "1", ttl).Err(); err != nil {
		return ErrSessionStoreUnavailable
	}
	return nil
}

func (c *RedisSessionCache) SetAccountEpoch(ctx context.Context, accountID string, epoch int64, ttl time.Duration) error {
	if err := c.client.Set(ctx, epochCacheKey(accountID), epoch, ttl).Err(); err != nil {
		return ErrSessionStoreUnavailable
	}
	return nil
}

func (c *RedisSessionCache) Close() error { return c.client.Close() }

func sessionCacheKey(id string) string { return "drlife:iam:session:" + id }
func revokedCacheKey(id string) string { return "drlife:iam:revoked:" + id }
func epochCacheKey(id string) string   { return "drlife:iam:epoch:" + id }
