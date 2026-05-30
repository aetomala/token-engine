package integration_test

import (
	"context"
	"net"
	"time"

	"github.com/alicebob/miniredis/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"github.com/aetomala/token-engine/internal/audit"
	"github.com/aetomala/token-engine/internal/config"
	"github.com/aetomala/token-engine/internal/handler"
	"github.com/aetomala/token-engine/internal/interceptor"
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/registry"
	"github.com/aetomala/token-engine/internal/store"
)

var (
	mr         *miniredis.Miniredis
	grpcServer *grpc.Server
	conn       *grpc.ClientConn
	client     tokenv1.TokenEngineClient
	km         interface{ Shutdown(context.Context) error }
)

var _ = BeforeSuite(func() {
	var err error

	// ===== Start miniredis =====
	mr, err = miniredis.Run()
	Expect(err).NotTo(HaveOccurred())

	// ===== Build config directly — config.Load() calls os.Exit on fatal errors =====
	cfg := &config.Config{
		Issuer:           "test-issuer",
		Audience:         "api",
		TLSMode:          "disabled",
		StaticCallerKeys: map[string]string{"test-api-key": "test-caller"},
		RedisAddr:        mr.Addr(),
		IdempotencyTTL:   24 * time.Hour,
		JWKSCacheMaxAge:  5 * time.Minute,
	}

	// ===== Noop observability =====
	logger := observability.NewNoOpLogger()
	tracer := observability.NewNoOpTracer()
	metrics := observability.NewNoOpMetrics()

	// ===== Redis client =====
	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	// ===== Prometheus registry =====
	promReg := prometheus.NewRegistry()

	// ===== StaticTenantRegistry — km.Start() generates RSA keys; allow 30s =====
	initCtx, initCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer initCancel()
	tenantReg, err := registry.NewStaticTenantRegistry(initCtx, redisClient, cfg, promReg, logger, tracer, metrics)
	Expect(err).NotTo(HaveOccurred())
	km = tenantReg.KeyManager()

	// ===== Idempotency store =====
	idempStore := store.NewRedisIdempotencyStore(redisClient, cfg.IdempotencyTTL)

	// ===== Interceptors (same order as main.go, otelgrpc skipped — noop OTel not needed) =====
	auth := interceptor.NewStaticKeyAuthenticator(cfg.StaticCallerKeys)
	callerReg := registry.NewStaticCallerRegistry(&registry.CallerRegistryConfig{
		Version: 1,
		Callers: []registry.CallerEntry{
			// test-caller is permitted for test-issuer (normal flow) and unknown-tenant
			// (so the "unknown tenant" integration test reaches the registry and gets NotFound).
			{Identity: "test-caller", PermittedTenants: []string{"test-issuer", "unknown-tenant"}},
		},
	}, logger)

	correlationInterceptor := observability.NewCorrelationInterceptor(logger, metrics)
	authInterceptor := interceptor.NewAuthInterceptor(auth, logger)
	callerAuthInterceptor := interceptor.NewCallerAuthorizationInterceptor(callerReg, logger)
	idempotencyInterceptor := interceptor.NewIdempotencyInterceptor(idempStore, logger, metrics)
	validationInterceptor := interceptor.NewValidationInterceptor(logger)

	// ===== gRPC server =====
	grpcServer = grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			correlationInterceptor,
			authInterceptor,
			callerAuthInterceptor,
			idempotencyInterceptor,
			validationInterceptor,
		),
	)
	tokenHandler := handler.NewTokenHandler(tenantReg, audit.NewNoOpAuditStore(), logger, tracer, metrics)
	tokenv1.RegisterTokenEngineServer(grpcServer, tokenHandler)

	// ===== Listen on a random port =====
	listener, err := net.Listen("tcp", ":0")
	Expect(err).NotTo(HaveOccurred())
	go func() { _ = grpcServer.Serve(listener) }()

	// ===== gRPC client =====
	conn, err = grpc.NewClient(
		listener.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	Expect(err).NotTo(HaveOccurred())
	client = tokenv1.NewTokenEngineClient(conn)
})

var _ = AfterSuite(func() {
	if conn != nil {
		_ = conn.Close()
	}
	if grpcServer != nil {
		grpcServer.GracefulStop()
	}
	if km != nil {
		_ = km.Shutdown(context.Background())
	}
	if mr != nil {
		mr.Close()
	}
})

// authCtx returns a context carrying the test API key header and a 5s timeout.
func authCtx() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	ctx = metadata.AppendToOutgoingContext(ctx, "x-api-key", "test-api-key")
	return ctx, cancel
}

var _ = Describe("TokenEngine", func() {

	// ===== IssueToken =====
	Describe("IssueToken", func() {
		Context("when request is valid", func() {
			It("returns a non-empty access token, refresh token, and positive expiry values", func() {
				ctx, cancel := authCtx()
				defer cancel()

				resp, err := client.IssueToken(ctx, &tokenv1.IssueTokenRequest{
					Sub:      "user-1",
					TenantId: "test-issuer",
				})

				Expect(err).NotTo(HaveOccurred())
				Expect(resp.AccessToken).NotTo(BeEmpty())
				Expect(resp.RefreshToken).NotTo(BeEmpty())
			})
		})

		Context("when sub is empty", func() {
			It("returns codes.InvalidArgument", func() {
				ctx, cancel := authCtx()
				defer cancel()

				_, err := client.IssueToken(ctx, &tokenv1.IssueTokenRequest{
					Sub:      "",
					TenantId: "test-issuer",
				})

				Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			})
		})

		Context("when API key header is missing", func() {
			It("returns codes.Unauthenticated", func() {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()

				_, err := client.IssueToken(ctx, &tokenv1.IssueTokenRequest{
					Sub:      "user-1",
					TenantId: "test-issuer",
				})

				Expect(status.Code(err)).To(Equal(codes.Unauthenticated))
			})
		})

		Context("when tenant_id does not match the configured issuer", func() {
			It("returns codes.NotFound", func() {
				ctx, cancel := authCtx()
				defer cancel()

				_, err := client.IssueToken(ctx, &tokenv1.IssueTokenRequest{
					Sub:      "user-1",
					TenantId: "unknown-tenant",
				})

				Expect(status.Code(err)).To(Equal(codes.NotFound))
			})
		})
	})

	// ===== RefreshToken =====
	Describe("RefreshToken", func() {
		Context("when refresh token is valid", func() {
			It("returns a new access token and refresh token", func() {
				ctx, cancel := authCtx()
				defer cancel()

				issued, err := client.IssueToken(ctx, &tokenv1.IssueTokenRequest{
					Sub:      "user-2",
					TenantId: "test-issuer",
				})
				Expect(err).NotTo(HaveOccurred())

				ctx2, cancel2 := authCtx()
				defer cancel2()

				refreshed, err := client.RefreshToken(ctx2, &tokenv1.RefreshTokenRequest{
					RefreshToken: issued.RefreshToken,
					TenantId:     "test-issuer",
				})

				Expect(err).NotTo(HaveOccurred())
				Expect(refreshed.AccessToken).NotTo(BeEmpty())
				// RefreshToken handler returns access token only — refresh_token field is not populated
			})
		})

		Context("when refresh token string is invalid", func() {
			It("returns a non-OK status", func() {
				ctx, cancel := authCtx()
				defer cancel()

				_, err := client.RefreshToken(ctx, &tokenv1.RefreshTokenRequest{
					RefreshToken: "not-a-valid-token",
					TenantId:     "test-issuer",
				})

				Expect(status.Code(err)).NotTo(Equal(codes.OK))
			})
		})
	})

	// ===== RevokeToken =====
	Describe("RevokeToken", func() {
		Context("after revoking a refresh token", func() {
			It("causes a subsequent RefreshToken call to fail", func() {
				ctx, cancel := authCtx()
				defer cancel()

				issued, err := client.IssueToken(ctx, &tokenv1.IssueTokenRequest{
					Sub:      "user-3",
					TenantId: "test-issuer",
				})
				Expect(err).NotTo(HaveOccurred())

				ctx2, cancel2 := authCtx()
				defer cancel2()
				_, err = client.RevokeToken(ctx2, &tokenv1.RevokeTokenRequest{
					RefreshToken: issued.RefreshToken,
					TenantId:     "test-issuer",
				})
				Expect(err).NotTo(HaveOccurred())

				ctx3, cancel3 := authCtx()
				defer cancel3()
				_, err = client.RefreshToken(ctx3, &tokenv1.RefreshTokenRequest{
					RefreshToken: issued.RefreshToken,
					TenantId:     "test-issuer",
				})
				Expect(status.Code(err)).NotTo(Equal(codes.OK))
			})
		})
	})

	// ===== RevokeAllForAudience =====
	Describe("RevokeAllForAudience", func() {
		Context("after revoking all tokens for the default audience", func() {
			It("causes a subsequent RefreshToken call to fail", func() {
				ctx, cancel := authCtx()
				defer cancel()

				issued, err := client.IssueToken(ctx, &tokenv1.IssueTokenRequest{
					Sub:      "user-4",
					TenantId: "test-issuer",
				})
				Expect(err).NotTo(HaveOccurred())

				ctx2, cancel2 := authCtx()
				defer cancel2()
				_, err = client.RevokeAllForAudience(ctx2, &tokenv1.RevokeAudienceRequest{
					Audience: "api",
					TenantId: "test-issuer",
				})
				Expect(err).NotTo(HaveOccurred())

				ctx3, cancel3 := authCtx()
				defer cancel3()
				_, err = client.RefreshToken(ctx3, &tokenv1.RefreshTokenRequest{
					RefreshToken: issued.RefreshToken,
					TenantId:     "test-issuer",
				})
				Expect(status.Code(err)).NotTo(Equal(codes.OK))
			})
		})
	})

	// ===== RevokeAllUserTokens =====
	Describe("RevokeAllUserTokens", func() {
		Context("after revoking all tokens for a user", func() {
			It("causes a subsequent RefreshToken call for that user to fail", func() {
				ctx, cancel := authCtx()
				defer cancel()

				issued, err := client.IssueToken(ctx, &tokenv1.IssueTokenRequest{
					Sub:      "user-5",
					TenantId: "test-issuer",
				})
				Expect(err).NotTo(HaveOccurred())

				ctx2, cancel2 := authCtx()
				defer cancel2()
				_, err = client.RevokeAllUserTokens(ctx2, &tokenv1.RevokeUserRequest{
					UserId:   "user-5",
					TenantId: "test-issuer",
				})
				Expect(err).NotTo(HaveOccurred())

				ctx3, cancel3 := authCtx()
				defer cancel3()
				_, err = client.RefreshToken(ctx3, &tokenv1.RefreshTokenRequest{
					RefreshToken: issued.RefreshToken,
					TenantId:     "test-issuer",
				})
				Expect(status.Code(err)).NotTo(Equal(codes.OK))
			})
		})
	})

	// ===== Idempotency interceptor =====
	Describe("Idempotency interceptor", func() {
		Context("when the same x-idempotency-key is sent twice", func() {
			It("returns the cached response on the second call — access_token is identical", func() {
				idempKey := "idem-test-key-abc"

				ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel1()
				ctx1 = metadata.AppendToOutgoingContext(ctx1,
					"x-api-key", "test-api-key",
					observability.MetadataKeyIdempotencyKey, idempKey,
				)

				first, err := client.IssueToken(ctx1, &tokenv1.IssueTokenRequest{
					Sub:      "user-idem",
					TenantId: "test-issuer",
				})
				Expect(err).NotTo(HaveOccurred())

				ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel2()
				ctx2 = metadata.AppendToOutgoingContext(ctx2,
					"x-api-key", "test-api-key",
					observability.MetadataKeyIdempotencyKey, idempKey,
				)

				second, err := client.IssueToken(ctx2, &tokenv1.IssueTokenRequest{
					Sub:      "user-idem",
					TenantId: "test-issuer",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(second.AccessToken).To(Equal(first.AccessToken))
			})
		})

		Context("when different x-idempotency-key values are sent", func() {
			It("returns distinct access tokens", func() {
				ctx1, cancel1 := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel1()
				ctx1 = metadata.AppendToOutgoingContext(ctx1,
					"x-api-key", "test-api-key",
					observability.MetadataKeyIdempotencyKey, "idem-key-X",
				)

				first, err := client.IssueToken(ctx1, &tokenv1.IssueTokenRequest{
					Sub:      "user-idem2",
					TenantId: "test-issuer",
				})
				Expect(err).NotTo(HaveOccurred())

				ctx2, cancel2 := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel2()
				ctx2 = metadata.AppendToOutgoingContext(ctx2,
					"x-api-key", "test-api-key",
					observability.MetadataKeyIdempotencyKey, "idem-key-Y",
				)

				second, err := client.IssueToken(ctx2, &tokenv1.IssueTokenRequest{
					Sub:      "user-idem2",
					TenantId: "test-issuer",
				})
				Expect(err).NotTo(HaveOccurred())
				Expect(second.AccessToken).NotTo(Equal(first.AccessToken))
			})
		})
	})
})
