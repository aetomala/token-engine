package config

import (
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Sentinel errors for Config validation failures.
var (
	ErrIssuerEmpty             = errors.New("TOKEN_ENGINE_ISSUER must not be empty")
	ErrAudienceEmpty           = errors.New("TOKEN_ENGINE_AUDIENCE must not be empty")
	ErrInvalidTLSMode          = errors.New("TOKEN_ENGINE_TLS_MODE must be 'mtls' or 'disabled'")
	ErrStaticCallerKeysFormat  = errors.New("TOKEN_ENGINE_STATIC_CALLER_KEYS format error: must be 'key1=identity1,key2=identity2'")
	ErrStaticCallerKeysEmpty   = errors.New("TOKEN_ENGINE_STATIC_CALLER_KEYS must not be empty when TLSMode is disabled")
	ErrTLSCertFileEmpty        = errors.New("TOKEN_ENGINE_TLS_CERT_FILE must not be empty when TLSMode is mtls")
	ErrTLSKeyFileEmpty         = errors.New("TOKEN_ENGINE_TLS_KEY_FILE must not be empty when TLSMode is mtls")
	ErrTLSCAFileEmpty          = errors.New("TOKEN_ENGINE_TLS_CA_FILE must not be empty when TLSMode is mtls")
	ErrCallerRegistryPathEmpty = errors.New("TOKEN_ENGINE_CALLER_REGISTRY_PATH must not be empty when TLSMode is mtls")
)

// Config holds service-global configuration loaded from environment variables.
// All validation happens during Load() — no network connections are opened.
// Required-field validation (Issuer, Audience, TLSMode, StaticCallerKeys) occurs before
// defaults are applied and returns a sentinel error on failure — callers decide whether to exit.
type Config struct {
	// GRPCAddr is the gRPC server listen address.
	// env: TOKEN_ENGINE_GRPC_ADDR; default: ":9090"
	// parse failure — log warning, use default
	GRPCAddr string

	// HTTPAddr is the HTTP server listen address (health, metrics, JWKS).
	// env: TOKEN_ENGINE_HTTP_ADDR; default: ":8080"
	// parse failure — log warning, use default
	HTTPAddr string

	// TLSMode controls transport security. Accepted values: "mtls", "disabled".
	// Empty string treated as "mtls".
	// env: TOKEN_ENGINE_TLS_MODE; default: "mtls"
	// unknown value — returns ErrInvalidTLSMode
	TLSMode string

	// Issuer is the JWT issuer claim stamped on all issued tokens.
	// env: TOKEN_ENGINE_ISSUER; no default
	// empty string — returns ErrIssuerEmpty
	Issuer string

	// Audience is the default JWT audience claim for all issued tokens.
	// env: TOKEN_ENGINE_AUDIENCE; no default
	// empty string — returns ErrAudienceEmpty
	Audience string

	// OTLPEndpoint is the OpenTelemetry collector endpoint for trace export.
	// env: OTEL_EXPORTER_OTLP_ENDPOINT; default: ""
	// empty — OTel SDK no-op TracerProvider used; no traces emitted
	OTLPEndpoint string

	// IdempotencyTTL is the TTL for idempotency store entries.
	// env: TOKEN_ENGINE_IDEMPOTENCY_TTL; default: 24 * time.Hour
	// parse failure — log warning, use default
	IdempotencyTTL time.Duration

	// MaxConnectionAge is the maximum age of a gRPC connection before graceful close.
	// env: TOKEN_ENGINE_MAX_CONNECTION_AGE; default: 30 * time.Minute
	// parse failure — log warning, use default
	MaxConnectionAge time.Duration

	// MaxConnectionAgeGrace is the grace period after MaxConnectionAge before forceful close.
	// env: TOKEN_ENGINE_MAX_CONNECTION_AGE_GRACE; default: 5 * time.Minute
	// parse failure — log warning, use default
	MaxConnectionAgeGrace time.Duration

	// StaticCallerKeys is the API key → caller identity map for StaticKeyAuthenticator.
	// Format: "key1=identity1,key2=identity2". Required when TLSMode == "disabled".
	// env: TOKEN_ENGINE_STATIC_CALLER_KEYS; no default
	// parse failure — returns ErrStaticCallerKeysFormat; empty map when TLSMode=="disabled" — returns ErrStaticCallerKeysEmpty
	StaticCallerKeys map[string]string

	// TLSCertFile is the path to the service's TLS certificate file (PEM).
	// env: TOKEN_ENGINE_TLS_CERT_FILE; no default
	// absent when TLSMode == "mtls" — returns ErrTLSCertFileEmpty
	// ignored when TLSMode == "disabled"
	TLSCertFile string

	// TLSKeyFile is the path to the service's TLS private key file (PEM).
	// env: TOKEN_ENGINE_TLS_KEY_FILE; no default
	// absent when TLSMode == "mtls" — returns ErrTLSKeyFileEmpty
	// ignored when TLSMode == "disabled"
	TLSKeyFile string

	// TLSCAFile is the path to the CA certificate file for client certificate verification (PEM).
	// env: TOKEN_ENGINE_TLS_CA_FILE; no default
	// absent when TLSMode == "mtls" — returns ErrTLSCAFileEmpty
	// ignored when TLSMode == "disabled"
	TLSCAFile string

	// CallerRegistryPath is the filesystem path to the caller-registry YAML file.
	// env: TOKEN_ENGINE_CALLER_REGISTRY_PATH; no default
	// absent when TLSMode == "mtls" — returns ErrCallerRegistryPathEmpty
	// optional when TLSMode == "disabled" — empty string is valid when disabled
	CallerRegistryPath string

	// RedisAddr is the Redis server address.
	// env: TOKEN_ENGINE_REDIS_ADDR; default: "localhost:6379"
	// parse failure — log warning, use default
	RedisAddr string

	// RedisPassword is the Redis AUTH password. Empty string means no authentication.
	// env: TOKEN_ENGINE_REDIS_PASSWORD; default: ""
	// empty allowed
	RedisPassword string

	// RedisDB is the Redis logical database index.
	// env: TOKEN_ENGINE_REDIS_DB; default: 0
	// parse failure — log warning, use default
	RedisDB int

	// JWKSCacheMaxAge is the Cache-Control max-age for the JWKS endpoint response.
	// env: TOKEN_ENGINE_JWKS_CACHE_MAX_AGE; default: 5 * time.Minute
	// parse failure — log warning, use default
	JWKSCacheMaxAge time.Duration

	// LockTTL is the TTL for all distributed lock keys.
	// env: TOKEN_ENGINE_LOCK_TTL; default: 30 * time.Second
	// parse failure — log warning, use default
	LockTTL time.Duration

	// ReconciliationInterval is the time between reconciliation passes.
	// env: TOKEN_ENGINE_RECONCILIATION_INTERVAL; default: 5 * time.Minute
	// parse failure — log warning, use default
	ReconciliationInterval time.Duration

	// RotationWindowGuard is the minimum time since last key generation before a new key is generated.
	// env: TOKEN_ENGINE_ROTATION_WINDOW_GUARD; default: 1 * time.Minute
	// parse failure — log warning, use default
	RotationWindowGuard time.Duration
}

// Load reads environment variables and validates them into a *Config.
// Required-field validation (Issuer, Audience, TLSMode, StaticCallerKeys) occurs before
// defaults are applied and returns a sentinel error on failure — see the Err* sentinels
// in this package. Duration and integer parse failures log a warning and use defaults.
// Callers are responsible for deciding whether to exit on a returned error.
func Load() (*Config, error) {
	c := &Config{}

	// ===== STEP 1: Read all env vars =====
	grpcAddrEnv := os.Getenv("TOKEN_ENGINE_GRPC_ADDR")
	httpAddrEnv := os.Getenv("TOKEN_ENGINE_HTTP_ADDR")
	tlsModeEnv := os.Getenv("TOKEN_ENGINE_TLS_MODE")
	issuerEnv := os.Getenv("TOKEN_ENGINE_ISSUER")
	audienceEnv := os.Getenv("TOKEN_ENGINE_AUDIENCE")
	otlpEndpointEnv := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	idempotencyTTLEnv := os.Getenv("TOKEN_ENGINE_IDEMPOTENCY_TTL")
	maxConnectionAgeEnv := os.Getenv("TOKEN_ENGINE_MAX_CONNECTION_AGE")
	maxConnectionAgeGraceEnv := os.Getenv("TOKEN_ENGINE_MAX_CONNECTION_AGE_GRACE")
	staticCallerKeysEnv := os.Getenv("TOKEN_ENGINE_STATIC_CALLER_KEYS")
	tlsCertFileEnv := os.Getenv("TOKEN_ENGINE_TLS_CERT_FILE")
	tlsKeyFileEnv := os.Getenv("TOKEN_ENGINE_TLS_KEY_FILE")
	tlsCAFileEnv := os.Getenv("TOKEN_ENGINE_TLS_CA_FILE")
	callerRegistryPathEnv := os.Getenv("TOKEN_ENGINE_CALLER_REGISTRY_PATH")
	redisAddrEnv := os.Getenv("TOKEN_ENGINE_REDIS_ADDR")
	redisPasswordEnv := os.Getenv("TOKEN_ENGINE_REDIS_PASSWORD")
	redisDBEnv := os.Getenv("TOKEN_ENGINE_REDIS_DB")
	jwksCacheMaxAgeEnv := os.Getenv("TOKEN_ENGINE_JWKS_CACHE_MAX_AGE")
	lockTTLEnv := os.Getenv("TOKEN_ENGINE_LOCK_TTL")
	reconciliationIntervalEnv := os.Getenv("TOKEN_ENGINE_RECONCILIATION_INTERVAL")
	rotationWindowGuardEnv := os.Getenv("TOKEN_ENGINE_ROTATION_WINDOW_GUARD")

	// ===== STEP 2: Fatal validations (before any defaults) =====

	// Validate Issuer
	if issuerEnv == "" {
		return nil, ErrIssuerEmpty
	}
	c.Issuer = issuerEnv

	// Validate Audience
	if audienceEnv == "" {
		return nil, ErrAudienceEmpty
	}
	c.Audience = audienceEnv

	// Validate and normalize TLSMode
	if tlsModeEnv == "" {
		tlsModeEnv = "mtls"
	}
	if tlsModeEnv != "mtls" && tlsModeEnv != "disabled" {
		return nil, ErrInvalidTLSMode
	}
	c.TLSMode = tlsModeEnv

	// Validate and parse StaticCallerKeys
	staticCallerKeys := make(map[string]string)
	if staticCallerKeysEnv != "" {
		pairs := strings.Split(staticCallerKeysEnv, ",")
		for _, pair := range pairs {
			// Split at the last '=' so a key containing '=' (e.g. base64 padding)
			// is preserved verbatim; the identity is read after the final delimiter.
			idx := strings.LastIndex(pair, "=")
			if idx <= 0 || idx == len(pair)-1 {
				return nil, fmt.Errorf("%w: pair %q", ErrStaticCallerKeysFormat, pair)
			}
			staticCallerKeys[pair[:idx]] = pair[idx+1:]
		}
	}
	if c.TLSMode == "disabled" && len(staticCallerKeys) == 0 {
		return nil, ErrStaticCallerKeysEmpty
	}
	c.StaticCallerKeys = staticCallerKeys

	// Validate TLS fields when TLSMode == "mtls"
	if c.TLSMode == "mtls" {
		if tlsCertFileEnv == "" {
			return nil, ErrTLSCertFileEmpty
		}
		if tlsKeyFileEnv == "" {
			return nil, ErrTLSKeyFileEmpty
		}
		if tlsCAFileEnv == "" {
			return nil, ErrTLSCAFileEmpty
		}
		if callerRegistryPathEnv == "" {
			return nil, ErrCallerRegistryPathEmpty
		}
	}

	// ===== STEP 3: Apply string defaults =====
	if grpcAddrEnv == "" {
		c.GRPCAddr = ":9090"
	} else {
		c.GRPCAddr = grpcAddrEnv
	}

	if httpAddrEnv == "" {
		c.HTTPAddr = ":8080"
	} else {
		c.HTTPAddr = httpAddrEnv
	}

	c.OTLPEndpoint = otlpEndpointEnv

	c.TLSCertFile = tlsCertFileEnv
	c.TLSKeyFile = tlsKeyFileEnv
	c.TLSCAFile = tlsCAFileEnv
	c.CallerRegistryPath = callerRegistryPathEnv

	if redisAddrEnv == "" {
		c.RedisAddr = "localhost:6379"
	} else {
		c.RedisAddr = redisAddrEnv
	}

	c.RedisPassword = redisPasswordEnv

	// ===== STEP 4: Parse duration fields (with warnings on failure) =====
	defaultIdempotencyTTL := 24 * time.Hour
	if idempotencyTTLEnv == "" {
		c.IdempotencyTTL = defaultIdempotencyTTL
	} else {
		duration, err := time.ParseDuration(idempotencyTTLEnv)
		if err != nil {
			log.Printf("TOKEN_ENGINE_IDEMPOTENCY_TTL parse error: %v; using default %v", err, defaultIdempotencyTTL)
			c.IdempotencyTTL = defaultIdempotencyTTL
		} else {
			c.IdempotencyTTL = duration
		}
	}

	defaultMaxConnectionAge := 30 * time.Minute
	if maxConnectionAgeEnv == "" {
		c.MaxConnectionAge = defaultMaxConnectionAge
	} else {
		duration, err := time.ParseDuration(maxConnectionAgeEnv)
		if err != nil {
			log.Printf("TOKEN_ENGINE_MAX_CONNECTION_AGE parse error: %v; using default %v", err, defaultMaxConnectionAge)
			c.MaxConnectionAge = defaultMaxConnectionAge
		} else {
			c.MaxConnectionAge = duration
		}
	}

	defaultMaxConnectionAgeGrace := 5 * time.Minute
	if maxConnectionAgeGraceEnv == "" {
		c.MaxConnectionAgeGrace = defaultMaxConnectionAgeGrace
	} else {
		duration, err := time.ParseDuration(maxConnectionAgeGraceEnv)
		if err != nil {
			log.Printf("TOKEN_ENGINE_MAX_CONNECTION_AGE_GRACE parse error: %v; using default %v", err, defaultMaxConnectionAgeGrace)
			c.MaxConnectionAgeGrace = defaultMaxConnectionAgeGrace
		} else {
			c.MaxConnectionAgeGrace = duration
		}
	}

	defaultJWKSCacheMaxAge := 5 * time.Minute
	if jwksCacheMaxAgeEnv == "" {
		c.JWKSCacheMaxAge = defaultJWKSCacheMaxAge
	} else {
		duration, err := time.ParseDuration(jwksCacheMaxAgeEnv)
		if err != nil {
			log.Printf("TOKEN_ENGINE_JWKS_CACHE_MAX_AGE parse error: %v; using default %v", err, defaultJWKSCacheMaxAge)
			c.JWKSCacheMaxAge = defaultJWKSCacheMaxAge
		} else {
			c.JWKSCacheMaxAge = duration
		}
	}

	defaultLockTTL := 30 * time.Second
	if lockTTLEnv == "" {
		c.LockTTL = defaultLockTTL
	} else {
		duration, err := time.ParseDuration(lockTTLEnv)
		if err != nil {
			log.Printf("TOKEN_ENGINE_LOCK_TTL parse error: %v; using default %v", err, defaultLockTTL)
			c.LockTTL = defaultLockTTL
		} else {
			c.LockTTL = duration
		}
	}

	defaultReconciliationInterval := 5 * time.Minute
	if reconciliationIntervalEnv == "" {
		c.ReconciliationInterval = defaultReconciliationInterval
	} else {
		duration, err := time.ParseDuration(reconciliationIntervalEnv)
		if err != nil {
			log.Printf("TOKEN_ENGINE_RECONCILIATION_INTERVAL parse error: %v; using default %v", err, defaultReconciliationInterval)
			c.ReconciliationInterval = defaultReconciliationInterval
		} else {
			c.ReconciliationInterval = duration
		}
	}

	defaultRotationWindowGuard := 1 * time.Minute
	if rotationWindowGuardEnv == "" {
		c.RotationWindowGuard = defaultRotationWindowGuard
	} else {
		duration, err := time.ParseDuration(rotationWindowGuardEnv)
		if err != nil {
			log.Printf("TOKEN_ENGINE_ROTATION_WINDOW_GUARD parse error: %v; using default %v", err, defaultRotationWindowGuard)
			c.RotationWindowGuard = defaultRotationWindowGuard
		} else {
			c.RotationWindowGuard = duration
		}
	}

	// ===== STEP 5: Parse integer fields (with warnings on failure) =====
	if redisDBEnv == "" {
		c.RedisDB = 0
	} else {
		db, err := strconv.Atoi(redisDBEnv)
		if err != nil {
			log.Printf("TOKEN_ENGINE_REDIS_DB parse error: %v; using default 0", err)
			c.RedisDB = 0
		} else {
			c.RedisDB = db
		}
	}

	// ===== STEP 6: Return config =====
	return c, nil
}
