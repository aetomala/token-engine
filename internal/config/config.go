package config

import (
	"log"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds service-global configuration loaded from environment variables.
// All validation happens during Load() — no network connections are opened.
// Fatal validation (Issuer, Audience, TLSMode, StaticCallerKeys) occurs before defaults are applied.
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
	// unknown value — log error and os.Exit(1)
	TLSMode string

	// Issuer is the JWT issuer claim stamped on all issued tokens.
	// env: TOKEN_ENGINE_ISSUER; no default
	// empty string — log error and os.Exit(1)
	Issuer string

	// Audience is the default JWT audience claim for all issued tokens.
	// env: TOKEN_ENGINE_AUDIENCE; no default
	// empty string — log error and os.Exit(1)
	Audience string

	// OTLPEndpoint is the OpenTelemetry collector endpoint for trace export.
	// env: OTEL_EXPORTER_OTLP_ENDPOINT; default: ""
	// empty — OTel SDK no-op TracerProvider used; no traces emitted
	OTLPEndpoint string

	// IdempotencyTTL is the TTL for idempotency store entries.
	// env: TOKEN_ENGINE_IDEMPOTENCY_TTL; default: 5 * time.Minute
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
	// parse failure or empty map when TLSMode=="disabled" — log error and os.Exit(1)
	StaticCallerKeys map[string]string

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
}

// Load reads environment variables and validates them into a *Config.
// Fatal validation (Issuer, Audience, TLSMode, StaticCallerKeys) occurs before defaults.
// Duration and integer parse failures log a warning and use defaults.
// Returns *Config on success or exits fatally on required-field violations.
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
	redisAddrEnv := os.Getenv("TOKEN_ENGINE_REDIS_ADDR")
	redisPasswordEnv := os.Getenv("TOKEN_ENGINE_REDIS_PASSWORD")
	redisDBEnv := os.Getenv("TOKEN_ENGINE_REDIS_DB")
	jwksCacheMaxAgeEnv := os.Getenv("TOKEN_ENGINE_JWKS_CACHE_MAX_AGE")

	// ===== STEP 2: Fatal validations (before any defaults) =====

	// Validate Issuer
	if issuerEnv == "" {
		log.Printf("TOKEN_ENGINE_ISSUER must not be empty")
		os.Exit(1)
	}
	c.Issuer = issuerEnv

	// Validate Audience
	if audienceEnv == "" {
		log.Printf("TOKEN_ENGINE_AUDIENCE must not be empty")
		os.Exit(1)
	}
	c.Audience = audienceEnv

	// Validate and normalize TLSMode
	if tlsModeEnv == "" {
		tlsModeEnv = "mtls"
	}
	if tlsModeEnv != "mtls" && tlsModeEnv != "disabled" {
		log.Printf("TOKEN_ENGINE_TLS_MODE must be 'mtls' or 'disabled'")
		os.Exit(1)
	}
	c.TLSMode = tlsModeEnv

	// Validate and parse StaticCallerKeys
	staticCallerKeys := make(map[string]string)
	if staticCallerKeysEnv != "" {
		pairs := strings.Split(staticCallerKeysEnv, ",")
		for _, pair := range pairs {
			parts := strings.Split(pair, "=")
			if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
				log.Printf("TOKEN_ENGINE_STATIC_CALLER_KEYS format error: must be 'key1=identity1,key2=identity2'")
				os.Exit(1)
			}
			staticCallerKeys[parts[0]] = parts[1]
		}
	}
	if c.TLSMode == "disabled" && len(staticCallerKeys) == 0 {
		log.Printf("TOKEN_ENGINE_STATIC_CALLER_KEYS must not be empty when TLSMode is disabled")
		os.Exit(1)
	}
	c.StaticCallerKeys = staticCallerKeys

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

	if redisAddrEnv == "" {
		c.RedisAddr = "localhost:6379"
	} else {
		c.RedisAddr = redisAddrEnv
	}

	c.RedisPassword = redisPasswordEnv

	// ===== STEP 4: Parse duration fields (with warnings on failure) =====
	defaultIdempotencyTTL := 5 * time.Minute
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
