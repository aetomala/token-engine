package audit

import (
	"context"
	"time"
)

// Store provides the audit log interface for recording compliance-grade events.
type Store interface {
	// RecordRevocation writes a durable audit record for a revocation event.
	// Returns an error if the record cannot be durably written.
	// D2: Revocation operations are gated on Store availability — a Store error aborts revocation.
	RecordRevocation(ctx context.Context, event RevocationEvent) error

	// Ping verifies the store is reachable. Used by the readiness probe.
	Ping(ctx context.Context) error
}

// Sentinel scope values for RevocationEvent.Scope.
const (
	RevocationScopeToken    = "token"
	RevocationScopeAudience = "audience"
	RevocationScopeUser     = "user"
)

// RevocationEvent captures a token revocation event for audit logging.
type RevocationEvent struct {
	TenantID       string
	CallerIdentity string
	TokenID        string    // populated for Scope="token"; "" otherwise
	Target         string    // populated for Scope="audience" (audience value) and Scope="user" (user ID); "" for Scope="token"
	Scope          string    // "token", "audience", "user"
	OccurredAt     time.Time
}

// NoOpAuditStore is a no-op implementation of the Store interface.
// All methods return nil safely — suitable for testing and optional audit pipelines.
type NoOpAuditStore struct{}

// NewNoOpAuditStore returns a new NoOpAuditStore.
func NewNoOpAuditStore() *NoOpAuditStore {
	return &NoOpAuditStore{}
}

// RecordRevocation records a revocation event. Always returns nil.
func (s *NoOpAuditStore) RecordRevocation(ctx context.Context, event RevocationEvent) error {
	return nil
}

// Ping verifies the store is reachable. Always returns nil.
func (s *NoOpAuditStore) Ping(ctx context.Context) error {
	return nil
}
