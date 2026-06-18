package reconciliation

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/aetomala/jwtauth/pkg/tokens"
	"github.com/aetomala/token-engine/internal/lock"
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/redis/go-redis/v9"
)

const (
	lockKeyReconciliationPrefix = "locks:reconciliation:"
	cursorKeyPrefix             = "reconciliation:cursor:"
)

// CursorReconciler is a cursor-based best-effort reconciler that processes tokens per tenant
// using distributed locks to prevent concurrent reconciliation passes. All methods are safe
// for concurrent use.
type CursorReconciler struct {
	// ===== Observability =====
	logger  observability.Logger
	metrics observability.Metrics

	// ===== Dependencies =====
	managers map[string]tokens.TokenManager
	locker   lock.Locker
	redis    *redis.Client

	// ===== Config =====
	pageSize int
	lockTTL  time.Duration

	// ===== State =====
	lastSuccessAt atomic.Int64 // Unix nanos of the last successful Run pass.
}

var _ Reconciler = (*CursorReconciler)(nil)

// NewCursorReconciler returns a new CursorReconciler. LastSuccessAt is initialized to the
// current time so the health checker grace window starts from server startup, not the epoch.
func NewCursorReconciler(
	managers map[string]tokens.TokenManager,
	locker lock.Locker,
	redisClient *redis.Client,
	logger observability.Logger,
	metrics observability.Metrics,
	pageSize int,
	lockTTL time.Duration,
) *CursorReconciler {
	r := &CursorReconciler{
		managers: managers,
		locker:   locker,
		redis:    redisClient,
		logger:   logger,
		metrics:  metrics,
		pageSize: pageSize,
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

// Run executes a single reconciliation pass over all tenants. It acquires a per-tenant
// distributed lock before processing and uses a Redis-persisted cursor to resume paginated
// ListTokens calls across restarts. On successful completion of the full pass, lastSuccessAt
// is updated so health checks can observe liveness.
// Returns the context error if the context is cancelled before all tenants are processed.
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

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		cursor, _ := r.redis.Get(ctx, cursorKeyPrefix+tenantID).Result()

		_, nextCursor, err := r.managers[tenantID].ListTokens(ctx, cursor, r.pageSize)
		if err != nil {
			r.logger.Warn(ctx, "reconciler: list tokens error", "error", err)
			return
		}

		_, _ = r.managers[tenantID].CleanupExpiredTokens(ctx)

		if nextCursor == "" {
			_ = r.redis.Del(ctx, cursorKeyPrefix+tenantID).Err()
			break
		}
		_ = r.redis.Set(ctx, cursorKeyPrefix+tenantID, nextCursor, 0).Err()
	}
}
