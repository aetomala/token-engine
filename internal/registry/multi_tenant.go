package registry

import (
	"context"
	"fmt"
	"sync"

	librarymetrics "github.com/aetomala/jwtauth/pkg/metrics"
	"github.com/aetomala/jwtauth/pkg/keys"
	"github.com/aetomala/jwtauth/pkg/storage"
	"github.com/aetomala/jwtauth/pkg/tokens"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aetomala/token-engine/internal/observability"
)

const (
	tenantRegistryOpAdd    = "add"
	tenantRegistryOpDrain  = "drain"
	tenantRegistryOpRemove = "remove"
	tenantRegistryOpLabel  = "operation"
)

type tenantEntry struct {
	manager  tokens.TokenManager
	km       keys.KeyManager
	draining bool
}

// MultiTenantRegistry is a dynamic multi-tenant implementation of TenantRegistry.
// It supports runtime Add/Drain/Remove lifecycle for per-tenant token manager stacks.
// Each tenant gets an isolated key prefix, library namespace, and Prometheus metric namespace.
// All methods are safe for concurrent use.
type MultiTenantRegistry struct {
	client  *redis.Client
	promReg *prometheus.Registry
	logger  observability.Logger
	tracer  observability.Tracer
	metrics observability.Metrics
	mu      sync.RWMutex
	tenants map[string]*tenantEntry
}

var _ TenantRegistry = (*MultiTenantRegistry)(nil)

// NewMultiTenantRegistry returns a new, empty MultiTenantRegistry. The caller is
// responsible for calling Add before any Get calls.
func NewMultiTenantRegistry(
	client *redis.Client,
	promReg *prometheus.Registry,
	logger observability.Logger,
	tracer observability.Tracer,
	metrics observability.Metrics,
) *MultiTenantRegistry {
	return &MultiTenantRegistry{
		client:  client,
		promReg: promReg,
		logger:  logger,
		tracer:  tracer,
		metrics: metrics,
		tenants: make(map[string]*tenantEntry),
	}
}

// Add constructs and registers a new tenant stack — KeyStore, KeyManager, RefreshStore, and TokenManager.
// Starts the TokenManager (which internally starts the KeyManager) before returning.
// Returns codes.InvalidArgument if tenantID, cfg.Issuer, or cfg.Audience is "".
// Returns codes.AlreadyExists if tenantID is already registered.
func (r *MultiTenantRegistry) Add(ctx context.Context, tenantID string, cfg TenantConfig) error {
	// ===== STEP 1: Validate inputs (no lock) =====
	if tenantID == "" {
		return status.Error(codes.InvalidArgument, "tenantID must not be empty")
	}
	if cfg.Issuer == "" {
		return status.Error(codes.InvalidArgument, "cfg.Issuer must not be empty")
	}
	if cfg.Audience == "" {
		return status.Error(codes.InvalidArgument, "cfg.Audience must not be empty")
	}

	// ===== STEP 2: Acquire write lock =====
	r.mu.Lock()
	defer r.mu.Unlock()

	// ===== STEP 3: Check AlreadyExists =====
	if _, exists := r.tenants[tenantID]; exists {
		return status.Errorf(codes.AlreadyExists, "tenant %s is already registered", tenantID)
	}

	// ===== STEP 4: Build per-tenant stack =====
	keyStore, err := keys.NewRedisKeyStore(keys.RedisKeyStoreConfig{
		Client:    r.client,
		KeyPrefix: tenantID,
		Namespace: tenantID,
		Logger:    observability.NewLibraryLoggerAdapter(r.logger),
	})
	if err != nil {
		return fmt.Errorf("creating redis key store for tenant %s: %w", tenantID, err)
	}

	km, err := keys.NewManager(keys.KeyManagerConfig{
		KeyStore:  keyStore,
		Namespace: tenantID,
		Logger:    observability.NewLibraryLoggerAdapter(r.logger),
	})
	if err != nil {
		return fmt.Errorf("creating key manager for tenant %s: %w", tenantID, err)
	}

	refreshStore, err := storage.NewRedisRefreshStore(storage.RedisRefreshStoreConfig{
		Client:    r.client,
		KeyPrefix: tenantID,
		Namespace: tenantID,
		Logger:    observability.NewLibraryLoggerAdapter(r.logger),
	})
	if err != nil {
		return fmt.Errorf("creating redis refresh store for tenant %s: %w", tenantID, err)
	}

	manager, err := tokens.NewManager(tokens.TokenManagerConfig{
		KeyManager:   km,
		RefreshStore: refreshStore,
		Logger:       observability.NewLibraryLoggerAdapter(r.logger),
		Metrics: librarymetrics.NewPrometheusMetrics(librarymetrics.PrometheusConfig{
			Registry:  r.promReg,
			Namespace: tenantID,
		}),
		Namespace: tenantID,
		Issuer:    cfg.Issuer,
		Audience:  []string{cfg.Audience},
	})
	if err != nil {
		return fmt.Errorf("creating token manager for tenant %s: %w", tenantID, err)
	}

	if err := manager.Start(ctx); err != nil {
		return fmt.Errorf("starting token manager for tenant %s: %w", tenantID, err)
	}

	// ===== STEP 5: Register entry =====
	r.tenants[tenantID] = &tenantEntry{
		manager:  manager,
		km:       km,
		draining: false,
	}

	// ===== STEP 6: Emit metrics =====
	r.metrics.SetGauge(observability.MetricActiveTenants, float64(len(r.tenants)), nil)
	r.metrics.IncrementCounter(observability.MetricRegistryOps, map[string]string{tenantRegistryOpLabel: tenantRegistryOpAdd})

	return nil
}

// Drain marks tenantID as draining so subsequent Get calls return codes.NotFound.
// In-flight requests already past the registry lookup are unaffected.
// Returns codes.InvalidArgument if tenantID is "".
// Returns codes.NotFound if tenantID is not registered.
func (r *MultiTenantRegistry) Drain(ctx context.Context, tenantID string) error {
	// ===== STEP 1: Validate =====
	if tenantID == "" {
		return status.Error(codes.InvalidArgument, "tenantID must not be empty")
	}

	// ===== STEP 2: Acquire write lock =====
	r.mu.Lock()
	defer r.mu.Unlock()

	// ===== STEP 3: Locate entry =====
	entry, exists := r.tenants[tenantID]
	if !exists {
		return status.Errorf(codes.NotFound, "tenant %s not found", tenantID)
	}

	// ===== STEP 4: Mark draining =====
	entry.draining = true

	// ===== STEP 5: Emit metrics =====
	r.metrics.IncrementCounter(observability.MetricRegistryOps, map[string]string{tenantRegistryOpLabel: tenantRegistryOpDrain})

	return nil
}

// Remove stops the tenant's KeyManager and removes it from the registry.
// Must be called only after Drain — returns codes.FailedPrecondition if the tenant is not draining.
// Returns codes.InvalidArgument if tenantID is "".
// Returns codes.NotFound if tenantID is not registered.
func (r *MultiTenantRegistry) Remove(ctx context.Context, tenantID string) error {
	// ===== STEP 1: Validate =====
	if tenantID == "" {
		return status.Error(codes.InvalidArgument, "tenantID must not be empty")
	}

	// ===== STEP 2: Acquire write lock =====
	r.mu.Lock()
	defer r.mu.Unlock()

	// ===== STEP 3: Locate entry =====
	entry, exists := r.tenants[tenantID]
	if !exists {
		return status.Errorf(codes.NotFound, "tenant %s not found", tenantID)
	}

	// ===== STEP 4: Enforce drain precondition =====
	if !entry.draining {
		return status.Errorf(codes.FailedPrecondition, "tenant %s must be drained before removal", tenantID)
	}

	// ===== STEP 5: Shutdown KeyManager =====
	if err := entry.km.Shutdown(ctx); err != nil {
		r.logger.Warn(ctx, "key manager shutdown error during tenant removal", "tenant", tenantID, "error", err)
	}

	// ===== STEP 6: Remove entry =====
	delete(r.tenants, tenantID)

	// ===== STEP 7: Emit metrics =====
	r.metrics.SetGauge(observability.MetricActiveTenants, float64(len(r.tenants)), nil)
	r.metrics.IncrementCounter(observability.MetricRegistryOps, map[string]string{tenantRegistryOpLabel: tenantRegistryOpRemove})

	return nil
}

// Get returns the TokenManager for tenantID.
// Returns codes.InvalidArgument if tenantID is "".
// Returns codes.NotFound if tenantID is unknown or draining.
func (r *MultiTenantRegistry) Get(_ context.Context, tenantID string) (tokens.TokenManager, error) {
	// ===== STEP 1: Validate =====
	if tenantID == "" {
		return nil, status.Error(codes.InvalidArgument, "tenant_id must not be empty")
	}

	// ===== STEP 2: Acquire read lock =====
	r.mu.RLock()
	defer r.mu.RUnlock()

	// ===== STEP 3: Locate entry =====
	entry, exists := r.tenants[tenantID]
	if !exists || entry.draining {
		return nil, status.Errorf(codes.NotFound, "tenant %s not found", tenantID)
	}

	return entry.manager, nil
}

// AllKeyManagers returns a snapshot of all registered tenants' KeyManagers.
// The returned map is a defensive copy — mutations do not affect the registry.
func (r *MultiTenantRegistry) AllKeyManagers() map[string]keys.KeyManager {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]keys.KeyManager, len(r.tenants))
	for id, entry := range r.tenants {
		result[id] = entry.km
	}
	return result
}

// GetAll returns a snapshot of all non-draining tenant managers at call time.
// Returns a map of tenantID → tokens.TokenManager.
// CursorReconciler receives this map at construction and does not observe
// tenants added or removed after construction.
func (r *MultiTenantRegistry) GetAll() map[string]tokens.TokenManager {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make(map[string]tokens.TokenManager)
	for id, entry := range r.tenants {
		if !entry.draining {
			result[id] = entry.manager
		}
	}
	return result
}
