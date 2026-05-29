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

// TenantRegistry provides tenant-aware token manager lookup.
type TenantRegistry interface {
	// Get returns the Manager for the given tenantID.
	// Returns codes.NotFound if tenantID is unknown or draining.
	// Returns codes.InvalidArgument if tenantID is empty string "".
	// Absent tenant_id (zero-value from proto unmarshalling) routes to the default tenant —
	// the registry receives "" only when the caller explicitly sends "".
	Get(ctx context.Context, tenantID string) (tokens.TokenManager, error)
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
