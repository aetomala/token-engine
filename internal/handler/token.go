package handler

import (
	"context"
	"time"

	"github.com/aetomala/jwtauth/pkg/tokens"
	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"github.com/aetomala/token-engine/internal/audit"
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

const (
	spanNameRevokeToken                    = "RevokeToken"
	spanNameRevokeAllForAudience           = "RevokeAllForAudience"
	spanNameRevokeAllUserTokens            = "RevokeAllUserTokens"
	spanNameRevokeAllForUserAndAudience    = "RevokeAllForUserAndAudience"
)

// TokenHandler implements the TokenEngine gRPC service.
type TokenHandler struct {
	registry   registry.TenantRegistry
	auditStore audit.Store
	logger     observability.Logger
	tracer     observability.Tracer
	metrics    observability.Metrics
	tokenv1.UnimplementedTokenEngineServer
}

// NewTokenHandler returns a new TokenHandler wired with the given dependencies.
// V0.3: auditStore parameter added — second positional argument.
// V0.2: NewTokenHandler(registry, logger, tracer, metrics) — 4 args.
// V0.3: NewTokenHandler(registry, auditStore, logger, tracer, metrics) — 5 args.
// All parameters are required and must not be nil.
func NewTokenHandler(
	registry   registry.TenantRegistry,
	auditStore audit.Store,
	logger     observability.Logger,
	tracer     observability.Tracer,
	metrics    observability.Metrics,
) *TokenHandler {
	return &TokenHandler{
		registry:   registry,
		auditStore: auditStore,
		logger:     logger,
		tracer:     tracer,
		metrics:    metrics,
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

// RefreshToken rotates the access and refresh token pair using a valid refresh token,
// preserving custom claims on the replacement refresh token.
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

	// ===== STEP 4: Rotate access token — revokes the presented refresh token atomically =====
	access, err := manager.RefreshAccessTokenWithClaims(ctx, req.RefreshToken, customClaims)
	if err != nil {
		mapped := observability.MapLibraryError(err)
		span.RecordError(mapped)
		span.SetStatus(observability.StatusError, mapped.Error())
		return nil, mapped
	}

	// ===== STEP 5: Recover subject and audience from new access token =====
	registered, err := manager.ValidateAccessToken(ctx, access)
	if err != nil {
		h.logger.Error(ctx, "failed to validate access token during refresh rotation", "error", err)
		span.RecordError(err)
		span.SetStatus(observability.StatusError, err.Error())
		return nil, status.Error(codes.Internal, "refresh rotation: access token validation failed")
	}

	// ===== STEP 6: Issue replacement refresh token with original claims =====
	refresh, err := manager.IssueRefreshTokenWithClaims(ctx, registered.Subject, customClaims, tokens.WithAudience([]string(registered.Audience)...))
	if err != nil {
		mapped := observability.MapLibraryError(err)
		span.RecordError(mapped)
		span.SetStatus(observability.StatusError, mapped.Error())
		return nil, mapped
	}

	span.SetStatus(observability.StatusOK, "")
	return &tokenv1.TokenPair{AccessToken: access, RefreshToken: refresh}, nil
}

// RevokeToken revokes a specific refresh token by resolving its token ID and revoking it.
func (h *TokenHandler) RevokeToken(ctx context.Context, req *tokenv1.RevokeTokenRequest) (*tokenv1.RevokeTokenResponse, error) {
	// Revocation is gated on audit store availability. Issuance and refresh are not.
	// This asymmetry is intentional — see architecture doc D2.
	// A failed revocation audit leaves an undetectable gap with legal and security
	// consequences. A failed issuance leaves no token in circulation.
	// Do not add audit gating to IssueToken or RefreshToken handlers.

	// ===== STEP 1: Open span =====
	ctx, span := h.tracer.Start(ctx, spanNameRevokeToken)
	defer span.End()

	// ===== STEP 2: Audit gate =====
	if err := h.auditStore.Ping(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(observability.StatusError, err.Error())
		return nil, status.Error(codes.Unavailable, "audit store unavailable")
	}

	// ===== STEP 3: Get tenant manager =====
	manager, err := h.registry.Get(ctx, req.TenantId)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(observability.StatusError, err.Error())
		return nil, err
	}

	// ===== STEP 4: Resolve token ID via IntrospectToken =====
	tokenMetadata, err := manager.IntrospectToken(ctx, req.RefreshToken)
	if err != nil {
		mapped := observability.MapLibraryError(err)
		span.RecordError(mapped)
		span.SetStatus(observability.StatusError, mapped.Error())
		return nil, mapped
	}

	// ===== STEP 5: Revoke by token ID =====
	if err := manager.RevokeRefreshToken(ctx, tokenMetadata.TokenID); err != nil {
		mapped := observability.MapLibraryError(err)
		span.RecordError(mapped)
		span.SetStatus(observability.StatusError, mapped.Error())
		return nil, mapped
	}

	// ===== STEP 6: Record audit event =====
	event := audit.RevocationEvent{
		TenantID:       req.TenantId,
		CallerIdentity: observability.CallerIdentityFromContext(ctx),
		TokenID:        tokenMetadata.TokenID,
		Target:         "",
		Scope:          audit.RevocationScopeToken,
		OccurredAt:     time.Now().UTC(),
	}
	if err := h.auditStore.RecordRevocation(ctx, event); err != nil {
		h.logger.Error(ctx, "failed to record revocation audit", "error", err)
		span.RecordError(err)
		span.SetStatus(observability.StatusError, err.Error())
		return nil, status.Error(codes.Internal, "audit record failed")
	}

	span.SetStatus(observability.StatusOK, "")
	return &tokenv1.RevokeTokenResponse{}, nil
}

// RevokeAllForAudience revokes all tokens issued for the given audience.
func (h *TokenHandler) RevokeAllForAudience(ctx context.Context, req *tokenv1.RevokeAudienceRequest) (*tokenv1.RevokeTokenResponse, error) {
	// Revocation is gated on audit store availability. Issuance and refresh are not.
	// This asymmetry is intentional — see architecture doc D2.
	// A failed revocation audit leaves an undetectable gap with legal and security
	// consequences. A failed issuance leaves no token in circulation.
	// Do not add audit gating to IssueToken or RefreshToken handlers.

	// ===== STEP 1: Open span =====
	ctx, span := h.tracer.Start(ctx, spanNameRevokeAllForAudience)
	defer span.End()

	// ===== STEP 2: Audit gate =====
	if err := h.auditStore.Ping(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(observability.StatusError, err.Error())
		return nil, status.Error(codes.Unavailable, "audit store unavailable")
	}

	// ===== STEP 3: Get tenant manager =====
	manager, err := h.registry.Get(ctx, req.TenantId)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(observability.StatusError, err.Error())
		return nil, err
	}

	// ===== STEP 4: Revoke all tokens for audience =====
	if err := manager.RevokeAllForAudience(ctx, req.Audience); err != nil {
		mapped := observability.MapLibraryError(err)
		span.RecordError(mapped)
		span.SetStatus(observability.StatusError, mapped.Error())
		return nil, mapped
	}

	// ===== STEP 5: Record audit event =====
	event := audit.RevocationEvent{
		TenantID:       req.TenantId,
		CallerIdentity: observability.CallerIdentityFromContext(ctx),
		TokenID:        "",
		Target:         req.Audience,
		Scope:          audit.RevocationScopeAudience,
		OccurredAt:     time.Now().UTC(),
	}
	if err := h.auditStore.RecordRevocation(ctx, event); err != nil {
		h.logger.Error(ctx, "failed to record revocation audit", "error", err)
		span.RecordError(err)
		span.SetStatus(observability.StatusError, err.Error())
		return nil, status.Error(codes.Internal, "audit record failed")
	}

	span.SetStatus(observability.StatusOK, "")
	return &tokenv1.RevokeTokenResponse{}, nil
}

// RevokeAllUserTokens revokes all tokens issued for the given user.
func (h *TokenHandler) RevokeAllUserTokens(ctx context.Context, req *tokenv1.RevokeUserRequest) (*tokenv1.RevokeTokenResponse, error) {
	// Revocation is gated on audit store availability. Issuance and refresh are not.
	// This asymmetry is intentional — see architecture doc D2.
	// A failed revocation audit leaves an undetectable gap with legal and security
	// consequences. A failed issuance leaves no token in circulation.
	// Do not add audit gating to IssueToken or RefreshToken handlers.

	// ===== STEP 1: Open span =====
	ctx, span := h.tracer.Start(ctx, spanNameRevokeAllUserTokens)
	defer span.End()

	// ===== STEP 2: Audit gate =====
	if err := h.auditStore.Ping(ctx); err != nil {
		span.RecordError(err)
		span.SetStatus(observability.StatusError, err.Error())
		return nil, status.Error(codes.Unavailable, "audit store unavailable")
	}

	// ===== STEP 3: Get tenant manager =====
	manager, err := h.registry.Get(ctx, req.TenantId)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(observability.StatusError, err.Error())
		return nil, err
	}

	// ===== STEP 4: Revoke all tokens for user =====
	if err := manager.RevokeAllUserTokens(ctx, req.UserId); err != nil {
		mapped := observability.MapLibraryError(err)
		span.RecordError(mapped)
		span.SetStatus(observability.StatusError, mapped.Error())
		return nil, mapped
	}

	// ===== STEP 5: Record audit event =====
	event := audit.RevocationEvent{
		TenantID:       req.TenantId,
		CallerIdentity: observability.CallerIdentityFromContext(ctx),
		TokenID:        "",
		Target:         req.UserId,
		Scope:          audit.RevocationScopeUser,
		OccurredAt:     time.Now().UTC(),
	}
	if err := h.auditStore.RecordRevocation(ctx, event); err != nil {
		h.logger.Error(ctx, "failed to record revocation audit", "error", err)
		span.RecordError(err)
		span.SetStatus(observability.StatusError, err.Error())
		return nil, status.Error(codes.Internal, "audit record failed")
	}

	span.SetStatus(observability.StatusOK, "")
	return &tokenv1.RevokeTokenResponse{}, nil
}

// RevokeAllForUserAndAudience revokes all tokens for the given user and audience combination.
// Gated on audit store availability — returns codes.Unavailable if audit store is unreachable.
func (h *TokenHandler) RevokeAllForUserAndAudience(ctx context.Context, req *tokenv1.RevokeUserAndAudienceRequest) (*tokenv1.RevokeTokenResponse, error) {
	// ===== STEP 1: Audit gate =====
	if err := h.auditStore.Ping(ctx); err != nil {
		h.logger.Warn(ctx, "audit store unavailable", "error", err)
		return nil, status.Error(codes.Unavailable, "audit store unavailable")
	}

	// ===== STEP 2: Open span =====
	ctx, span := h.tracer.Start(ctx, spanNameRevokeAllForUserAndAudience)
	defer span.End()

	// ===== STEP 3: Resolve tenant =====
	tenantID := req.TenantId
	manager, err := h.registry.Get(ctx, tenantID)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(observability.StatusError, err.Error())
		return nil, err
	}

	// ===== STEP 4: Revoke =====
	if err := manager.RevokeAllForUserAndAudience(ctx, req.UserId, req.Audience); err != nil {
		mapped := observability.MapLibraryError(err)
		span.RecordError(mapped)
		span.SetStatus(observability.StatusError, mapped.Error())
		return nil, mapped
	}

	// ===== STEP 5: Record audit event =====
	event := audit.RevocationEvent{
		TenantID: tenantID,
		Target:   req.UserId,
		Scope:    audit.RevocationScopeUser,
	}
	if err := h.auditStore.RecordRevocation(ctx, event); err != nil {
		h.logger.Error(ctx, "failed to record revocation audit", "error", err)
		span.RecordError(err)
		span.SetStatus(observability.StatusError, err.Error())
		return nil, status.Error(codes.Internal, "audit record failed")
	}

	// ===== STEP 6: Success =====
	span.SetStatus(observability.StatusOK, "")
	return &tokenv1.RevokeTokenResponse{}, nil
}
