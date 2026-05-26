package interceptor

import (
	"context"

	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/store"
	"google.golang.org/grpc"
)

// ===== NewIdempotencyInterceptor =====

// NewIdempotencyInterceptor returns a gRPC unary server interceptor for idempotency handling.
// v0.1–v0.3: stub — return handler(ctx, req). store, logger, metrics accepted but unused.
// v0.4: real implementation — check store before handler, cache response after.
func NewIdempotencyInterceptor(st store.IdempotencyStore, logger observability.Logger, metrics observability.Metrics) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}
}
