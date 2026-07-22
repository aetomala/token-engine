package interceptor

import (
	"context"

	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"github.com/aetomala/token-engine/internal/observability"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ReservedClaimKeys is the set of JWT claim keys the validation interceptor rejects.
var ReservedClaimKeys = map[string]struct{}{
	"sub": {}, "iss": {}, "aud": {}, "exp": {}, "iat": {}, "nbf": {}, "jti": {},
}

// NewValidationInterceptor returns a gRPC unary server interceptor for request validation.
// Enforces non-empty tenant_id on every tenant-aware RPC, non-empty sub on IssueToken, and
// rejects reserved JWT claim keys on IssueToken and RefreshToken. All other methods pass
// through unchanged.
func NewValidationInterceptor(logger observability.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// ===== STEP 1: Validate tenant_id on tenant-aware requests =====
		if tar, ok := req.(TenantAwareRequest); ok {
			if tar.GetTenantId() == "" {
				return nil, status.Error(codes.InvalidArgument, "tenant_id must not be empty")
			}
		}
		// ===== STEP 2: Route by method =====
		switch info.FullMethod {
		case tokenv1.TokenEngine_IssueToken_FullMethodName:
			// ===== STEP 3: Validate IssueToken request =====
			r := req.(*tokenv1.IssueTokenRequest)
			if r.Sub == "" {
				return nil, status.Error(codes.InvalidArgument, "sub must not be empty")
			}
			for key := range r.Claims {
				if _, reserved := ReservedClaimKeys[key]; reserved {
					return nil, status.Errorf(codes.InvalidArgument, "claims key %q is reserved", key)
				}
			}
		case tokenv1.TokenEngine_RefreshToken_FullMethodName:
			// ===== STEP 4: Validate RefreshToken request =====
			r := req.(*tokenv1.RefreshTokenRequest)
			for key := range r.Claims {
				if _, reserved := ReservedClaimKeys[key]; reserved {
					return nil, status.Errorf(codes.InvalidArgument, "claims key %q is reserved", key)
				}
			}
		}
		// ===== STEP 5: Pass through =====
		return handler(ctx, req)
	}
}
