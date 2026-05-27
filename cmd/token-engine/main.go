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
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
	"google.golang.org/grpc"
	grpc_health_v1 "google.golang.org/grpc/health/grpc_health_v1"
	grpchealth "google.golang.org/grpc/health"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/reflection"

	"github.com/aetomala/token-engine/internal/audit"
	"github.com/aetomala/token-engine/internal/config"
	internalhealth "github.com/aetomala/token-engine/internal/health"
	"github.com/aetomala/token-engine/internal/handler"
	"github.com/aetomala/token-engine/internal/interceptor"
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/reconciliation"
	"github.com/aetomala/token-engine/internal/registry"
	"github.com/aetomala/token-engine/internal/store"
	tokenv1 "github.com/aetomala/token-engine/gen/v1"
)

func main() {
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
	if cfg.OTLPEndpoint != "" {
		ctx := context.Background()
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
	} else {
		noopTP := tracenoop.NewTracerProvider()
		sdkTracer = noopTP.Tracer("token-engine")
	}

	// ===== Prometheus Registry =====
	promReg := prometheus.NewRegistry()

	// ===== Observability (Metrics and Tracers) =====
	metrics := observability.NewPrometheusMetrics(promReg)
	_ = observability.NewOtelTracer(sdkTracer)         // service tracer; unused in v0.1
	_ = observability.NewLibraryOtelTracer(sdkTracer)  // library tracer; unused in v0.1

	// ===== Authenticator =====
	auth := interceptor.NewStaticKeyAuthenticator(cfg.StaticCallerKeys)

	// ===== Stores =====
	idempStore := store.NewNoOpIdempotencyStore()

	// ===== Registries =====
	_ = registry.NewStaticTenantRegistry(nil, logger)  // tenant registry; unused in v0.1
	callerReg := registry.NewStaticCallerRegistry(logger)

	// ===== Audit and Reconciliation =====
	auditStore := audit.NewNoOpAuditStore()
	_ = auditStore // unused in v0.1

	reconciler := reconciliation.NewNoOpReconciler()
	_ = reconciler // unused in v0.1

	// ===== Interceptors =====
	// Order: otelgrpc → correlation → auth → caller authorization → idempotency → validation
	correlationInterceptor := observability.NewCorrelationInterceptor(logger)
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
	tokenHandler := handler.NewTokenHandler()
	tokenv1.RegisterTokenEngineServer(grpcServer, tokenHandler)

	// ===== gRPC Health =====
	grpcHealthServer := grpchealth.NewServer()
	grpc_health_v1.RegisterHealthServer(grpcServer, grpcHealthServer)

	// ===== gRPC Reflection (only when TLS is disabled) =====
	if cfg.TLSMode == "disabled" {
		reflection.Register(grpcServer)
	}

	// ===== HTTP Mux =====
	httpMux := http.NewServeMux()
	httpMux.Handle("GET /healthz/live", internalhealth.NewLiveHandler())
	httpMux.Handle("GET /healthz/ready", internalhealth.NewReadyHandler(nil))
	httpMux.HandleFunc("GET /.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotImplemented)
	})
	httpMux.Handle("GET /metrics", promhttp.HandlerFor(promReg, promhttp.HandlerOpts{}))

	// ===== HTTP Server =====
	httpServer := &http.Server{
		Addr:    cfg.HTTPAddr,
		Handler: httpMux,
	}

	// ===== Start Servers in Goroutines =====
	// gRPC server
	go func() {
		listener, err := net.Listen("tcp", cfg.GRPCAddr)
		if err != nil {
			log.Printf("failed to listen on %s: %v", cfg.GRPCAddr, err)
			os.Exit(1)
		}
		if err := grpcServer.Serve(listener); err != nil {
			log.Printf("grpc server error: %v", err)
		}
	}()

	// HTTP server
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
	// gRPC first
	grpcServer.GracefulStop()

	// HTTP second
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("http server shutdown error: %v", err)
	}
}
