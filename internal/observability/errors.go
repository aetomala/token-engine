package observability

import (
	"errors"

	"github.com/aetomala/jwtauth/pkg/keys"
	"github.com/aetomala/jwtauth/pkg/storage"
	"github.com/aetomala/jwtauth/pkg/tokens"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// ===== Library Error Mapping =====

// MapLibraryError converts library error sentinels to gRPC status errors.
func MapLibraryError(err error) error {
	if err == nil {
		return nil
	}
	// map known jwtauth sentinel errors to gRPC status errors
	switch {
	case errors.Is(err, tokens.ErrTokenRevoked):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, storage.ErrTokenNotFound):
		return status.Error(codes.NotFound, err.Error())
	case errors.Is(err, tokens.ErrInvalidAudience):
		return status.Error(codes.PermissionDenied, err.Error())
	case errors.Is(err, keys.ErrKeyStoreInvalidKeyID):
		return status.Error(codes.Internal, err.Error())
	case errors.Is(err, tokens.ErrTokenMissingKid):
		return status.Error(codes.Internal, err.Error())
	case errors.Is(err, tokens.ErrTokenExpired):
		return status.Error(codes.Unauthenticated, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
