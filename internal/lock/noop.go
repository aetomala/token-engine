package lock

import (
	"context"
	"time"
)

// NoOpLocker is a no-op implementation of Locker that always succeeds without
// acquiring any real lock. Suitable for single-node deployments and unit tests
// that do not require distributed mutual exclusion. All methods are safe for
// concurrent use.
type NoOpLocker struct{}

var _ Locker = (*NoOpLocker)(nil)

// NoOpLock is a no-op implementation of Lock returned by NoOpLocker.Acquire.
// Release is always a no-op. All methods are safe for concurrent use.
type NoOpLock struct{}

var _ Lock = (*NoOpLock)(nil)

// NewNoOpLocker returns a new NoOpLocker.
func NewNoOpLocker() *NoOpLocker {
	return &NoOpLocker{}
}

// Acquire returns a NoOpLock immediately without contacting any store.
// It never returns an error.
func (l *NoOpLocker) Acquire(_ context.Context, _ string, _ time.Duration) (Lock, error) {
	return &NoOpLock{}, nil
}

// Release is a no-op. It is idempotent — it always returns nil regardless of
// how many times it is called.
func (l *NoOpLock) Release(_ context.Context) error {
	return nil
}
