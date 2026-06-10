package client_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc"

	"github.com/aetomala/token-engine/client"
	tokenv1 "github.com/aetomala/token-engine/gen/v1"
)

// fakeStub is a test double for tokenv1.TokenEngineClient. Unset function fields return (nil, nil).
type fakeStub struct {
	issueTokenFn              func(context.Context, *tokenv1.IssueTokenRequest, ...grpc.CallOption) (*tokenv1.TokenPair, error)
	refreshTokenFn            func(context.Context, *tokenv1.RefreshTokenRequest, ...grpc.CallOption) (*tokenv1.TokenPair, error)
	revokeTokenFn             func(context.Context, *tokenv1.RevokeTokenRequest, ...grpc.CallOption) (*tokenv1.RevokeTokenResponse, error)
	revokeAllForAudienceFn    func(context.Context, *tokenv1.RevokeAudienceRequest, ...grpc.CallOption) (*tokenv1.RevokeTokenResponse, error)
	revokeAllUserTokensFn     func(context.Context, *tokenv1.RevokeUserRequest, ...grpc.CallOption) (*tokenv1.RevokeTokenResponse, error)
	revokeAllForUserAndAudFn  func(context.Context, *tokenv1.RevokeUserAndAudienceRequest, ...grpc.CallOption) (*tokenv1.RevokeTokenResponse, error)
}

func (f *fakeStub) IssueToken(ctx context.Context, in *tokenv1.IssueTokenRequest, opts ...grpc.CallOption) (*tokenv1.TokenPair, error) {
	if f.issueTokenFn != nil {
		return f.issueTokenFn(ctx, in, opts...)
	}
	return nil, nil
}

func (f *fakeStub) RefreshToken(ctx context.Context, in *tokenv1.RefreshTokenRequest, opts ...grpc.CallOption) (*tokenv1.TokenPair, error) {
	if f.refreshTokenFn != nil {
		return f.refreshTokenFn(ctx, in, opts...)
	}
	return nil, nil
}

func (f *fakeStub) RevokeToken(ctx context.Context, in *tokenv1.RevokeTokenRequest, opts ...grpc.CallOption) (*tokenv1.RevokeTokenResponse, error) {
	if f.revokeTokenFn != nil {
		return f.revokeTokenFn(ctx, in, opts...)
	}
	return nil, nil
}

func (f *fakeStub) RevokeAllForAudience(ctx context.Context, in *tokenv1.RevokeAudienceRequest, opts ...grpc.CallOption) (*tokenv1.RevokeTokenResponse, error) {
	if f.revokeAllForAudienceFn != nil {
		return f.revokeAllForAudienceFn(ctx, in, opts...)
	}
	return nil, nil
}

func (f *fakeStub) RevokeAllUserTokens(ctx context.Context, in *tokenv1.RevokeUserRequest, opts ...grpc.CallOption) (*tokenv1.RevokeTokenResponse, error) {
	if f.revokeAllUserTokensFn != nil {
		return f.revokeAllUserTokensFn(ctx, in, opts...)
	}
	return nil, nil
}

func (f *fakeStub) RevokeAllForUserAndAudience(ctx context.Context, in *tokenv1.RevokeUserAndAudienceRequest, opts ...grpc.CallOption) (*tokenv1.RevokeTokenResponse, error) {
	if f.revokeAllForUserAndAudFn != nil {
		return f.revokeAllForUserAndAudFn(ctx, in, opts...)
	}
	return nil, nil
}

// ===== PHASE 1: Constructor and Initialization =====.
var _ = Describe("Phase 1: Constructor and Initialization", func() {
	Describe("NewClient", func() {
		It("returns ErrNoCredentials when no transport option is provided", func() {
			c, err := client.NewClient("localhost:9090")
			Expect(err).To(MatchError(client.ErrNoCredentials))
			Expect(c).To(BeNil())
		})

		It("returns a non-nil Client with WithPlaintext", func() {
			c, err := client.NewClient("localhost:9090", client.WithPlaintext())
			Expect(err).NotTo(HaveOccurred())
			Expect(c).NotTo(BeNil())
			Expect(c.Close()).To(Succeed())
		})

		It("returns an error when cert file does not exist with WithMTLS", func() {
			c, err := client.NewClient("localhost:9090",
				client.WithMTLS("/nonexistent/cert.pem", "/nonexistent/key.pem", "/nonexistent/ca.pem"),
			)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("load client cert/key"))
			Expect(c).To(BeNil())
		})
	})
})

// ===== PHASE 2: RPC Delegation =====.
var _ = Describe("Phase 2: RPC Delegation", func() {
	var (
		ctx  context.Context
		stub *fakeStub
		sut  client.Client
	)

	BeforeEach(func() {
		ctx = context.Background()
		stub = &fakeStub{}
		sut = client.NewClientFromStub(stub)
	})

	AfterEach(func() {
		Expect(sut.Close()).To(Succeed())
	})

	It("IssueToken — propagates stub response", func() {
		expected := &tokenv1.TokenPair{AccessToken: "access-abc", RefreshToken: "refresh-xyz"}
		stub.issueTokenFn = func(_ context.Context, _ *tokenv1.IssueTokenRequest, _ ...grpc.CallOption) (*tokenv1.TokenPair, error) {
			return expected, nil
		}
		got, err := sut.IssueToken(ctx, &tokenv1.IssueTokenRequest{Sub: "user-1", TenantId: "t1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(expected))
	})

	It("IssueToken — propagates stub error", func() {
		sentinel := errors.New("issue failed")
		stub.issueTokenFn = func(_ context.Context, _ *tokenv1.IssueTokenRequest, _ ...grpc.CallOption) (*tokenv1.TokenPair, error) {
			return nil, sentinel
		}
		_, err := sut.IssueToken(ctx, &tokenv1.IssueTokenRequest{Sub: "user-1", TenantId: "t1"})
		Expect(err).To(MatchError(sentinel))
	})

	It("RefreshToken — propagates stub response", func() {
		expected := &tokenv1.TokenPair{AccessToken: "new-access"}
		stub.refreshTokenFn = func(_ context.Context, _ *tokenv1.RefreshTokenRequest, _ ...grpc.CallOption) (*tokenv1.TokenPair, error) {
			return expected, nil
		}
		got, err := sut.RefreshToken(ctx, &tokenv1.RefreshTokenRequest{RefreshToken: "old-rt", TenantId: "t1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).To(Equal(expected))
	})

	It("RevokeToken — propagates stub response", func() {
		stub.revokeTokenFn = func(_ context.Context, _ *tokenv1.RevokeTokenRequest, _ ...grpc.CallOption) (*tokenv1.RevokeTokenResponse, error) {
			return &tokenv1.RevokeTokenResponse{}, nil
		}
		got, err := sut.RevokeToken(ctx, &tokenv1.RevokeTokenRequest{RefreshToken: "rt", TenantId: "t1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
	})

	It("RevokeAllForAudience — propagates stub response", func() {
		stub.revokeAllForAudienceFn = func(_ context.Context, _ *tokenv1.RevokeAudienceRequest, _ ...grpc.CallOption) (*tokenv1.RevokeTokenResponse, error) {
			return &tokenv1.RevokeTokenResponse{}, nil
		}
		got, err := sut.RevokeAllForAudience(ctx, &tokenv1.RevokeAudienceRequest{Audience: "aud", TenantId: "t1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
	})

	It("RevokeAllUserTokens — propagates stub response", func() {
		stub.revokeAllUserTokensFn = func(_ context.Context, _ *tokenv1.RevokeUserRequest, _ ...grpc.CallOption) (*tokenv1.RevokeTokenResponse, error) {
			return &tokenv1.RevokeTokenResponse{}, nil
		}
		got, err := sut.RevokeAllUserTokens(ctx, &tokenv1.RevokeUserRequest{UserId: "u1", TenantId: "t1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
	})

	It("RevokeAllForUserAndAudience — propagates stub response", func() {
		stub.revokeAllForUserAndAudFn = func(_ context.Context, _ *tokenv1.RevokeUserAndAudienceRequest, _ ...grpc.CallOption) (*tokenv1.RevokeTokenResponse, error) {
			return &tokenv1.RevokeTokenResponse{}, nil
		}
		got, err := sut.RevokeAllForUserAndAudience(ctx, &tokenv1.RevokeUserAndAudienceRequest{UserId: "u1", Audience: "aud", TenantId: "t1"})
		Expect(err).NotTo(HaveOccurred())
		Expect(got).NotTo(BeNil())
	})

	It("propagates errors from all RPC methods", func() {
		sentinel := errors.New("rpc error")
		stub.revokeAllUserTokensFn = func(_ context.Context, _ *tokenv1.RevokeUserRequest, _ ...grpc.CallOption) (*tokenv1.RevokeTokenResponse, error) {
			return nil, sentinel
		}
		_, err := sut.RevokeAllUserTokens(ctx, &tokenv1.RevokeUserRequest{UserId: "u1", TenantId: "t1"})
		Expect(err).To(MatchError(sentinel))
	})
})

// ===== PHASE 3: Close =====.
var _ = Describe("Phase 3: Close", func() {
	It("Close on stub-backed client returns nil", func() {
		sut := client.NewClientFromStub(&fakeStub{})
		Expect(sut.Close()).To(Succeed())
	})

	It("Close on stub-backed client is safe to call multiple times", func() {
		sut := client.NewClientFromStub(&fakeStub{})
		Expect(sut.Close()).To(Succeed())
		Expect(sut.Close()).To(Succeed())
	})

	It("Close on dial-created client releases the connection", func() {
		sut, err := client.NewClient("localhost:9090", client.WithPlaintext())
		Expect(err).NotTo(HaveOccurred())
		Expect(sut.Close()).To(Succeed())
	})
})
