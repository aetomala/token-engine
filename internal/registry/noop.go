package registry

import "context"

// NoOpCallerRegistry is a CallerRegistry that permits every caller for every tenant.
// Suitable for TLS-disabled development deployments where no caller registry file is configured.
// All methods are safe for concurrent use.
type NoOpCallerRegistry struct{}

var _ CallerRegistry = (*NoOpCallerRegistry)(nil)

// NewNoOpCallerRegistry returns a NoOpCallerRegistry that allows all callers for all tenants.
func NewNoOpCallerRegistry() *NoOpCallerRegistry {
	return &NoOpCallerRegistry{}
}

// IsPermitted always returns (true, nil) — all callers are permitted.
func (r *NoOpCallerRegistry) IsPermitted(_ context.Context, _ string, _ string) (bool, error) {
	return true, nil
}
