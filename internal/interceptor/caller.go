package interceptor

import (
	"context"
	"strings"

	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/registry"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// TenantAwareRequest is satisfied by all proto request messages containing a tenant_id field.
// Proto-gen-go generates GetTenantId() string for every proto3 message with a string tenant_id field.
// Used by the caller interceptor to extract tenant_id without type-asserting each request type individually.
type TenantAwareRequest interface {
	GetTenantId() string
}

const (
	grpcHealthPrefix = "/grpc.health.v1.Health/"
)

// NewCallerAuthorizationInterceptor returns a gRPC unary server interceptor for caller authorization.
// Skips authorization for gRPC health protocol RPCs.
// Returns codes.Internal if no caller identity is present in context (auth interceptor must run first).
// Returns codes.PermissionDenied if the caller is not authorized for the requested tenant.
func NewCallerAuthorizationInterceptor(reg registry.CallerRegistry, logger observability.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// ===== STEP 1: Skip health protocol RPCs =====
		if strings.HasPrefix(info.FullMethod, grpcHealthPrefix) {
			return handler(ctx, req)
		}

		// ===== STEP 2: Extract caller identity =====
		callerIdentity := observability.CallerIdentityFromContext(ctx)
		if callerIdentity == "" {
			return nil, status.Error(codes.Internal, "caller identity not set in context")
		}

		// ===== STEP 3: Extract tenantID =====
		tenantID := ""
		if tar, ok := req.(TenantAwareRequest); ok {
			tenantID = tar.GetTenantId()
		}

		// ===== STEP 4: Check authorization =====
		permitted, err := reg.IsPermitted(ctx, callerIdentity, tenantID)
		if err != nil {
			return nil, err
		}
		if !permitted {
			return nil, status.Error(codes.PermissionDenied, "caller not authorized")
		}

		// ===== STEP 5: Call handler =====
		return handler(ctx, req)
	}
}
