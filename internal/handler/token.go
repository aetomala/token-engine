package handler

import (
	"context"

	"github.com/aetomala/jwtauth/pkg/tokens"
	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/registry"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// D2 — Audit asymmetry: Revocation operations are gated on audit.Store availability.
// Issuance operations are NOT gated — a temporarily unavailable audit store must not block
// token issuance. Revocation handlers must call store.RecordRevocation before revoking,
// and abort if RecordRevocation returns an error.
// This guard applies to: RevokeToken, RevokeAllForAudience, RevokeAllUserTokens.

// TokenHandler implements the TokenEngine gRPC service.
type TokenHandler struct {
	registry registry.TenantRegistry
	logger   observability.Logger
	tracer   observability.Tracer
	metrics  observability.Metrics
	tokenv1.UnimplementedTokenEngineServer
}

// NewTokenHandler returns a new TokenHandler wired with the given dependencies.
func NewTokenHandler(
	registry registry.TenantRegistry,
	logger observability.Logger,
	tracer observability.Tracer,
	metrics observability.Metrics,
) *TokenHandler {
	return &TokenHandler{
		registry: registry,
		logger:   logger,
		tracer:   tracer,
		metrics:  metrics,
	}
}

// IssueToken issues a new access/refresh token pair for the requesting tenant.
func (h *TokenHandler) IssueToken(ctx context.Context, req *tokenv1.IssueTokenRequest) (*tokenv1.TokenPair, error) {
	// ===== STEP 1: Open span =====
	ctx, span := h.tracer.Start(ctx, "IssueToken")
	defer span.End()

	// ===== STEP 2: Get tenant manager =====
	manager, err := h.registry.Get(ctx, req.TenantId)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(observability.StatusError, err.Error())
		return nil, err
	}

	// ===== STEP 3: Build options =====
	var opts []tokens.IssueOption
	if len(req.Audiences) > 0 {
		opts = append(opts, tokens.WithAudience(req.Audiences...))
	}

	// ===== STEP 4: Convert claims =====
	customClaims := make(tokens.CustomClaims, len(req.Claims))
	for k, v := range req.Claims {
		customClaims[k] = v
	}

	// ===== STEP 5: Issue token pair =====
	access, refresh, err := manager.IssueTokenPairWithClaims(ctx, req.Sub, customClaims, nil, opts...)
	if err != nil {
		mapped := observability.MapLibraryError(err)
		span.RecordError(mapped)
		span.SetStatus(observability.StatusError, mapped.Error())
		return nil, mapped
	}

	span.SetStatus(observability.StatusOK, "")
	return &tokenv1.TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}

// RefreshToken issues a new access token using a valid refresh token.
func (h *TokenHandler) RefreshToken(ctx context.Context, req *tokenv1.RefreshTokenRequest) (*tokenv1.TokenPair, error) {
	// ===== STEP 1: Open span =====
	ctx, span := h.tracer.Start(ctx, "RefreshToken")
	defer span.End()

	// ===== STEP 2: Get tenant manager =====
	manager, err := h.registry.Get(ctx, req.TenantId)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(observability.StatusError, err.Error())
		return nil, err
	}

	// ===== STEP 3: Convert claims =====
	customClaims := make(tokens.CustomClaims, len(req.Claims))
	for k, v := range req.Claims {
		customClaims[k] = v
	}

	// ===== STEP 4: Refresh access token =====
	access, err := manager.RefreshAccessTokenWithClaims(ctx, req.RefreshToken, customClaims)
	if err != nil {
		mapped := observability.MapLibraryError(err)
		span.RecordError(mapped)
		span.SetStatus(observability.StatusError, mapped.Error())
		return nil, mapped
	}

	span.SetStatus(observability.StatusOK, "")
	return &tokenv1.TokenPair{AccessToken: access}, nil
}

// RevokeToken revokes a specific token. Unimplemented in v0.2.
func (h *TokenHandler) RevokeToken(ctx context.Context, req *tokenv1.RevokeTokenRequest) (*tokenv1.RevokeTokenResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

// RevokeAllForAudience revokes all tokens for an audience. Unimplemented in v0.2.
func (h *TokenHandler) RevokeAllForAudience(ctx context.Context, req *tokenv1.RevokeAudienceRequest) (*tokenv1.RevokeTokenResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}

// RevokeAllUserTokens revokes all tokens for a user. Unimplemented in v0.2.
func (h *TokenHandler) RevokeAllUserTokens(ctx context.Context, req *tokenv1.RevokeUserRequest) (*tokenv1.RevokeTokenResponse, error) {
	return nil, status.Error(codes.Unimplemented, "not implemented")
}
