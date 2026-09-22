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
	// The TTL is determined by the store implementation's construction-time pending-claim
	// config — it is intentionally short, distinct from the TTL applied by Set, since SetNX
	// is used to claim a key before a handler runs, not to cache its final result.
	// Returns (true, nil) if the key was written.
	// Returns (false, nil) if the key already existed — concurrent write, not an error.
	// Returns (false, err) on store error.
	SetNX(ctx context.Context, key string, value []byte) (bool, error)

	// Set unconditionally overwrites key with value, applying the store implementation's
	// construction-time response TTL. Used to promote a pending claim written by SetNX to a
	// completed record once its handler has finished successfully.
	// Returns nil on success.
	// Returns err on store error.
	Set(ctx context.Context, key string, value []byte) error
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

// Set returns nil for all inputs.
func (n *NoOpIdempotencyStore) Set(ctx context.Context, key string, value []byte) error {
	return nil
}
