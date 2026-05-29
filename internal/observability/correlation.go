package observability

import (
	"context"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ===== Context Key Types =====

// CorrelationIDKey is the context key for correlation IDs.
type CorrelationIDKey struct{}

// CallerIdentityKey is the context key for caller identities.
type CallerIdentityKey struct{}

// ===== Metadata Key Constants =====

const (
	MetadataKeyCorrelationID  = "x-correlation-id"
	MetadataKeyAPIKey         = "x-api-key"
	MetadataKeyIdempotencyKey = "x-idempotency-key"
)

// ===== Correlation ID Accessors =====

// CorrelationIDFromContext extracts the correlation ID from the context.
func CorrelationIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(CorrelationIDKey{}).(string)
	return v
}

// WithCorrelationID adds a correlation ID to the context.
func WithCorrelationID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, CorrelationIDKey{}, id)
}

// ===== Caller Identity Accessors =====

// CallerIdentityFromContext extracts the caller identity from the context.
func CallerIdentityFromContext(ctx context.Context) string {
	v, _ := ctx.Value(CallerIdentityKey{}).(string)
	return v
}

// WithCallerIdentity adds a caller identity to the context.
func WithCallerIdentity(ctx context.Context, identity string) context.Context {
	return context.WithValue(ctx, CallerIdentityKey{}, identity)
}

// ===== Correlation Interceptor =====

// NewCorrelationInterceptor creates a gRPC unary server interceptor that manages correlation IDs
// and emits per-RPC request count and duration metrics.
func NewCorrelationInterceptor(logger Logger, metrics Metrics) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// ===== STEP 1: Extract or generate correlation ID =====
		md, ok := metadata.FromIncomingContext(ctx)
		var corrID string
		if ok {
			if values := md.Get(MetadataKeyCorrelationID); len(values) > 0 {
				corrID = values[0]
			}
		}
		if corrID == "" {
			corrID = uuid.New().String()
		}

		// ===== STEP 2: Bind correlation ID to context =====
		ctx = WithCorrelationID(ctx, corrID)

		// ===== STEP 3: Set correlation ID on active OTel span =====
		span := trace.SpanFromContext(ctx)
		span.SetAttributes(attribute.String("correlation_id", corrID))

		// ===== STEP 4: Set correlation ID in response metadata =====
		_ = grpc.SetHeader(ctx, metadata.Pairs(MetadataKeyCorrelationID, corrID))

		// ===== STEP 5: Call handler and emit metrics =====
		start := time.Now()
		resp, err := handler(ctx, req)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}
		metrics.IncrementCounter(MetricGRPCRequests, map[string]string{
			"rpc_method": info.FullMethod,
			"status":     code.String(),
		})
		metrics.RecordDuration(MetricGRPCDuration, time.Since(start), map[string]string{
			"rpc_method": info.FullMethod,
		})
		return resp, err
	}
}
