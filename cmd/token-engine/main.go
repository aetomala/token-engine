package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	grpchealth "google.golang.org/grpc/health"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"github.com/aetomala/token-engine/internal/audit"
	"github.com/aetomala/token-engine/internal/config"
	"github.com/aetomala/token-engine/internal/handler"
	internalhealth "github.com/aetomala/token-engine/internal/health"
	"github.com/aetomala/token-engine/internal/interceptor"
	"github.com/aetomala/token-engine/internal/lock"
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/reconciliation"
	"github.com/aetomala/token-engine/internal/registry"
	"github.com/aetomala/token-engine/internal/store"
)

const (
	lockKeyRotationPrefix       = "locks:key_rotation:"
	rotationLastGeneratedPrefix = "key_rotation:last_generated:"
)

func main() {
	ctx, cancelCtx := context.WithCancel(context.Background())

	// ===== Load Configuration =====
	cfg, err := config.Load()
	if err != nil {
		log.Printf("failed to load config: %v", err)
		os.Exit(1)
	}

	// ===== Logger =====
	logger := observability.NewSlogLogger(os.Stderr)

	// ===== OTel Setup =====
	var sdkTracer trace.Tracer
	otelShutdown := func(context.Context) error { return nil }
	if cfg.OTLPEndpoint != "" {
		exporter, err := otlptracegrpc.New(
			ctx,
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlptracegrpc.WithInsecure(),
		)
		if err != nil {
			log.Printf("failed to create OTLP exporter: %v", err)
			os.Exit(1)
		}
		tp := sdktrace.NewTracerProvider(
			sdktrace.WithBatcher(exporter),
			sdktrace.WithResource(resource.NewWithAttributes(
				semconv.SchemaURL,
				semconv.ServiceNameKey.String("token-engine"),
			)),
		)
		sdkTracer = tp.Tracer("token-engine")
		otelShutdown = tp.Shutdown
	} else {
		noopTP := tracenoop.NewTracerProvider()
		sdkTracer = noopTP.Tracer("token-engine")
	}

	// ===== Prometheus Registry =====
	promReg := prometheus.NewRegistry()

	// ===== Observability (Metrics and Tracers) =====
	metrics := observability.NewPrometheusMetrics(promReg)
	tracer := observability.NewOtelTracer(sdkTracer)

	// ===== Redis Client =====
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	// ===== Redis Startup Retry =====
	retryStart := time.Now()
	for {
		if err := redisClient.Ping(ctx).Err(); err == nil {
			break
		}
		if time.Since(retryStart) >= registry.RedisStartupRetryMaxWait {
			logger.Error(ctx, "redis unreachable after retry window; aborting",
				"max_wait", registry.RedisStartupRetryMaxWait.String())
			os.Exit(1)
		}
		time.Sleep(registry.RedisStartupRetryInterval)
	}

	locker := lock.NewRedisLocker(redisClient, logger)

	// ===== Tenant Registry =====
	tenantReg := registry.NewMultiTenantRegistry(redisClient, promReg, logger, tracer, metrics)
	if err := tenantReg.Add(ctx, cfg.Issuer, registry.TenantConfig{
		Issuer:   cfg.Issuer,
		Audience: cfg.Audience,
	}); err != nil {
		logger.Error(ctx, "failed to add tenant", "error", err)
		os.Exit(1)
	}

	// ===== Authenticator =====
	var auth interceptor.Authenticator
	if cfg.TLSMode == "mtls" {
		auth = interceptor.NewMTLSAuthenticator()
	} else {
		auth = interceptor.NewStaticKeyAuthenticator(cfg.StaticCallerKeys)
	}

	// ===== gRPC Server Credentials =====
	var grpcOpts []grpc.ServerOption
	if cfg.TLSMode == "mtls" {
		cert, err := tls.LoadX509KeyPair(cfg.TLSCertFile, cfg.TLSKeyFile)
		if err != nil {
			log.Printf("failed to load TLS cert/key: %v", err)
			os.Exit(1)
		}
		caCert, err := os.ReadFile(cfg.TLSCAFile)
		if err != nil {
			log.Printf("failed to read CA cert: %v", err)
			os.Exit(1)
		}
		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caCert) {
			log.Printf("failed to parse CA cert from %s", cfg.TLSCAFile)
			os.Exit(1)
		}
		tlsCfg := &tls.Config{
			Certificates: []tls.Certificate{cert},
			ClientAuth:   tls.RequireAndVerifyClientCert,
			ClientCAs:    caPool,
			MinVersion:   tls.VersionTLS13,
		}
		grpcOpts = append(grpcOpts, grpc.Creds(credentials.NewTLS(tlsCfg)))
	}

	// ===== Stores =====
	idempStore := store.NewRedisIdempotencyStore(redisClient, cfg.IdempotencyTTL)

	// ===== Caller Registry =====
	var callerReg registry.CallerRegistry
	if cfg.CallerRegistryPath != "" {
		callerCfg, err := registry.LoadCallerRegistryConfig(cfg.CallerRegistryPath)
		if err != nil {
			logger.Error(ctx, "failed to load caller registry", "error", err)
			os.Exit(1)
		}
		if callerCfg.Version != 1 {
			log.Printf("caller registry version must be 1, got %d", callerCfg.Version)
			os.Exit(1)
		}
		seen := make(map[string]bool)
		for _, entry := range callerCfg.Callers {
			if entry.Identity == "" {
				log.Printf("caller registry: entry with empty identity")
				os.Exit(1)
			}
			if len(entry.PermittedTenants) == 0 {
				log.Printf("caller registry: entry %q has no permitted_tenants", entry.Identity)
				os.Exit(1)
			}
			if seen[entry.Identity] {
				log.Printf("caller registry: duplicate identity %q", entry.Identity)
				os.Exit(1)
			}
			seen[entry.Identity] = true
		}
		callerReg = registry.NewStaticCallerRegistry(callerCfg, logger)
	} else {
		callerReg = registry.NewStaticCallerRegistry(&registry.CallerRegistryConfig{Version: 1}, logger)
	}

	// ===== Audit and Reconciliation =====
	auditStore := audit.NewSlogAuditStore(logger)

	allManagers := tenantReg.GetAll()
	reconciler := reconciliation.NewCursorReconciler(
		allManagers,
		locker,
		redisClient,
		logger,
		metrics,
		cfg.ReconciliationPageSize,
		cfg.LockTTL,
	)

	// ===== Key Rotation Guard =====
	for tenantID, km := range tenantReg.AllKeyManagers() {
		tenantID, km := tenantID, km
		go func() {
			ticker := time.NewTicker(cfg.RotationWindowGuard)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					lastGenKey := rotationLastGeneratedPrefix + tenantID
					val, _ := redisClient.Get(ctx, lastGenKey).Result()
					if val != "" {
						if t, err := time.Parse(time.RFC3339Nano, val); err == nil {
							if time.Since(t) < cfg.RotationWindowGuard {
								logger.Debug(ctx, "key rotation skipped: within guard window", "tenant_id", tenantID)
								continue
							}
						}
					}
					lk, err := locker.Acquire(ctx, lockKeyRotationPrefix+tenantID, cfg.LockTTL)
					if err != nil {
						logger.Info(ctx, "key rotation skipped: lock not acquired", "tenant_id", tenantID)
						continue
					}
					if err := km.RotateKeys(ctx); err != nil {
						logger.Warn(ctx, "key rotation error", "tenant_id", tenantID, "error", err)
						_ = lk.Release(ctx)
						continue
					}
					_ = redisClient.Set(ctx, lastGenKey, time.Now().Format(time.RFC3339Nano), 0).Err()
					_ = lk.Release(ctx)
				}
			}
		}()
	}

	// ===== Reconciliation Runner =====
	go func() {
		ticker := time.NewTicker(cfg.ReconciliationInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				_ = reconciler.Run(ctx)
			}
		}
	}()

	// ===== Interceptors =====
	// Order: otelgrpc → correlation → auth → caller authorization → idempotency → validation
	correlationInterceptor := observability.NewCorrelationInterceptor(logger, metrics)
	authInterceptor := interceptor.NewAuthInterceptor(auth, logger)
	callerAuthInterceptor := interceptor.NewCallerAuthorizationInterceptor(callerReg, logger)
	idempotencyInterceptor := interceptor.NewIdempotencyInterceptor(idempStore, logger, metrics)
	validationInterceptor := interceptor.NewValidationInterceptor(logger)

	// ===== gRPC Server =====
	grpcServerOpts := append(grpcOpts,
		grpc.ChainUnaryInterceptor(
			otelgrpc.UnaryServerInterceptor(), //nolint:staticcheck // v0.52.0 pinned; NewServerHandler requires grpc.StatsHandler wiring removed in v0.60.0+
			correlationInterceptor,
			authInterceptor,
			callerAuthInterceptor,
			idempotencyInterceptor,
			validationInterceptor,
		),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			MaxConnectionAge:      cfg.MaxConnectionAge,
			MaxConnectionAgeGrace: cfg.MaxConnectionAgeGrace,
		}),
	)
	grpcServer := grpc.NewServer(grpcServerOpts...)

	// ===== Register Handlers =====
	tokenHandler := handler.NewTokenHandler(tenantReg, auditStore, logger, tracer, metrics)
	tokenv1.RegisterTokenEngineServer(grpcServer, tokenHandler)

	// ===== gRPC Health =====
	grpcHealthServer := grpchealth.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, grpcHealthServer)

	// ===== gRPC Reflection (only when TLS is disabled) =====
	if cfg.TLSMode == "disabled" {
		reflection.Register(grpcServer)
	}

	// ===== HTTP Mux =====
	checkers := []internalhealth.Checker{
		internalhealth.NewRedisChecker(redisClient),
		internalhealth.NewAuditChecker(auditStore),
	}
	for _, tenantKM := range tenantReg.AllKeyManagers() {
		checkers = append(checkers, internalhealth.NewKeyAvailabilityChecker(tenantKM))
	}
	httpMux := http.NewServeMux()
	httpMux.Handle("GET /healthz/live", internalhealth.NewLiveHandler())
	httpMux.Handle("GET /healthz/ready", internalhealth.NewReadyHandler(checkers))
	httpMux.HandleFunc("GET /.well-known/jwks.json", handler.JWKSHandler(tenantReg.AllKeyManagers()[cfg.Issuer], cfg.Issuer, cfg, metrics))
	httpMux.Handle("GET /metrics", promhttp.HandlerFor(promReg, promhttp.HandlerOpts{}))

	// ===== HTTP Server =====
	httpServer := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpMux,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// ===== Bind gRPC Listener — fail fast on port conflict before spawning goroutines =====
	grpcListener, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		log.Printf("failed to listen on %s: %v", cfg.GRPCAddr, err)
		os.Exit(1)
	}

	// ===== Start Servers =====
	go func() {
		if err := grpcServer.Serve(grpcListener); err != nil {
			log.Printf("grpc server error: %v", err)
		}
	}()

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("http server error: %v", err)
		}
	}()

	// ===== Signal Handling =====
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	<-sigChan
	cancelCtx()

	// ===== Graceful Shutdown =====
	// 30 s total budget covers gRPC drain, OTel flush, key manager, and HTTP.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 1. Drain gRPC — 10 s sub-deadline so in-flight RPCs cannot block the full window.
	grpcDone := make(chan struct{})
	go func() {
		grpcServer.GracefulStop()
		close(grpcDone)
	}()
	select {
	case <-grpcDone:
	case <-time.After(10 * time.Second):
		log.Print("graceful gRPC stop timed out; forcing stop")
		grpcServer.Stop()
	}

	// 2. Flush buffered OTel spans before the process exits.
	if err := otelShutdown(shutdownCtx); err != nil {
		log.Printf("otel shutdown error: %v", err)
	}

	// 3. Stop key manager background goroutines.
	for tenantID, tenantKM := range tenantReg.AllKeyManagers() {
		if err := tenantKM.Shutdown(shutdownCtx); err != nil {
			log.Printf("key manager shutdown error for tenant %s: %v", tenantID, err)
		}
	}

	// 4. Stop HTTP last — keeps health and metrics available through the gRPC drain.
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http server shutdown error: %v", err)
	}
}
