package lock

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrLockNotAcquired is returned when a lock key is already held.
var ErrLockNotAcquired = errors.New("lock not acquired")

// RedisLocker provides simple distributed locks backed by Redis.
type RedisLocker struct {
	client *redis.Client
	prefix string
}

// RedisLock represents a held lock.
type RedisLock struct {
	client *redis.Client
	key    string
	value  string
}

// NewRedisLocker creates a new locker with the given key prefix.
func NewRedisLocker(client *redis.Client, prefix string) *RedisLocker {
	if prefix == "" {
		prefix = "lock:"
	}
	return &RedisLocker{
		client: client,
		prefix: prefix,
	}
}

// Acquire tries to acquire a lock for the specified key with the given TTL.
// On success, it returns a RedisLock that must be released.
func (l *RedisLocker) Acquire(ctx context.Context, key string, ttl time.Duration) (*RedisLock, error) {
	fullKey := l.prefix + key
	value := randomLockValue()

	ok, err := l.client.SetNX(ctx, fullKey, value, ttl).Result()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrLockNotAcquired
	}
	return &RedisLock{
		client: l.client,
		key:    fullKey,
		value:  value,
	}, nil
}

// Release releases the held lock. It is safe to call multiple times.
func (l *RedisLock) Release(ctx context.Context) error {
	const script = `
if redis.call("GET", KEYS[1]) == ARGV[1] then
  return redis.call("DEL", KEYS[1])
else
  return 0
end`

	cmd := l.client.Eval(ctx, script, []string{l.key}, l.value)

	return cmd.Err()
}

func randomLockValue() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback to time-based data if crypto/rand fails.
		now := time.Now().UnixNano()
		return hex.EncodeToString([]byte{
			byte(now >> 56),
			byte(now >> 48),
			byte(now >> 40),
			byte(now >> 32),
			byte(now >> 24),
			byte(now >> 16),
			byte(now >> 8),
			byte(now),
		})
	}
	return hex.EncodeToString(b)
}
