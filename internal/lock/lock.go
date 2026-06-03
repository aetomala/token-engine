package lock

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/aetomala/token-engine/internal/observability"
)

// Locker acquires distributed locks backed by a shared store.
// All methods are safe for concurrent use.
type Locker interface {
	Acquire(ctx context.Context, key string, ttl time.Duration) (Lock, error)
}

// Lock represents a held distributed lock. Release must be called to free the lock.
// All methods are safe for concurrent use.
type Lock interface {
	Release(ctx context.Context) error
}

// RedisLock implements Locker using Redis SET NX PX for acquisition and a Lua
// compare-and-delete script for release. Suitable for per-operation mutual exclusion
// across multiple replicas. All methods are safe for concurrent use.
type RedisLock struct {
	client *redis.Client
	logger observability.Logger
}

var _ Locker = (*RedisLock)(nil)

// redisLockHandle is the held-lock value returned by Acquire.
type redisLockHandle struct {
	client *redis.Client
	key    string
	token  string
}

// NewRedisLocker returns a new RedisLock backed by client.
func NewRedisLocker(client *redis.Client, logger observability.Logger) *RedisLock {
	return &RedisLock{client: client, logger: logger}
}

// Acquire attempts to acquire a lock for key with the given TTL.
// Returns a non-nil Lock on success. Returns an error if the lock is already held
// or if the Redis operation fails.
func (r *RedisLock) Acquire(ctx context.Context, key string, ttl time.Duration) (Lock, error) {
	token := uuid.New().String()
	set, err := r.client.SetArgs(ctx, key, token, redis.SetArgs{
		Mode: "NX",
		TTL:  ttl,
	}).Result()
	if err != nil {
		return nil, err
	}
	if set != "OK" {
		return nil, errors.New("lock: already held")
	}
	return &redisLockHandle{client: r.client, key: key, token: token}, nil
}

// Release releases the lock using a Lua compare-and-delete script.
// It is idempotent — if the key no longer exists or was acquired by a different holder,
// the call returns nil.
func (h *redisLockHandle) Release(ctx context.Context) error {
	const script = `if redis.call("GET",KEYS[1]) == ARGV[1] then return redis.call("DEL",KEYS[1]) else return 0 end`
	return h.client.Eval(ctx, script, []string{h.key}, h.token).Err()
}
