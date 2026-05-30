package registry

import (
	"context"
	"fmt"
	"time"

	librarymetrics "github.com/aetomala/jwtauth/pkg/metrics"
	"github.com/aetomala/jwtauth/pkg/keys"
	"github.com/aetomala/jwtauth/pkg/storage"
	"github.com/aetomala/jwtauth/pkg/tokens"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aetomala/token-engine/internal/config"
	"github.com/aetomala/token-engine/internal/observability"
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

// ===== StaticTenantRegistry =====

// StaticTenantRegistry is a single-tenant implementation of TenantRegistry backed by Redis.
// It builds and owns the full jwtauth key manager and token manager stack for the configured issuer.
// All methods are safe for concurrent use.
var _ TenantRegistry = (*StaticTenantRegistry)(nil)

type StaticTenantRegistry struct {
	manager *tokens.Manager
	km      keys.KeyManager
	cfg     *config.Config
	logger  observability.Logger
}

// NewStaticTenantRegistry constructs and starts a StaticTenantRegistry. It builds a
// RedisKeyStore, KeyManager, RedisRefreshStore, and tokens.Manager in order, then starts
// the tokens.Manager — which in turn starts the KeyManager. Returns an error if any
// construction or startup step fails.
func NewStaticTenantRegistry(
	ctx context.Context,
	client *redis.Client,
	cfg *config.Config,
	promReg *prometheus.Registry,
	logger observability.Logger,
	tracer observability.Tracer,
	metrics observability.Metrics,
) (*StaticTenantRegistry, error) {
	// ===== STEP 1: RedisKeyStore =====
	keyStore, err := keys.NewRedisKeyStore(keys.RedisKeyStoreConfig{
		Client:    client,
		KeyPrefix: cfg.Issuer,
		Logger:    observability.NewLibraryLoggerAdapter(logger),
	})
	if err != nil {
		return nil, fmt.Errorf("creating redis key store for tenant %s: %w", cfg.Issuer, err)
	}

	// ===== STEP 2: KeyManager =====
	km, err := keys.NewManager(keys.KeyManagerConfig{
		KeyStore:  keyStore,
		Namespace: cfg.Issuer,
		Logger:    observability.NewLibraryLoggerAdapter(logger),
	})
	if err != nil {
		return nil, fmt.Errorf("creating key manager for tenant %s: %w", cfg.Issuer, err)
	}

	// ===== STEP 3: RedisRefreshStore =====
	refreshStore, err := storage.NewRedisRefreshStore(storage.RedisRefreshStoreConfig{
		Client:    client,
		KeyPrefix: cfg.Issuer,
		Logger:    observability.NewLibraryLoggerAdapter(logger),
	})
	if err != nil {
		return nil, fmt.Errorf("creating redis refresh store for tenant %s: %w", cfg.Issuer, err)
	}

	// ===== STEP 4: tokens.Manager =====
	manager, err := tokens.NewManager(tokens.TokenManagerConfig{
		KeyManager:   km,
		RefreshStore: refreshStore,
		Logger:       observability.NewLibraryLoggerAdapter(logger),
		Metrics:      librarymetrics.NewPrometheusMetrics(librarymetrics.PrometheusConfig{Registry: promReg}),
		Namespace:    cfg.Issuer,
		Issuer:       cfg.Issuer,
		Audience:     []string{cfg.Audience},
	})
	if err != nil {
		return nil, fmt.Errorf("creating token manager for tenant %s: %w", cfg.Issuer, err)
	}

	// ===== STEP 5: Start — tokens.Manager.Start also starts the KeyManager =====
	if err := manager.Start(ctx); err != nil {
		return nil, fmt.Errorf("starting token manager for tenant %s: %w", cfg.Issuer, err)
	}

	// ===== STEP 6: Initialize and return =====
	return &StaticTenantRegistry{
		manager: manager,
		km:      km,
		cfg:     cfg,
		logger:  logger,
	}, nil
}

// KeyManager returns the underlying keys.KeyManager. Used by main.go to call km.Stop()
// during graceful shutdown.
func (r *StaticTenantRegistry) KeyManager() keys.KeyManager {
	return r.km
}

// Add returns codes.Unimplemented — StaticTenantRegistry does not support dynamic tenant management.
// Use MultiTenantRegistry for Add/Drain/Remove lifecycle.
func (r *StaticTenantRegistry) Add(_ context.Context, _ string, _ TenantConfig) error {
	return status.Error(codes.Unimplemented, "Add not supported on StaticTenantRegistry — use MultiTenantRegistry")
}

// Drain returns codes.Unimplemented — StaticTenantRegistry does not support dynamic tenant management.
func (r *StaticTenantRegistry) Drain(_ context.Context, _ string) error {
	return status.Error(codes.Unimplemented, "Drain not supported on StaticTenantRegistry — use MultiTenantRegistry")
}

// Remove returns codes.Unimplemented — StaticTenantRegistry does not support dynamic tenant management.
func (r *StaticTenantRegistry) Remove(_ context.Context, _ string) error {
	return status.Error(codes.Unimplemented, "Remove not supported on StaticTenantRegistry — use MultiTenantRegistry")
}

// Get returns the tokens.Manager for the given tenantID.
// Returns codes.InvalidArgument if tenantID is empty.
// Returns codes.NotFound if tenantID does not match the configured issuer.
func (r *StaticTenantRegistry) Get(ctx context.Context, tenantID string) (tokens.TokenManager, error) {
	// ===== STEP 1: Validate tenant ID =====
	if tenantID == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id must not be empty")
	}
	// ===== STEP 2: Match against configured issuer =====
	if tenantID != r.cfg.Issuer {
		return nil, status.Error(codes.NotFound, "tenant not found")
	}
	return r.manager, nil
}
