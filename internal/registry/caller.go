package registry

import (
	"context"

	"github.com/aetomala/token-engine/internal/observability"
)

// CallerRegistry provides caller authorization checks for tenant operations.
type CallerRegistry interface {
	// IsPermitted returns true if the given callerIdentity is authorized to operate on tenantID.
	// v0.1 stub: return true, nil for all inputs — all callers permitted.
	// v0.5: return false, codes.PermissionDenied if not in permitted_tenants list.
	IsPermitted(ctx context.Context, callerIdentity string, tenantID string) (bool, error)
}

// StaticCallerRegistry is a v0.1 stub implementation of CallerRegistry.
// All callers are permitted in v0.1.
type StaticCallerRegistry struct {
	logger observability.Logger
}

// NewStaticCallerRegistry returns a new StaticCallerRegistry.
// logger is accepted but unused in v0.1.
func NewStaticCallerRegistry(logger observability.Logger) *StaticCallerRegistry {
	return &StaticCallerRegistry{
		logger: logger,
	}
}

// IsPermitted returns true for all inputs in v0.1 — all callers are permitted.
func (r *StaticCallerRegistry) IsPermitted(ctx context.Context, callerIdentity string, tenantID string) (bool, error) {
	return true, nil
}
