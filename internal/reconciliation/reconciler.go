package reconciliation

import (
	"context"
	"time"
)

// Reconciler provides the reconciliation interface for background token store reconciliation.
type Reconciler interface {
	// Run executes a single reconciliation pass. Returns when ctx is cancelled.
	// Implementations must be idempotent — restarting from the beginning must be safe.
	Run(ctx context.Context) error
}

// NoOpReconciler is a no-op implementation of the Reconciler interface.
// All methods return nil safely — suitable for testing and optional reconciliation pipelines.
type NoOpReconciler struct{}

// NewNoOpReconciler returns a new NoOpReconciler.
func NewNoOpReconciler() *NoOpReconciler {
	return &NoOpReconciler{}
}

// Run executes a reconciliation pass. Always returns nil immediately without blocking.
func (r *NoOpReconciler) Run(ctx context.Context) error {
	return nil
}

// LastSuccessAt returns the current time — the NoOp reconciler is always considered healthy.
func (r *NoOpReconciler) LastSuccessAt() time.Time {
	return time.Now()
}
