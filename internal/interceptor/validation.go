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
// Every rejection returns codes.InvalidArgument naming the offending field, without calling the
// handler. Enforces non-empty tenant_id on every tenant-aware RPC, then the primary identifier of
// each TokenEngine RPC — sub on IssueToken, refresh_token on RefreshToken and RevokeToken, user_id
// on RevokeAllUserTokens, audience on RevokeAllForAudience, and user_id then audience on
// RevokeAllForUserAndAudience — and rejects reserved JWT claim keys on IssueToken and
// RefreshToken. Requests of any other type pass through unchanged.
func NewValidationInterceptor(logger observability.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// ===== STEP 1: Validate tenant_id on tenant-aware requests =====
		if tar, ok := req.(TenantAwareRequest); ok {
			if tar.GetTenantId() == "" {
				return nil, status.Error(codes.InvalidArgument, "tenant_id must not be empty")
			}
		}

		// ===== STEP 2: Route by request type =====
		switch r := req.(type) {
		case *tokenv1.IssueTokenRequest:
			// ===== STEP 3: Validate IssueToken request =====
			if r.GetSub() == "" {
				return nil, status.Error(codes.InvalidArgument, "sub must not be empty")
			}
			if err := rejectReservedClaims(r.GetClaims()); err != nil {
				return nil, err
			}
		case *tokenv1.RefreshTokenRequest:
			// ===== STEP 4: Validate RefreshToken request =====
			if r.GetRefreshToken() == "" {
				return nil, status.Error(codes.InvalidArgument, "refresh_token must not be empty")
			}
			if err := rejectReservedClaims(r.GetClaims()); err != nil {
				return nil, err
			}
		case *tokenv1.RevokeTokenRequest:
			// ===== STEP 5: Validate RevokeToken request =====
			if r.GetRefreshToken() == "" {
				return nil, status.Error(codes.InvalidArgument, "refresh_token must not be empty")
			}
		case *tokenv1.RevokeUserRequest:
			// ===== STEP 6: Validate RevokeAllUserTokens request =====
			if r.GetUserId() == "" {
				return nil, status.Error(codes.InvalidArgument, "user_id must not be empty")
			}
		case *tokenv1.RevokeAudienceRequest:
			// ===== STEP 7: Validate RevokeAllForAudience request =====
			if r.GetAudience() == "" {
				return nil, status.Error(codes.InvalidArgument, "audience must not be empty")
			}
		case *tokenv1.RevokeUserAndAudienceRequest:
			// ===== STEP 8: Validate RevokeAllForUserAndAudience request =====
			if r.GetUserId() == "" {
				return nil, status.Error(codes.InvalidArgument, "user_id must not be empty")
			}
			if r.GetAudience() == "" {
				return nil, status.Error(codes.InvalidArgument, "audience must not be empty")
			}
		}

		// ===== STEP 9: Pass through =====
		return handler(ctx, req)
	}
}

// rejectReservedClaims returns a codes.InvalidArgument error naming the first reserved JWT claim
// key found in claims, or nil if none is present.
func rejectReservedClaims(claims map[string]string) error {
	for key := range claims {
		if _, reserved := ReservedClaimKeys[key]; reserved {
			return status.Errorf(codes.InvalidArgument, "claims key %q is reserved", key)
		}
	}
	return nil
}
