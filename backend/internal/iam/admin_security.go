package iam

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// AdminSecurityStore keeps administrator failure and TOTP replay state shared
// across all application instances. Authentication fails closed on store errors.
type AdminSecurityStore interface {
	ReserveAttempt(context.Context, string, string) (bool, error)
	ClearAccountFailures(context.Context, string) error
	ConsumeTOTP(context.Context, string, int64, time.Duration) (bool, error)
}

type RedisAdminSecurityStore struct{ client *redis.Client }

func NewRedisAdminSecurityStore(rawURL string) (*RedisAdminSecurityStore, error) {
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		return nil, errors.New("invalid Redis URL")
	}
	options.DialTimeout, options.ReadTimeout, options.WriteTimeout = 500*time.Millisecond, 500*time.Millisecond, 500*time.Millisecond
	options.MaxRetries = -1
	return &RedisAdminSecurityStore{client: redis.NewClient(options)}, nil
}

func (s *RedisAdminSecurityStore) Locked(ctx context.Context, account, ip string) (bool, error) {
	values, err := s.client.MGet(ctx, adminFailureKeys(account, ip)...).Result()
	if err != nil {
		return false, err
	}
	accountCount, _ := strconv.Atoi(stringValue(values[0]))
	ipCount, _ := strconv.Atoi(stringValue(values[1]))
	return accountCount >= adminLoginFailureLimit || ipCount >= adminLoginIPFailureLimit, nil
}

func (s *RedisAdminSecurityStore) ReserveAttempt(ctx context.Context, account, ip string) (bool, error) {
	keys := adminFailureKeys(account, ip)
	result, err := s.client.Eval(ctx, `
local account_count = tonumber(redis.call('GET', KEYS[1]) or '0')
local ip_count = tonumber(redis.call('GET', KEYS[2]) or '0')
if account_count >= tonumber(ARGV[1]) or ip_count >= tonumber(ARGV[2]) then
  return 0
end
account_count = redis.call('INCR', KEYS[1])
ip_count = redis.call('INCR', KEYS[2])
if account_count == 1 then redis.call('PEXPIRE', KEYS[1], ARGV[3]) end
if ip_count == 1 then redis.call('PEXPIRE', KEYS[2], ARGV[3]) end
return 1`, keys, adminLoginFailureLimit, adminLoginIPFailureLimit, adminLoginFailureWindow.Milliseconds()).Int()
	return result == 1, err
}

func (s *RedisAdminSecurityStore) ClearAccountFailures(ctx context.Context, account string) error {
	return s.client.Del(ctx, adminFailureKeys(account, "")[0]).Err()
}

func (s *RedisAdminSecurityStore) ConsumeTOTP(ctx context.Context, adminPublicID string, step int64, ttl time.Duration) (bool, error) {
	return s.client.SetNX(ctx, "drlife:admin:"+adminPublicID+":totp:"+strconv.FormatInt(step, 10), "1", ttl).Result()
}

func (s *RedisAdminSecurityStore) Close() error { return s.client.Close() }

func adminFailureKeys(account, ip string) []string {
	return []string{"drlife:admin-auth:account:" + hashAdminDimension(account), "drlife:admin-auth:ip:" + hashAdminDimension(ip)}
}

func hashAdminDimension(value string) string {
	digest := sha256.Sum256([]byte(value))
	return hex.EncodeToString(digest[:])
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	default:
		return ""
	}
}
