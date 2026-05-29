package store

import (
	"context"
	"errors"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisIdempotencyStore is a Redis-backed IdempotencyStore.
// TTL is fixed at construction time and applied on every SetNX call.
// All methods are safe for concurrent use.
type RedisIdempotencyStore struct {
	client redis.UniversalClient
	ttl    time.Duration
}

// Compile-time interface assertion.
var _ IdempotencyStore = (*RedisIdempotencyStore)(nil)

// NewRedisIdempotencyStore returns a new RedisIdempotencyStore using client with the given ttl.
func NewRedisIdempotencyStore(client redis.UniversalClient, ttl time.Duration) *RedisIdempotencyStore {
	return &RedisIdempotencyStore{client: client, ttl: ttl}
}

// Get retrieves a cached response for key.
// Returns (value, true, nil) on hit.
// Returns (nil, false, nil) on miss — redis.Nil is not treated as an error.
// Returns (nil, false, err) on store error.
func (s *RedisIdempotencyStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	val, err := s.client.Get(ctx, key).Bytes()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return val, true, nil
}

// SetNX stores value at key if and only if the key does not already exist.
// The TTL configured at construction is applied.
// Returns (true, nil) if the key was written.
// Returns (false, nil) if the key already existed — concurrent write, not an error.
// Returns (false, err) on store error.
func (s *RedisIdempotencyStore) SetNX(ctx context.Context, key string, value []byte) (bool, error) {
	ok, err := s.client.SetNX(ctx, key, value, s.ttl).Result()
	if err != nil {
		return false, err
	}
	return ok, nil
}
