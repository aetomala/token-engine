package handler

import (
	"context"

	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// D2 — Audit asymmetry: Revocation operations are gated on audit.Store availability.
// Issuance operations are NOT gated — a temporarily unavailable audit store must not block
// token issuance. Revocation handlers must call store.RecordRevocation before revoking,
// and abort if RecordRevocation returns an error.
// This guard applies to: RevokeToken, RevokeAllForAudience, RevokeAllUserTokens.
// v0.1: all handlers are Unimplemented — this comment is a guard for future contributors.

// TokenHandler implements the TokenEngine gRPC service.
type TokenHandler struct {
	tokenv1.UnimplementedTokenEngineServer
}

// NewTokenHandler returns a new TokenHandler.
func NewTokenHandler() *TokenHandler {
	return &TokenHandler{}
}

// IssueToken issues a new token. Unimplemented in v0.1.
func (h *TokenHandler) IssueToken(ctx context.Context, req *tokenv1.IssueTokenRequest) (*tokenv1.TokenPair, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

// RefreshToken refreshes an existing token. Unimplemented in v0.1.
func (h *TokenHandler) RefreshToken(ctx context.Context, req *tokenv1.RefreshTokenRequest) (*tokenv1.TokenPair, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

// RevokeToken revokes a specific token. Unimplemented in v0.1.
func (h *TokenHandler) RevokeToken(ctx context.Context, req *tokenv1.RevokeTokenRequest) (*tokenv1.RevokeTokenResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

// RevokeAllForAudience revokes all tokens for an audience. Unimplemented in v0.1.
func (h *TokenHandler) RevokeAllForAudience(ctx context.Context, req *tokenv1.RevokeAudienceRequest) (*tokenv1.RevokeTokenResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

// RevokeAllUserTokens revokes all tokens for a user. Unimplemented in v0.1.
func (h *TokenHandler) RevokeAllUserTokens(ctx context.Context, req *tokenv1.RevokeUserRequest) (*tokenv1.RevokeTokenResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
