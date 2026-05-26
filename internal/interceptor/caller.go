package interceptor

import (
	"context"

	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/registry"
	"google.golang.org/grpc"
)

// ===== NewCallerAuthorizationInterceptor =====

// NewCallerAuthorizationInterceptor returns a gRPC unary server interceptor for caller authorization.
// v0.1–v0.4: stub — return handler(ctx, req). registry and logger accepted but unused.
// v0.5: real implementation — registry lookup against caller identity and tenant_id.
func NewCallerAuthorizationInterceptor(reg registry.CallerRegistry, logger observability.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		return handler(ctx, req)
	}
}
