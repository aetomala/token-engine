package client

import (
	"context"

	tokenv1 "github.com/aetomala/token-engine/gen/v1"
)

// NoOpClient is a no-op implementation of Client. All RPC methods return (nil, nil) and Close
// returns nil. It is suitable for use in tests that depend on the Client interface but do not
// require real token-engine communication.
type NoOpClient struct{}

// NewNoOpClient returns a new NoOpClient.
func NewNoOpClient() *NoOpClient { return &NoOpClient{} }

// IssueToken is a no-op — returns (nil, nil).
func (n *NoOpClient) IssueToken(_ context.Context, _ *tokenv1.IssueTokenRequest) (*tokenv1.TokenPair, error) {
	return nil, nil
}

// RefreshToken is a no-op — returns (nil, nil).
func (n *NoOpClient) RefreshToken(_ context.Context, _ *tokenv1.RefreshTokenRequest) (*tokenv1.TokenPair, error) {
	return nil, nil
}

// RevokeToken is a no-op — returns (nil, nil).
func (n *NoOpClient) RevokeToken(_ context.Context, _ *tokenv1.RevokeTokenRequest) (*tokenv1.RevokeTokenResponse, error) {
	return nil, nil
}

// RevokeAllForAudience is a no-op — returns (nil, nil).
func (n *NoOpClient) RevokeAllForAudience(_ context.Context, _ *tokenv1.RevokeAudienceRequest) (*tokenv1.RevokeTokenResponse, error) {
	return nil, nil
}

// RevokeAllUserTokens is a no-op — returns (nil, nil).
func (n *NoOpClient) RevokeAllUserTokens(_ context.Context, _ *tokenv1.RevokeUserRequest) (*tokenv1.RevokeTokenResponse, error) {
	return nil, nil
}

// RevokeAllForUserAndAudience is a no-op — returns (nil, nil).
func (n *NoOpClient) RevokeAllForUserAndAudience(_ context.Context, _ *tokenv1.RevokeUserAndAudienceRequest) (*tokenv1.RevokeTokenResponse, error) {
	return nil, nil
}

// Close is a no-op — returns nil.
func (n *NoOpClient) Close() error { return nil }
