package interceptor

import (
	"context"

	"github.com/aetomala/token-engine/internal/observability"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
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

// ===== MTLSAuthenticator =====

// MTLSAuthenticator authenticates requests using the TLS peer certificate Common Name.
// The gRPC server is configured with tls.RequireAndVerifyClientCert — the certificate
// has already been cryptographically verified before Authenticate is called.
// MTLSAuthenticator only extracts identity; it does not perform certificate verification.
// All methods are safe for concurrent use.
type MTLSAuthenticator struct{}

var _ Authenticator = (*MTLSAuthenticator)(nil)

// NewMTLSAuthenticator returns a new MTLSAuthenticator.
func NewMTLSAuthenticator() *MTLSAuthenticator {
	return &MTLSAuthenticator{}
}

// Authenticate extracts the caller identity from the TLS peer certificate Common Name.
// Returns codes.Unauthenticated if: no peer in context, peer has no TLS credentials,
// peer has no certificates, or the leaf certificate has an empty Common Name.
func (a *MTLSAuthenticator) Authenticate(ctx context.Context) (string, error) {
	p, ok := peer.FromContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "no peer information in context")
	}
	tlsInfo, ok := p.AuthInfo.(credentials.TLSInfo)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "no TLS credentials in peer")
	}
	if len(tlsInfo.State.PeerCertificates) == 0 {
		return "", status.Error(codes.Unauthenticated, "no client certificates provided")
	}
	cn := tlsInfo.State.PeerCertificates[0].Subject.CommonName
	if cn == "" {
		return "", status.Error(codes.Unauthenticated, "client certificate Common Name is empty")
	}
	return cn, nil
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
