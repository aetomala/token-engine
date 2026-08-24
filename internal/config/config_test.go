package config_test

import (
	"errors"
	"os"
	"time"

	"github.com/aetomala/token-engine/internal/config"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Config", func() {
	// Helper function to set required env vars for testing fatal paths
	setRequiredEnvVars := func() {
		os.Setenv("TOKEN_ENGINE_ISSUER", "required-issuer")
		os.Setenv("TOKEN_ENGINE_AUDIENCE", "required-audience")
		os.Setenv("TOKEN_ENGINE_TLS_MODE", "disabled")
		os.Setenv("TOKEN_ENGINE_STATIC_CALLER_KEYS", "key1=caller1")
	}

	// Helper function to clean up env vars after tests
	cleanupEnvVars := func() {
		os.Unsetenv("TOKEN_ENGINE_ISSUER")
		os.Unsetenv("TOKEN_ENGINE_AUDIENCE")
		os.Unsetenv("TOKEN_ENGINE_TLS_MODE")
		os.Unsetenv("TOKEN_ENGINE_STATIC_CALLER_KEYS")
		os.Unsetenv("TOKEN_ENGINE_GRPC_ADDR")
		os.Unsetenv("TOKEN_ENGINE_HTTP_ADDR")
		os.Unsetenv("OTEL_EXPORTER_OTLP_ENDPOINT")
		os.Unsetenv("TOKEN_ENGINE_IDEMPOTENCY_TTL")
		os.Unsetenv("TOKEN_ENGINE_MAX_CONNECTION_AGE")
		os.Unsetenv("TOKEN_ENGINE_MAX_CONNECTION_AGE_GRACE")
		os.Unsetenv("TOKEN_ENGINE_REDIS_ADDR")
		os.Unsetenv("TOKEN_ENGINE_REDIS_PASSWORD")
		os.Unsetenv("TOKEN_ENGINE_REDIS_DB")
		os.Unsetenv("TOKEN_ENGINE_JWKS_CACHE_MAX_AGE")
		os.Unsetenv("TOKEN_ENGINE_TLS_CERT_FILE")
		os.Unsetenv("TOKEN_ENGINE_TLS_KEY_FILE")
		os.Unsetenv("TOKEN_ENGINE_TLS_CA_FILE")
		os.Unsetenv("TOKEN_ENGINE_CALLER_REGISTRY_PATH")
		os.Unsetenv("TOKEN_ENGINE_LOCK_TTL")
		os.Unsetenv("TOKEN_ENGINE_RECONCILIATION_INTERVAL")
		os.Unsetenv("TOKEN_ENGINE_ROTATION_WINDOW_GUARD")
		os.Unsetenv("TOKEN_ENGINE_BACKFILL_EXPIRY_INDEX")
	}

	AfterEach(func() {
		cleanupEnvVars()
	})

	// ===== PHASE 1: Constructor and Initialization =====
	Describe("Phase 1: Constructor and Initialization", func() {
	Context("TOKEN_ENGINE_ISSUER empty", func() {
		It("returns ErrIssuerEmpty", func() {
			os.Setenv("TOKEN_ENGINE_ISSUER", "")
			os.Setenv("TOKEN_ENGINE_AUDIENCE", "required-audience")
			os.Setenv("TOKEN_ENGINE_TLS_MODE", "disabled")
			os.Setenv("TOKEN_ENGINE_STATIC_CALLER_KEYS", "key1=caller1")

			cfg, err := config.Load()

			Expect(errors.Is(err, config.ErrIssuerEmpty)).To(BeTrue())
			Expect(cfg).To(BeNil())

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_AUDIENCE empty", func() {
		It("returns ErrAudienceEmpty", func() {
			os.Setenv("TOKEN_ENGINE_ISSUER", "required-issuer")
			os.Setenv("TOKEN_ENGINE_AUDIENCE", "")
			os.Setenv("TOKEN_ENGINE_TLS_MODE", "disabled")
			os.Setenv("TOKEN_ENGINE_STATIC_CALLER_KEYS", "key1=caller1")

			cfg, err := config.Load()

			Expect(errors.Is(err, config.ErrAudienceEmpty)).To(BeTrue())
			Expect(cfg).To(BeNil())

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_TLS_MODE unknown value", func() {
		It("returns ErrInvalidTLSMode", func() {
			os.Setenv("TOKEN_ENGINE_ISSUER", "required-issuer")
			os.Setenv("TOKEN_ENGINE_AUDIENCE", "required-audience")
			os.Setenv("TOKEN_ENGINE_TLS_MODE", "unknown-mode")
			os.Setenv("TOKEN_ENGINE_STATIC_CALLER_KEYS", "key1=caller1")

			cfg, err := config.Load()

			Expect(errors.Is(err, config.ErrInvalidTLSMode)).To(BeTrue())
			Expect(cfg).To(BeNil())

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_STATIC_CALLER_KEYS malformed", func() {
		It("returns ErrStaticCallerKeysFormat", func() {
			os.Setenv("TOKEN_ENGINE_ISSUER", "required-issuer")
			os.Setenv("TOKEN_ENGINE_AUDIENCE", "required-audience")
			os.Setenv("TOKEN_ENGINE_TLS_MODE", "disabled")
			os.Setenv("TOKEN_ENGINE_STATIC_CALLER_KEYS", "malformed-key-no-equals-sign")

			cfg, err := config.Load()

			Expect(errors.Is(err, config.ErrStaticCallerKeysFormat)).To(BeTrue())
			Expect(cfg).To(BeNil())

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_STATIC_CALLER_KEYS empty when TLSMode is disabled", func() {
		It("returns ErrStaticCallerKeysEmpty", func() {
			os.Setenv("TOKEN_ENGINE_ISSUER", "required-issuer")
			os.Setenv("TOKEN_ENGINE_AUDIENCE", "required-audience")
			os.Setenv("TOKEN_ENGINE_TLS_MODE", "disabled")
			// TOKEN_ENGINE_STATIC_CALLER_KEYS intentionally not set

			cfg, err := config.Load()

			Expect(errors.Is(err, config.ErrStaticCallerKeysEmpty)).To(BeTrue())
			Expect(cfg).To(BeNil())

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_IDEMPOTENCY_TTL parse failure", func() {
		It("logs warning and uses default 24h", func() {
			setRequiredEnvVars()
			os.Setenv("TOKEN_ENGINE_IDEMPOTENCY_TTL", "not-a-duration")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.IdempotencyTTL).To(Equal(24 * time.Hour))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_IDEMPOTENCY_TTL set to valid duration", func() {
		It("parses and stores the duration", func() {
			setRequiredEnvVars()
			os.Setenv("TOKEN_ENGINE_IDEMPOTENCY_TTL", "30m")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.IdempotencyTTL).To(Equal(30 * time.Minute))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_IDEMPOTENCY_TTL absent", func() {
		It("uses default of 24h without logging a warning", func() {
			setRequiredEnvVars()
			// TOKEN_ENGINE_IDEMPOTENCY_TTL not set — cleanupEnvVars already unsets it

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.IdempotencyTTL).To(Equal(24 * time.Hour))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_JWKS_CACHE_MAX_AGE set to valid duration", func() {
		It("parses and stores the duration", func() {
			setRequiredEnvVars()
			os.Setenv("TOKEN_ENGINE_JWKS_CACHE_MAX_AGE", "10m")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.JWKSCacheMaxAge).To(Equal(10 * time.Minute))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_JWKS_CACHE_MAX_AGE absent", func() {
		It("uses default of 5m", func() {
			setRequiredEnvVars()

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.JWKSCacheMaxAge).To(Equal(5 * time.Minute))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_JWKS_CACHE_MAX_AGE set to unparseable value", func() {
		It("logs warning and uses default of 5m", func() {
			setRequiredEnvVars()
			os.Setenv("TOKEN_ENGINE_JWKS_CACHE_MAX_AGE", "notaduration")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.JWKSCacheMaxAge).To(Equal(5 * time.Minute))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_STATIC_CALLER_KEYS value contains '=' padding", func() {
		It("preserves the key verbatim and parses the correct identity", func() {
			os.Setenv("TOKEN_ENGINE_ISSUER", "required-issuer")
			os.Setenv("TOKEN_ENGINE_AUDIENCE", "required-audience")
			os.Setenv("TOKEN_ENGINE_TLS_MODE", "disabled")
			os.Setenv("TOKEN_ENGINE_STATIC_CALLER_KEYS", "c2VjcmV0Zm9v===caller-a")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.StaticCallerKeys).To(Equal(map[string]string{
				"c2VjcmV0Zm9v==": "caller-a",
			}))

			cleanupEnvVars()
		})
	})

	Context("all required fields set", func() {
		It("returns a fully populated Config without error", func() {
			setRequiredEnvVars()
			os.Setenv("TOKEN_ENGINE_GRPC_ADDR", ":9999")
			os.Setenv("TOKEN_ENGINE_HTTP_ADDR", ":8888")
			os.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "http://localhost:4317")
			os.Setenv("TOKEN_ENGINE_IDEMPOTENCY_TTL", "10m")
			os.Setenv("TOKEN_ENGINE_MAX_CONNECTION_AGE", "1h")
			os.Setenv("TOKEN_ENGINE_MAX_CONNECTION_AGE_GRACE", "10s")
			os.Setenv("TOKEN_ENGINE_REDIS_ADDR", "redis.example.com:6379")
			os.Setenv("TOKEN_ENGINE_REDIS_PASSWORD", "secret")
			os.Setenv("TOKEN_ENGINE_REDIS_DB", "2")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg).NotTo(BeNil())

			Expect(cfg.Issuer).To(Equal("required-issuer"))
			Expect(cfg.Audience).To(Equal("required-audience"))
			Expect(cfg.TLSMode).To(Equal("disabled"))
			Expect(cfg.GRPCAddr).To(Equal(":9999"))
			Expect(cfg.HTTPAddr).To(Equal(":8888"))
			Expect(cfg.OTLPEndpoint).To(Equal("http://localhost:4317"))
			Expect(cfg.IdempotencyTTL).To(Equal(10 * time.Minute))
			Expect(cfg.MaxConnectionAge).To(Equal(1 * time.Hour))
			Expect(cfg.MaxConnectionAgeGrace).To(Equal(10 * time.Second))
			Expect(cfg.RedisAddr).To(Equal("redis.example.com:6379"))
			Expect(cfg.RedisPassword).To(Equal("secret"))
			Expect(cfg.RedisDB).To(Equal(2))
			Expect(cfg.StaticCallerKeys).To(Equal(map[string]string{
				"key1": "caller1",
			}))
			Expect(cfg.StaticCallerKeys).NotTo(BeEmpty())

			cleanupEnvVars()
		})
	})
	Context("when TLSMode is 'mtls' and all TLS fields are present", func() {
		It("stores TLSCertFile, TLSKeyFile, TLSCAFile, CallerRegistryPath", func() {
			os.Setenv("TOKEN_ENGINE_ISSUER", "required-issuer")
			os.Setenv("TOKEN_ENGINE_AUDIENCE", "required-audience")
			os.Setenv("TOKEN_ENGINE_TLS_MODE", "mtls")
			os.Setenv("TOKEN_ENGINE_TLS_CERT_FILE", "/path/to/cert.pem")
			os.Setenv("TOKEN_ENGINE_TLS_KEY_FILE", "/path/to/key.pem")
			os.Setenv("TOKEN_ENGINE_TLS_CA_FILE", "/path/to/ca.pem")
			os.Setenv("TOKEN_ENGINE_CALLER_REGISTRY_PATH", "/path/to/caller-registry.yaml")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.TLSCertFile).To(Equal("/path/to/cert.pem"))
			Expect(cfg.TLSKeyFile).To(Equal("/path/to/key.pem"))
			Expect(cfg.TLSCAFile).To(Equal("/path/to/ca.pem"))
			Expect(cfg.CallerRegistryPath).To(Equal("/path/to/caller-registry.yaml"))

			cleanupEnvVars()
		})
	})

	Context("when TLSMode is 'mtls' and TOKEN_ENGINE_TLS_CERT_FILE is absent", func() {
		It("returns ErrTLSCertFileEmpty", func() {
			os.Setenv("TOKEN_ENGINE_ISSUER", "required-issuer")
			os.Setenv("TOKEN_ENGINE_AUDIENCE", "required-audience")
			os.Setenv("TOKEN_ENGINE_TLS_MODE", "mtls")
			os.Setenv("TOKEN_ENGINE_TLS_KEY_FILE", "/path/to/key.pem")
			os.Setenv("TOKEN_ENGINE_TLS_CA_FILE", "/path/to/ca.pem")
			os.Setenv("TOKEN_ENGINE_CALLER_REGISTRY_PATH", "/path/to/caller-registry.yaml")

			cfg, err := config.Load()

			Expect(errors.Is(err, config.ErrTLSCertFileEmpty)).To(BeTrue())
			Expect(cfg).To(BeNil())

			cleanupEnvVars()
		})
	})

	Context("when TLSMode is 'mtls' and TOKEN_ENGINE_TLS_KEY_FILE is absent", func() {
		It("returns ErrTLSKeyFileEmpty", func() {
			os.Setenv("TOKEN_ENGINE_ISSUER", "required-issuer")
			os.Setenv("TOKEN_ENGINE_AUDIENCE", "required-audience")
			os.Setenv("TOKEN_ENGINE_TLS_MODE", "mtls")
			os.Setenv("TOKEN_ENGINE_TLS_CERT_FILE", "/path/to/cert.pem")
			os.Setenv("TOKEN_ENGINE_TLS_CA_FILE", "/path/to/ca.pem")
			os.Setenv("TOKEN_ENGINE_CALLER_REGISTRY_PATH", "/path/to/caller-registry.yaml")

			cfg, err := config.Load()

			Expect(errors.Is(err, config.ErrTLSKeyFileEmpty)).To(BeTrue())
			Expect(cfg).To(BeNil())

			cleanupEnvVars()
		})
	})

	Context("when TLSMode is 'mtls' and TOKEN_ENGINE_TLS_CA_FILE is absent", func() {
		It("returns ErrTLSCAFileEmpty", func() {
			os.Setenv("TOKEN_ENGINE_ISSUER", "required-issuer")
			os.Setenv("TOKEN_ENGINE_AUDIENCE", "required-audience")
			os.Setenv("TOKEN_ENGINE_TLS_MODE", "mtls")
			os.Setenv("TOKEN_ENGINE_TLS_CERT_FILE", "/path/to/cert.pem")
			os.Setenv("TOKEN_ENGINE_TLS_KEY_FILE", "/path/to/key.pem")
			os.Setenv("TOKEN_ENGINE_CALLER_REGISTRY_PATH", "/path/to/caller-registry.yaml")

			cfg, err := config.Load()

			Expect(errors.Is(err, config.ErrTLSCAFileEmpty)).To(BeTrue())
			Expect(cfg).To(BeNil())

			cleanupEnvVars()
		})
	})

	Context("when TLSMode is 'mtls' and TOKEN_ENGINE_CALLER_REGISTRY_PATH is absent", func() {
		It("returns ErrCallerRegistryPathEmpty", func() {
			os.Setenv("TOKEN_ENGINE_ISSUER", "required-issuer")
			os.Setenv("TOKEN_ENGINE_AUDIENCE", "required-audience")
			os.Setenv("TOKEN_ENGINE_TLS_MODE", "mtls")
			os.Setenv("TOKEN_ENGINE_TLS_CERT_FILE", "/path/to/cert.pem")
			os.Setenv("TOKEN_ENGINE_TLS_KEY_FILE", "/path/to/key.pem")
			os.Setenv("TOKEN_ENGINE_TLS_CA_FILE", "/path/to/ca.pem")

			cfg, err := config.Load()

			Expect(errors.Is(err, config.ErrCallerRegistryPathEmpty)).To(BeTrue())
			Expect(cfg).To(BeNil())

			cleanupEnvVars()
		})
	})

	Context("when TLSMode is 'disabled' and TOKEN_ENGINE_CALLER_REGISTRY_PATH is absent", func() {
		It("stores empty CallerRegistryPath without error", func() {
			setRequiredEnvVars()
			// TOKEN_ENGINE_CALLER_REGISTRY_PATH not set

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.CallerRegistryPath).To(Equal(""))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_LOCK_TTL set to valid duration", func() {
		It("parses and stores the duration", func() {
			setRequiredEnvVars()
			os.Setenv("TOKEN_ENGINE_LOCK_TTL", "1m")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.LockTTL).To(Equal(1 * time.Minute))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_LOCK_TTL absent", func() {
		It("uses default of 30s", func() {
			setRequiredEnvVars()

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.LockTTL).To(Equal(30 * time.Second))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_LOCK_TTL parse failure", func() {
		It("logs warning and uses default 30s", func() {
			setRequiredEnvVars()
			os.Setenv("TOKEN_ENGINE_LOCK_TTL", "not-a-duration")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.LockTTL).To(Equal(30 * time.Second))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_RECONCILIATION_INTERVAL set to valid duration", func() {
		It("parses and stores the duration", func() {
			setRequiredEnvVars()
			os.Setenv("TOKEN_ENGINE_RECONCILIATION_INTERVAL", "10m")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.ReconciliationInterval).To(Equal(10 * time.Minute))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_RECONCILIATION_INTERVAL absent", func() {
		It("uses default of 5m", func() {
			setRequiredEnvVars()

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.ReconciliationInterval).To(Equal(5 * time.Minute))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_RECONCILIATION_INTERVAL parse failure", func() {
		It("logs warning and uses default 5m", func() {
			setRequiredEnvVars()
			os.Setenv("TOKEN_ENGINE_RECONCILIATION_INTERVAL", "bad")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.ReconciliationInterval).To(Equal(5 * time.Minute))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_ROTATION_WINDOW_GUARD set to valid duration", func() {
		It("parses and stores the duration", func() {
			setRequiredEnvVars()
			os.Setenv("TOKEN_ENGINE_ROTATION_WINDOW_GUARD", "2m")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.RotationWindowGuard).To(Equal(2 * time.Minute))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_ROTATION_WINDOW_GUARD absent", func() {
		It("uses default of 1m", func() {
			setRequiredEnvVars()

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.RotationWindowGuard).To(Equal(1 * time.Minute))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_ROTATION_WINDOW_GUARD parse failure", func() {
		It("logs warning and uses default 1m", func() {
			setRequiredEnvVars()
			os.Setenv("TOKEN_ENGINE_ROTATION_WINDOW_GUARD", "bad")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.RotationWindowGuard).To(Equal(1 * time.Minute))

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_BACKFILL_EXPIRY_INDEX absent", func() {
		It("defaults to false", func() {
			setRequiredEnvVars()

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.BackfillExpiryIndex).To(BeFalse())

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_BACKFILL_EXPIRY_INDEX set to true", func() {
		It("parses and stores true", func() {
			setRequiredEnvVars()
			os.Setenv("TOKEN_ENGINE_BACKFILL_EXPIRY_INDEX", "true")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.BackfillExpiryIndex).To(BeTrue())

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_BACKFILL_EXPIRY_INDEX set to false", func() {
		It("parses and stores false", func() {
			setRequiredEnvVars()
			os.Setenv("TOKEN_ENGINE_BACKFILL_EXPIRY_INDEX", "false")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.BackfillExpiryIndex).To(BeFalse())

			cleanupEnvVars()
		})
	})

	Context("TOKEN_ENGINE_BACKFILL_EXPIRY_INDEX parse failure", func() {
		It("logs warning and uses default false", func() {
			setRequiredEnvVars()
			os.Setenv("TOKEN_ENGINE_BACKFILL_EXPIRY_INDEX", "not-a-bool")

			cfg, err := config.Load()

			Expect(err).NotTo(HaveOccurred())
			Expect(cfg.BackfillExpiryIndex).To(BeFalse())

			cleanupEnvVars()
		})
	})
	}) // Phase 1
})
