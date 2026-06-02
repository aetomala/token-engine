package registry

import (
	"context"
	"os"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"gopkg.in/yaml.v3"

	"github.com/aetomala/token-engine/internal/observability"
)

// CallerRegistry provides caller authorization checks for tenant operations.
type CallerRegistry interface {
	// IsPermitted returns true if the given callerIdentity is authorized to operate on tenantID.
	// Returns (false, codes.PermissionDenied) if the caller is not authorized.
	IsPermitted(ctx context.Context, callerIdentity string, tenantID string) (bool, error)
}

// CallerRegistryConfig is the decoded contents of the caller-registry YAML file.
type CallerRegistryConfig struct {
	Version int           `yaml:"version"`
	Callers []CallerEntry `yaml:"callers"`
}

// CallerEntry is a single caller in the registry config.
type CallerEntry struct {
	Identity         string   `yaml:"identity"`
	PermittedTenants []string `yaml:"permitted_tenants"`
}

// LoadCallerRegistryConfig opens and decodes a caller-registry YAML file.
// Returns error if the file cannot be opened or YAML is malformed.
// Does NOT validate the decoded config — callers must validate separately.
func LoadCallerRegistryConfig(path string) (*CallerRegistryConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg CallerRegistryConfig
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

// StaticCallerRegistry is an in-memory implementation of CallerRegistry backed by a
// YAML-decoded CallerRegistryConfig. It checks each request against a pre-built
// allowed map of identity → set of permitted tenants.
// All methods are safe for concurrent use — the allowed map is read-only after construction.
type StaticCallerRegistry struct {
	allowed map[string]map[string]bool // identity → set of permitted tenants
	logger  observability.Logger
}

var _ CallerRegistry = (*StaticCallerRegistry)(nil)

// NewStaticCallerRegistry constructs a StaticCallerRegistry from an already-loaded config.
// Cfg must not be nil. Cfg must have been validated before this call.
// Logger is used for authorization decision logging.
func NewStaticCallerRegistry(cfg *CallerRegistryConfig, logger observability.Logger) *StaticCallerRegistry {
	allowed := make(map[string]map[string]bool, len(cfg.Callers))
	for _, entry := range cfg.Callers {
		tenantSet := make(map[string]bool, len(entry.PermittedTenants))
		for _, t := range entry.PermittedTenants {
			tenantSet[t] = true
		}
		allowed[entry.Identity] = tenantSet
	}
	return &StaticCallerRegistry{
		allowed: allowed,
		logger:  logger,
	}
}

// IsPermitted returns true if callerIdentity is authorized to operate on tenantID.
// If tenantID is empty, returns (true, nil) — validation interceptor handles the required check.
// Returns (false, codes.PermissionDenied) if callerIdentity is not in the registry or tenantID
// is not in the caller's permitted_tenants list.
func (r *StaticCallerRegistry) IsPermitted(_ context.Context, callerIdentity string, tenantID string) (bool, error) {
	if tenantID == "" {
		return true, nil
	}

	tenantSet, found := r.allowed[callerIdentity]
	if !found {
		return false, status.Error(codes.PermissionDenied, "caller not authorized")
	}
	if !tenantSet[tenantID] {
		return false, status.Error(codes.PermissionDenied, "caller not authorized")
	}
	return true, nil
}
