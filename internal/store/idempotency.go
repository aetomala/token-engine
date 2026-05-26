package store

import (
	"context"
	"time"
)

// IdempotencyStore provides caching of request responses for idempotency.
type IdempotencyStore interface {
	// Get returns the cached response bytes for key, or (nil, nil) if not found.
	Get(ctx context.Context, key string) ([]byte, error)

	// SetNX stores value under key with the given TTL only if the key does not exist.
	// Returns (true, nil) if the key was set. Returns (false, nil) if the key already existed.
	// Returns (false, err) on store error.
	SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error)
}

// NoOpIdempotencyStore is a v0.1 stub implementation of IdempotencyStore.
// It does not cache any responses.
type NoOpIdempotencyStore struct{}

// NewNoOpIdempotencyStore returns a new NoOpIdempotencyStore.
func NewNoOpIdempotencyStore() *NoOpIdempotencyStore {
	return &NoOpIdempotencyStore{}
}

// Get returns (nil, nil) for all inputs.
func (s *NoOpIdempotencyStore) Get(ctx context.Context, key string) ([]byte, error) {
	return nil, nil
}

// SetNX returns (false, nil) for all inputs.
func (s *NoOpIdempotencyStore) SetNX(ctx context.Context, key string, value []byte, ttl time.Duration) (bool, error) {
	return false, nil
}
