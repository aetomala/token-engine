package client

import (
	"context"
	"errors"

	tokenv1 "github.com/aetomala/token-engine/gen/v1"
)

// Sentinel errors for Client operations.
var (
	ErrNoCredentials = errors.New("no transport credentials: use WithMTLS or WithPlaintext")
	ErrInvalidCA     = errors.New("CA certificate PEM could not be parsed")
)

// Client is a token-engine gRPC client. It wraps the generated TokenEngine stubs and handles
// connection lifecycle, transport credentials, and optional static API key injection. It is
// suitable for use in multi-goroutine environments — all methods are safe for concurrent use.
type Client interface {
	// IssueToken issues a new token pair for the given subject and tenant.
	// Returns the context error if the context is cancelled.
	IssueToken(ctx context.Context, req *tokenv1.IssueTokenRequest) (*tokenv1.TokenPair, error)

	// RefreshToken exchanges a refresh token for a new token pair.
	// Returns the context error if the context is cancelled.
	RefreshToken(ctx context.Context, req *tokenv1.RefreshTokenRequest) (*tokenv1.TokenPair, error)

	// RevokeToken revokes a single refresh token by value.
	// Returns the context error if the context is cancelled.
	RevokeToken(ctx context.Context, req *tokenv1.RevokeTokenRequest) (*tokenv1.RevokeTokenResponse, error)

	// RevokeAllForAudience revokes all refresh tokens for the given audience within a tenant.
	// Returns the context error if the context is cancelled.
	RevokeAllForAudience(ctx context.Context, req *tokenv1.RevokeAudienceRequest) (*tokenv1.RevokeTokenResponse, error)

	// RevokeAllUserTokens revokes all refresh tokens for the given user within a tenant.
	// Returns the context error if the context is cancelled.
	RevokeAllUserTokens(ctx context.Context, req *tokenv1.RevokeUserRequest) (*tokenv1.RevokeTokenResponse, error)

	// RevokeAllForUserAndAudience revokes all refresh tokens for the given user and audience within a tenant.
	// Returns the context error if the context is cancelled.
	RevokeAllForUserAndAudience(ctx context.Context, req *tokenv1.RevokeUserAndAudienceRequest) (*tokenv1.RevokeTokenResponse, error)

	// Close tears down the underlying gRPC connection. It is idempotent — calling Close on a
	// stub-backed client (created via NewClientFromStub) is a no-op.
	Close() error
}
