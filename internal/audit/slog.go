package audit

import (
	"context"
	"time"

	"github.com/aetomala/token-engine/internal/observability"
)

// Field key constants for RecordRevocation log output.
// All RecordRevocation call sites must reference these constants — not inline strings.
const (
	auditKeyTenantID       = "tenant_id"
	auditKeyCallerIdentity = "caller_identity"
	auditKeyTokenID        = "token_id"
	auditKeyTarget         = "target"
	auditKeyScope          = "scope"
	auditKeyOccurredAt     = "occurred_at"
)

// SlogAuditStore is an audit.Store implementation that writes structured log
// lines via the service's observability.Logger. All methods are safe for concurrent use.
type SlogAuditStore struct {
	logger observability.Logger
}

// Compile-time assertion — place immediately after struct declaration.
var _ Store = (*SlogAuditStore)(nil)

// NewSlogAuditStore constructs a SlogAuditStore that writes audit records
// via logger. logger must not be nil.
func NewSlogAuditStore(logger observability.Logger) *SlogAuditStore {
	return &SlogAuditStore{logger: logger}
}

// Ping verifies the store is reachable. Always returns nil for SlogAuditStore.
func (s *SlogAuditStore) Ping(ctx context.Context) error {
	return nil
}

// RecordRevocation writes a structured log line for a revocation event.
// All RevocationEvent fields are emitted as key-value pairs in declaration order.
// Always returns nil.
func (s *SlogAuditStore) RecordRevocation(ctx context.Context, event RevocationEvent) error {
	s.logger.Info(ctx, "token revoked",
		auditKeyTenantID, event.TenantID,
		auditKeyCallerIdentity, event.CallerIdentity,
		auditKeyTokenID, event.TokenID,
		auditKeyTarget, event.Target,
		auditKeyScope, event.Scope,
		auditKeyOccurredAt, event.OccurredAt.Format(time.RFC3339),
	)
	return nil
}
