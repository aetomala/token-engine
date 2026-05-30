package main

import (
	"context"
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
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/reconciliation"
	"github.com/aetomala/token-engine/internal/registry"
	"github.com/aetomala/token-engine/internal/store"
)

func main() {
	ctx := context.Background()

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

	reconciler := reconciliation.NewNoOpReconciler()
	_ = reconciler

	// ===== Interceptors =====
	// Order: otelgrpc → correlation → auth → caller authorization → idempotency → validation
	correlationInterceptor := observability.NewCorrelationInterceptor(logger, metrics)
	authInterceptor := interceptor.NewAuthInterceptor(auth, logger)
	callerAuthInterceptor := interceptor.NewCallerAuthorizationInterceptor(callerReg, logger)
	idempotencyInterceptor := interceptor.NewIdempotencyInterceptor(idempStore, logger, metrics)
	validationInterceptor := interceptor.NewValidationInterceptor(logger)

	// ===== gRPC Server =====
	grpcServer := grpc.NewServer(
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
	httpMux.HandleFunc("GET /.well-known/jwks.json", handler.JWKSHandler(tenantReg.AllKeyManagers()[cfg.Issuer], cfg))
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
