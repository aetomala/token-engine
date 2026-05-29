package store

import "context"

// IdempotencyStore is the persistence layer for request deduplication.
// Implementations must be safe for concurrent use.
// TTL-based expiry handles cleanup — no explicit delete method.
type IdempotencyStore interface {
	// Get retrieves a cached response for key.
	// Returns (value, true, nil) on hit.
	// Returns (nil, false, nil) on miss (redis.Nil maps to this).
	// Returns (nil, false, err) on store error.
	Get(ctx context.Context, key string) ([]byte, bool, error)

	// SetNX stores value at key if and only if the key does not already exist.
	// The TTL is determined by the store implementation's construction-time config.
	// Returns (true, nil) if the key was written.
	// Returns (false, nil) if the key already existed — concurrent write, not an error.
	// Returns (false, err) on store error.
	SetNX(ctx context.Context, key string, value []byte) (bool, error)
}

// NoOpIdempotencyStore is a no-operation IdempotencyStore.
// Get always returns a miss. SetNX always returns (true, nil).
// Used in tests and as the pre-v0.4 stub.
type NoOpIdempotencyStore struct{}

// Compile-time interface assertion.
var _ IdempotencyStore = (*NoOpIdempotencyStore)(nil)

// NewNoOpIdempotencyStore returns a new NoOpIdempotencyStore.
func NewNoOpIdempotencyStore() *NoOpIdempotencyStore {
	return &NoOpIdempotencyStore{}
}

// Get returns (nil, false, nil) for all inputs — always a miss.
func (n *NoOpIdempotencyStore) Get(ctx context.Context, key string) ([]byte, bool, error) {
	return nil, false, nil
}

// SetNX returns (true, nil) for all inputs — always signals "written successfully".
func (n *NoOpIdempotencyStore) SetNX(ctx context.Context, key string, value []byte) (bool, error) {
	return true, nil
}
