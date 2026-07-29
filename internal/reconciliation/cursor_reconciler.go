package reconciliation

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/aetomala/jwtauth/pkg/tokens"
	"github.com/aetomala/token-engine/internal/lock"
	"github.com/aetomala/token-engine/internal/observability"
)

const (
	lockKeyReconciliationPrefix = "locks:reconciliation:"
)

// CursorReconciler is a best-effort reconciler that runs a single expired-token cleanup pass
// per tenant per Run, using a per-tenant distributed lock to prevent concurrent reconciliation
// passes across replicas. All methods are safe for concurrent use.
//
// The name is retained from ADR-011's original cursor-based, per-token design — that design
// specified paging through each tenant's tokens and revoking ones with no corresponding
// idempotency record. That per-token orphan detection was never implemented; see ADR-011's
// Outcome section for why. This implementation calls TokenManager.CleanupExpiredTokens once
// per tenant per pass, which is itself a self-contained full-namespace scan independent of
// any pagination.
type CursorReconciler struct {
	// ===== Observability =====
	logger  observability.Logger
	metrics observability.Metrics

	// ===== Dependencies =====
	managers map[string]tokens.TokenManager
	locker   lock.Locker

	// ===== Config =====
	lockTTL time.Duration

	// ===== State =====
	lastSuccessAt atomic.Int64 // Unix nanos of the last successful Run pass.
}

var _ Reconciler = (*CursorReconciler)(nil)

// NewCursorReconciler returns a new CursorReconciler. LastSuccessAt is initialized to the
// current time so the health checker grace window starts from server startup, not the epoch.
func NewCursorReconciler(
	managers map[string]tokens.TokenManager,
	locker lock.Locker,
	logger observability.Logger,
	metrics observability.Metrics,
	lockTTL time.Duration,
) *CursorReconciler {
	r := &CursorReconciler{
		managers: managers,
		locker:   locker,
		logger:   logger,
		metrics:  metrics,
		lockTTL:  lockTTL,
	}
	r.lastSuccessAt.Store(time.Now().UnixNano())
	return r
}

// LastSuccessAt returns the time of the last successful Run pass. Returns the construction
// time if no pass has completed yet — callers treat this as the start of the grace window.
func (r *CursorReconciler) LastSuccessAt() time.Time {
	return time.Unix(0, r.lastSuccessAt.Load())
}

// Run executes a single reconciliation pass over all tenants, cleaning up expired tokens for
// each under a per-tenant distributed lock. On successful completion of the full pass,
// lastSuccessAt is updated so health checks can observe liveness.
// Returns nil if the context is cancelled before all tenants are processed — cancellation is
// not treated as an error since the next pass covers any tenants skipped by this one.
func (r *CursorReconciler) Run(ctx context.Context) error {
	for tenantID := range r.managers {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		r.processTenant(ctx, tenantID)
	}

	// ===== Record successful pass =====
	r.lastSuccessAt.Store(time.Now().UnixNano())
	return nil
}

func (r *CursorReconciler) processTenant(ctx context.Context, tenantID string) {
	lk, err := r.locker.Acquire(ctx, lockKeyReconciliationPrefix+tenantID, r.lockTTL)
	if err != nil {
		r.logger.Info(ctx, "reconciler: lock not acquired, skipping tenant", "tenant_id", tenantID)
		return
	}
	defer lk.Release(ctx) //nolint:errcheck

	if _, err := r.managers[tenantID].CleanupExpiredTokens(ctx); err != nil {
		r.logger.Warn(ctx, "reconciler: cleanup expired tokens error", "tenant_id", tenantID, "error", err)
	}
}
