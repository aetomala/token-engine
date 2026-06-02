package reconciliation

import (
	"context"
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
// using distributed locks to prevent concurrent reconciliation passes.
type CursorReconciler struct {
	managers map[string]tokens.TokenManager
	locker   lock.Locker
	redis    *redis.Client
	logger   observability.Logger
	metrics  observability.Metrics
	pageSize int
	lockTTL  time.Duration
}

var _ Reconciler = (*CursorReconciler)(nil)

// NewCursorReconciler returns a new CursorReconciler.
func NewCursorReconciler(
	managers map[string]tokens.TokenManager,
	locker lock.Locker,
	redisClient *redis.Client,
	logger observability.Logger,
	metrics observability.Metrics,
	pageSize int,
	lockTTL time.Duration,
) *CursorReconciler {
	return &CursorReconciler{
		managers: managers,
		locker:   locker,
		redis:    redisClient,
		logger:   logger,
		metrics:  metrics,
		pageSize: pageSize,
		lockTTL:  lockTTL,
	}
}

// Run executes a single reconciliation pass over all tenants.
// It acquires a per-tenant distributed lock before processing and uses a Redis-persisted
// cursor to resume paginated ListTokens calls across restarts.
func (r *CursorReconciler) Run(ctx context.Context) error {
	for tenantID := range r.managers {
		select {
		case <-ctx.Done():
			return nil
		default:
		}
		r.processTenant(ctx, tenantID)
	}
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
