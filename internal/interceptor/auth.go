package interceptor

import (
	"context"

	"github.com/aetomala/token-engine/internal/observability"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// ===== Authenticator Interface =====

// Authenticator extracts and verifies a caller identity from the gRPC request context.
type Authenticator interface {
	// Authenticate extracts a verified caller identity from the gRPC request context.
	// Returns the caller identity string on success.
	// Returns a gRPC status error directly — callers must not wrap the error.
	Authenticate(ctx context.Context) (callerIdentity string, err error)
}

// ===== StaticKeyAuthenticator =====

// StaticKeyAuthenticator authenticates requests using a static mapping of API keys to caller identities.
type StaticKeyAuthenticator struct {
	keys map[string]string
}

// NewStaticKeyAuthenticator returns a new StaticKeyAuthenticator with the given key→identity mapping.
func NewStaticKeyAuthenticator(keys map[string]string) *StaticKeyAuthenticator {
	return &StaticKeyAuthenticator{
		keys: keys,
	}
}

// Authenticate extracts the API key from gRPC metadata and returns the mapped caller identity.
// If the key is absent or not found in the map, returns codes.Unauthenticated.
func (a *StaticKeyAuthenticator) Authenticate(ctx context.Context) (string, error) {
	// ===== STEP 1: Extract API key from metadata =====
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "invalid or missing api key")
	}

	vals := md.Get(observability.MetadataKeyAPIKey)
	if len(vals) == 0 {
		return "", status.Error(codes.Unauthenticated, "invalid or missing api key")
	}

	apiKey := vals[0]

	// ===== STEP 2: Lookup identity in key map =====
	identity, found := a.keys[apiKey]
	if !found {
		return "", status.Error(codes.Unauthenticated, "invalid or missing api key")
	}

	return identity, nil
}

// ===== NewAuthInterceptor =====

// NewAuthInterceptor returns a gRPC unary server interceptor that authenticates requests.
// On success, the caller identity is bound to the context via WithCallerIdentity.
// On authentication failure, the error is returned without calling the handler.
func NewAuthInterceptor(auth Authenticator, logger observability.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		// ===== STEP 1: Authenticate =====
		identity, err := auth.Authenticate(ctx)
		if err != nil {
			return nil, err
		}

		// ===== STEP 2: Bind identity to context =====
		ctx = observability.WithCallerIdentity(ctx, identity)

		// ===== STEP 3: Call handler =====
		return handler(ctx, req)
	}
}
