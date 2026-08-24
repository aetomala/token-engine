package registry

import (
	"context"

	"github.com/aetomala/token-engine/internal/observability"
)

// RunExpiryIndexBackfill runs BackfillExpiryIndex once for every tenant in backfillers. It is
// intended to be called once at startup, gated behind an operator-controlled flag, to migrate
// tokens stored before jwtauth's expiry-indexed Cleanup rewrite. A per-tenant failure is logged
// and does not halt the pass for the remaining tenants — it leaves that tenant in its current
// state, safe to retry on a subsequent gated run since BackfillExpiryIndex is idempotent.
func RunExpiryIndexBackfill(ctx context.Context, backfillers map[string]ExpiryIndexBackfiller, logger observability.Logger) {
	for tenantID, backfiller := range backfillers {
		removed, indexed, err := backfiller.BackfillExpiryIndex(ctx)
		if err != nil {
			logger.Warn(ctx, "expiry index backfill failed", "tenant_id", tenantID, "error", err)
			continue
		}
		logger.Info(ctx, "expiry index backfill complete", "tenant_id", tenantID, "removed", removed, "indexed", indexed)
	}
}
