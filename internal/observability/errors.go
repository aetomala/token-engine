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

// MapLibraryError returns a gRPC status error for a jwtauth error, or nil if err is nil. Sentinels
// are matched with errors.Is, so wrapped sentinels map the same as bare ones:
//   - PermissionDenied — tokens.ErrTokenRevoked, tokens.ErrInvalidAudience (claims audience mismatch)
//   - NotFound — storage.ErrTokenNotFound
//   - Unauthenticated — tokens.ErrTokenExpired, tokens.ErrRefreshTokenExpired
//   - InvalidArgument — tokens.ErrInvalidUserID, storage.ErrInvalidUserID, storage.ErrInvalidAudience
//   - Unavailable — tokens.ErrManagerNotRunning, keys.ErrManagerNotRunning
//   - Internal — keys.ErrKeyStoreInvalidKeyID, tokens.ErrTokenMissingKid, tokens.ErrInvalidRefreshToken,
//     and any unrecognized error
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
	case errors.Is(err, tokens.ErrRefreshTokenExpired):
		return status.Error(codes.Unauthenticated, err.Error())
	case errors.Is(err, tokens.ErrInvalidUserID),
		errors.Is(err, storage.ErrInvalidUserID),
		errors.Is(err, storage.ErrInvalidAudience):
		return status.Error(codes.InvalidArgument, err.Error())
	case errors.Is(err, tokens.ErrManagerNotRunning),
		errors.Is(err, keys.ErrManagerNotRunning):
		return status.Error(codes.Unavailable, err.Error())
	case errors.Is(err, tokens.ErrInvalidRefreshToken):
		// Deliberately Internal. On the refresh path jwtauth folds every storage error except
		// "revoked" into this sentinel — not-found, expired, and backend failures such as a Redis
		// outage alike. Mapping it to Unauthenticated would tell clients their credentials are bad
		// during an outage and log users out. Revisit once aetomala/jwtauth#286 distinguishes them.
		return status.Error(codes.Internal, err.Error())
	default:
		return status.Error(codes.Internal, err.Error())
	}
}
