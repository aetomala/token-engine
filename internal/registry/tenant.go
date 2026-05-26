package registry

import (
	"context"

	"github.com/aetomala/jwtauth/pkg/tokens"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aetomala/token-engine/internal/observability"
)

// TenantRegistry provides tenant-aware token manager lookup.
type TenantRegistry interface {
	// Get returns the Manager for the given tenantID.
	// Returns codes.NotFound if tenantID is unknown or draining.
	// Returns codes.InvalidArgument if tenantID is empty string "".
	// Absent tenant_id (zero-value from proto unmarshalling) routes to the default tenant —
	// the registry receives "" only when the caller explicitly sends "".
	Get(ctx context.Context, tenantID string) (*tokens.Manager, error)
}

// StaticTenantRegistry is a v0.1 stub implementation of TenantRegistry.
// It returns Unimplemented for all inputs.
type StaticTenantRegistry struct {
	manager *tokens.Manager
	logger  observability.Logger
}

// NewStaticTenantRegistry returns a new StaticTenantRegistry.
// manager and logger are accepted but unused in v0.1.
func NewStaticTenantRegistry(manager *tokens.Manager, logger observability.Logger) *StaticTenantRegistry {
	return &StaticTenantRegistry{
		manager: manager,
		logger:  logger,
	}
}

// Get returns an Unimplemented error for all inputs in v0.1.
func (r *StaticTenantRegistry) Get(ctx context.Context, tenantID string) (*tokens.Manager, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
