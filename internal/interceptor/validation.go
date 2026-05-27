package interceptor

import (
	"context"

	"github.com/aetomala/token-engine/internal/observability"
	"google.golang.org/grpc"
)

// ===== NewValidationInterceptor =====

// NewValidationInterceptor returns a gRPC unary server interceptor for request validation.
// v0.1: stub — return handler(ctx, req). logger accepted but unused.
// v0.2: enforces empty sub rejection and reserved claim key rejection.
func NewValidationInterceptor(logger observability.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}
}
