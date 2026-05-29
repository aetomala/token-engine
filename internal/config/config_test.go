package config_test

import (
	"errors"
	"os"
	"os/exec"
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
	}

	AfterEach(func() {
		cleanupEnvVars()
	})

	// ===== PHASE 1: Constructor and Initialization =====
	Describe("Phase 1: Constructor and Initialization", func() {
	Context("TOKEN_ENGINE_ISSUER empty", func() {
		It("calls os.Exit(1)", func() {
			if os.Getenv("TEST_SUBPROCESS") == "issuer_empty" {
				os.Setenv("TOKEN_ENGINE_ISSUER", "")
				os.Setenv("TOKEN_ENGINE_AUDIENCE", "required-audience")
				os.Setenv("TOKEN_ENGINE_TLS_MODE", "disabled")
				os.Setenv("TOKEN_ENGINE_STATIC_CALLER_KEYS", "key1=caller1")
				config.Load()
				return
			}

			cmd := exec.Command(os.Args[0], "-test.run=TestConfig")
			cmd.Env = append(os.Environ(), "TEST_SUBPROCESS=issuer_empty")
			err := cmd.Run()

			var exitErr *exec.ExitError
			Expect(errors.As(err, &exitErr)).To(BeTrue())
			Expect(exitErr.ExitCode()).To(Equal(1))
		})
	})

	Context("TOKEN_ENGINE_AUDIENCE empty", func() {
		It("calls os.Exit(1)", func() {
			if os.Getenv("TEST_SUBPROCESS") == "audience_empty" {
				os.Setenv("TOKEN_ENGINE_ISSUER", "required-issuer")
				os.Setenv("TOKEN_ENGINE_AUDIENCE", "")
				os.Setenv("TOKEN_ENGINE_TLS_MODE", "disabled")
				os.Setenv("TOKEN_ENGINE_STATIC_CALLER_KEYS", "key1=caller1")
				config.Load()
				return
			}

			cmd := exec.Command(os.Args[0], "-test.run=TestConfig")
			cmd.Env = append(os.Environ(), "TEST_SUBPROCESS=audience_empty")
			err := cmd.Run()

			var exitErr *exec.ExitError
			Expect(errors.As(err, &exitErr)).To(BeTrue())
			Expect(exitErr.ExitCode()).To(Equal(1))
		})
	})

	Context("TOKEN_ENGINE_TLS_MODE unknown value", func() {
		It("calls os.Exit(1)", func() {
			if os.Getenv("TEST_SUBPROCESS") == "tls_unknown" {
				os.Setenv("TOKEN_ENGINE_ISSUER", "required-issuer")
				os.Setenv("TOKEN_ENGINE_AUDIENCE", "required-audience")
				os.Setenv("TOKEN_ENGINE_TLS_MODE", "unknown-mode")
				os.Setenv("TOKEN_ENGINE_STATIC_CALLER_KEYS", "key1=caller1")
				config.Load()
				return
			}

			cmd := exec.Command(os.Args[0], "-test.run=TestConfig")
			cmd.Env = append(os.Environ(), "TEST_SUBPROCESS=tls_unknown")
			err := cmd.Run()

			var exitErr *exec.ExitError
			Expect(errors.As(err, &exitErr)).To(BeTrue())
			Expect(exitErr.ExitCode()).To(Equal(1))
		})
	})

	Context("TOKEN_ENGINE_STATIC_CALLER_KEYS malformed", func() {
		It("calls os.Exit(1)", func() {
			if os.Getenv("TEST_SUBPROCESS") == "keys_malformed" {
				os.Setenv("TOKEN_ENGINE_ISSUER", "required-issuer")
				os.Setenv("TOKEN_ENGINE_AUDIENCE", "required-audience")
				os.Setenv("TOKEN_ENGINE_TLS_MODE", "disabled")
				os.Setenv("TOKEN_ENGINE_STATIC_CALLER_KEYS", "malformed-key-no-equals-sign")
				config.Load()
				return
			}

			cmd := exec.Command(os.Args[0], "-test.run=TestConfig")
			cmd.Env = append(os.Environ(), "TEST_SUBPROCESS=keys_malformed")
			err := cmd.Run()

			var exitErr *exec.ExitError
			Expect(errors.As(err, &exitErr)).To(BeTrue())
			Expect(exitErr.ExitCode()).To(Equal(1))
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
	}) // Phase 1
})
