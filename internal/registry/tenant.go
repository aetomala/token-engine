package registry

import (
	"context"
	"time"

	"github.com/aetomala/jwtauth/pkg/tokens"
)

// TenantRegistry provides tenant-aware token manager lookup and lifecycle management.
// All methods are safe for concurrent use.
type TenantRegistry interface {
	// Get returns the TokenManager for tenantID.
	// Returns codes.InvalidArgument if tenantID is "".
	// Returns codes.NotFound if tenantID is unknown or draining.
	Get(ctx context.Context, tenantID string) (tokens.TokenManager, error)

	// Add constructs and registers a new tenant.
	// Starts the tenant's KeyManager before returning.
	// Returns codes.AlreadyExists if tenantID is already registered.
	// Returns codes.InvalidArgument if tenantID is "", cfg.Issuer is "", or cfg.Audience is "".
	Add(ctx context.Context, tenantID string, cfg TenantConfig) error

	// Drain marks tenantID as draining. Subsequent Get calls return codes.NotFound.
	// In-flight requests already past the registry lookup are unaffected.
	// Returns codes.NotFound if tenantID is not registered.
	// Returns codes.InvalidArgument if tenantID is "".
	Drain(ctx context.Context, tenantID string) error

	// Remove stops the tenant's KeyManager and removes it from the registry.
	// Must be called only after Drain.
	// Returns codes.NotFound if tenantID is not registered.
	// Returns codes.InvalidArgument if tenantID is "".
	Remove(ctx context.Context, tenantID string) error
}

// TenantConfig is the per-tenant configuration passed to TenantRegistry.Add.
// Issuer is used as the Redis key prefix, library namespace, and JWT iss claim.
// Audience is the default JWT audience for tokens issued under this tenant.
type TenantConfig struct {
	Issuer   string
	Audience string
}

// ===== Constants =====

const (
	RedisStartupRetryInterval = 2 * time.Second
	RedisStartupRetryMaxWait  = 30 * time.Second
)
