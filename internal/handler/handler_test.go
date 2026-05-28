package handler_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"time"

	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"github.com/aetomala/token-engine/internal/audit"
	"github.com/aetomala/token-engine/internal/handler"
	obs "github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/testutil"
	"github.com/aetomala/jwtauth/pkg/keys"
	"github.com/aetomala/jwtauth/pkg/storage"
	"github.com/aetomala/jwtauth/pkg/tokens"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// buildRealManager creates a started tokens.Manager backed by an in-memory store
// and the given MockKeyManager. It sets mock expectations for the Start and
// Shutdown lifecycle calls that tokens.Manager delegates to the KeyManager.
// Shutdown is registered via DeferCleanup.
func buildRealManager(ctrl *gomock.Controller, mockKM *testutil.MockKeyManager) *tokens.Manager {
	memStore := storage.NewMemoryRefreshStore(storage.MemoryRefreshStoreConfig{})
	manager, err := tokens.NewManager(tokens.TokenManagerConfig{
		KeyManager:   mockKM,
		RefreshStore: memStore,
		Namespace:    "test",
		Issuer:       "test-issuer",
		Audience:     []string{"api"},
	})
	Expect(err).NotTo(HaveOccurred())
	mockKM.EXPECT().Start(gomock.Any()).Return(nil)
	mockKM.EXPECT().Shutdown(gomock.Any()).Return(nil).AnyTimes()
	Expect(manager.Start(context.Background())).NotTo(HaveOccurred())
	DeferCleanup(func() { _ = manager.Shutdown(context.Background()) })
	return manager
}

// ===== Phase 3: IssueToken =====

var _ = Describe("Phase 3: TokenHandler — IssueToken", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		ctrl       *gomock.Controller
		mockReg    *testutil.MockTenantRegistry
		mockLogger *testutil.MockLogger
		privateKey *rsa.PrivateKey
		h          *handler.TokenHandler
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockReg = testutil.NewMockTenantRegistry(ctrl)
		mockLogger = testutil.NewMockLogger(ctrl)

		var err error
		privateKey, err = rsa.GenerateKey(rand.Reader, 2048)
		Expect(err).NotTo(HaveOccurred())

		h = handler.NewTokenHandler(mockReg, audit.NewNoOpAuditStore(), mockLogger, obs.NewNoOpTracer(), obs.NewNoOpMetrics())
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	Context("when TenantRegistry.Get returns the Manager", func() {
		It("calls IssueTokenPairWithClaims and returns access_token and refresh_token", func() {
			mockKM := testutil.NewMockKeyManager(ctrl)
			mockKM.EXPECT().GetCurrentSigningKey(gomock.Any()).Return(privateKey, "key-1", nil).AnyTimes()
			manager := buildRealManager(ctrl, mockKM)

			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(manager, nil)

			req := &tokenv1.IssueTokenRequest{Sub: "user1", TenantId: "test-tenant"}
			resp, err := h.IssueToken(ctx, req)

			Expect(err).To(BeNil())
			Expect(resp).NotTo(BeNil())
			Expect(resp.AccessToken).NotTo(BeEmpty())
			Expect(resp.RefreshToken).NotTo(BeEmpty())
		})

		It("passes WithAudience option when req.Audiences is non-empty", func() {
			mockKM := testutil.NewMockKeyManager(ctrl)
			mockKM.EXPECT().GetCurrentSigningKey(gomock.Any()).Return(privateKey, "key-1", nil).AnyTimes()
			manager := buildRealManager(ctrl, mockKM)

			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(manager, nil)

			req := &tokenv1.IssueTokenRequest{
				Sub:       "user1",
				TenantId:  "test-tenant",
				Audiences: []string{"api", "admin"},
			}
			resp, err := h.IssueToken(ctx, req)

			Expect(err).To(BeNil())
			Expect(resp.AccessToken).NotTo(BeEmpty())
		})

		It("passes no WithAudience option when req.Audiences is empty", func() {
			mockKM := testutil.NewMockKeyManager(ctrl)
			mockKM.EXPECT().GetCurrentSigningKey(gomock.Any()).Return(privateKey, "key-1", nil).AnyTimes()
			manager := buildRealManager(ctrl, mockKM)

			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(manager, nil)

			req := &tokenv1.IssueTokenRequest{
				Sub:       "user1",
				TenantId:  "test-tenant",
				Audiences: []string{},
			}
			resp, err := h.IssueToken(ctx, req)

			Expect(err).To(BeNil())
			Expect(resp.AccessToken).NotTo(BeEmpty())
		})

		It("converts req.Claims map[string]string to tokens.CustomClaims", func() {
			mockKM := testutil.NewMockKeyManager(ctrl)
			mockKM.EXPECT().GetCurrentSigningKey(gomock.Any()).Return(privateKey, "key-1", nil).AnyTimes()
			manager := buildRealManager(ctrl, mockKM)

			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(manager, nil)

			req := &tokenv1.IssueTokenRequest{
				Sub:      "user1",
				TenantId: "test-tenant",
				Claims:   map[string]string{"role": "admin", "org": "acme"},
			}
			resp, err := h.IssueToken(ctx, req)

			Expect(err).To(BeNil())
			Expect(resp.AccessToken).NotTo(BeEmpty())
		})
	})

	Context("when TenantRegistry.Get returns codes.NotFound", func() {
		It("returns codes.NotFound without calling the library", func() {
			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(nil, status.Error(codes.NotFound, "tenant not found"))

			req := &tokenv1.IssueTokenRequest{Sub: "user1", TenantId: "test-tenant"}
			_, err := h.IssueToken(ctx, req)

			Expect(err).NotTo(BeNil())
			Expect(status.Code(err)).To(Equal(codes.NotFound))
		})
	})

	Context("when TenantRegistry.Get returns codes.InvalidArgument", func() {
		It("returns codes.InvalidArgument without calling the library", func() {
			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(nil, status.Error(codes.InvalidArgument, "invalid tenant_id"))

			req := &tokenv1.IssueTokenRequest{Sub: "user1", TenantId: "test-tenant"}
			_, err := h.IssueToken(ctx, req)

			Expect(err).NotTo(BeNil())
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
		})
	})

	Context("when IssueTokenPairWithClaims returns a library error", func() {
		It("maps the error via MapLibraryError and returns the mapped gRPC status", func() {
			mockKM := testutil.NewMockKeyManager(ctrl)
			// GetCurrentSigningKey fails — causes IssueTokenPairWithClaims to fail
			mockKM.EXPECT().GetCurrentSigningKey(gomock.Any()).Return(nil, "", keys.ErrManagerNotRunning).AnyTimes()
			manager := buildRealManager(ctrl, mockKM)

			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(manager, nil)

			req := &tokenv1.IssueTokenRequest{Sub: "user1", TenantId: "test-tenant"}
			_, err := h.IssueToken(ctx, req)

			Expect(err).NotTo(BeNil())
			// ErrManagerNotRunning is not a mapped sentinel → codes.Internal
			Expect(status.Code(err)).To(Equal(codes.Internal))
		})
	})
})

// ===== Phase 3: RefreshToken =====

var _ = Describe("Phase 3: TokenHandler — RefreshToken", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		ctrl       *gomock.Controller
		mockReg    *testutil.MockTenantRegistry
		mockLogger *testutil.MockLogger
		privateKey *rsa.PrivateKey
		h          *handler.TokenHandler
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockReg = testutil.NewMockTenantRegistry(ctrl)
		mockLogger = testutil.NewMockLogger(ctrl)

		var err error
		privateKey, err = rsa.GenerateKey(rand.Reader, 2048)
		Expect(err).NotTo(HaveOccurred())

		h = handler.NewTokenHandler(mockReg, audit.NewNoOpAuditStore(), mockLogger, obs.NewNoOpTracer(), obs.NewNoOpMetrics())
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	Context("when TenantRegistry.Get returns the Manager and token is valid", func() {
		It("calls RefreshAccessTokenWithClaims and returns access_token", func() {
			mockKM := testutil.NewMockKeyManager(ctrl)
			mockKM.EXPECT().GetCurrentSigningKey(gomock.Any()).Return(privateKey, "key-1", nil).AnyTimes()
			manager := buildRealManager(ctrl, mockKM)

			// Issue a real token pair first
			issueReq := &tokenv1.IssueTokenRequest{Sub: "user1", TenantId: "test-tenant"}
			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(manager, nil)
			issueResp, err := h.IssueToken(ctx, issueReq)
			Expect(err).To(BeNil())
			Expect(issueResp.RefreshToken).NotTo(BeEmpty())

			// Now refresh
			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(manager, nil)
			refreshReq := &tokenv1.RefreshTokenRequest{
				RefreshToken: issueResp.RefreshToken,
				TenantId:     "test-tenant",
			}
			resp, err := h.RefreshToken(ctx, refreshReq)

			Expect(err).To(BeNil())
			Expect(resp).NotTo(BeNil())
			Expect(resp.AccessToken).NotTo(BeEmpty())
		})
	})

	Context("when refresh token is revoked", func() {
		It("returns codes.PermissionDenied", func() {
			mockKM := testutil.NewMockKeyManager(ctrl)
			mockKM.EXPECT().GetCurrentSigningKey(gomock.Any()).Return(privateKey, "key-1", nil).AnyTimes()
			mockKM.EXPECT().Start(gomock.Any()).Return(nil)
			mockKM.EXPECT().Shutdown(gomock.Any()).Return(nil).AnyTimes()
			memStore := storage.NewMemoryRefreshStore(storage.MemoryRefreshStoreConfig{})
			manager, err := tokens.NewManager(tokens.TokenManagerConfig{
				KeyManager:   mockKM,
				RefreshStore: memStore,
				Namespace:    "test",
			})
			Expect(err).NotTo(HaveOccurred())
			Expect(manager.Start(context.Background())).NotTo(HaveOccurred())
			DeferCleanup(func() { _ = manager.Shutdown(context.Background()) })

			// Issue a token pair to get a valid refresh token
			issueReq := &tokenv1.IssueTokenRequest{Sub: "user1", TenantId: "test-tenant"}
			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(manager, nil)
			issueResp, err := h.IssueToken(ctx, issueReq)
			Expect(err).To(BeNil())
			refreshToken := issueResp.RefreshToken

			// Revoke the refresh token via the manager
			revokeErr := manager.RevokeRefreshToken(ctx, refreshToken)
			Expect(revokeErr).To(BeNil())

			// Now refresh — should get PermissionDenied
			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(manager, nil)
			refreshReq := &tokenv1.RefreshTokenRequest{
				RefreshToken: refreshToken,
				TenantId:     "test-tenant",
			}
			_, err = h.RefreshToken(ctx, refreshReq)

			Expect(err).NotTo(BeNil())
			Expect(status.Code(err)).To(Equal(codes.PermissionDenied))
		})
	})

	Context("when refresh token is not in the store", func() {
		It("returns a non-OK status", func() {
			mockKM := testutil.NewMockKeyManager(ctrl)
			mockKM.EXPECT().GetCurrentSigningKey(gomock.Any()).Return(privateKey, "key-1", nil).AnyTimes()
			manager := buildRealManager(ctrl, mockKM)

			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(manager, nil)

			refreshReq := &tokenv1.RefreshTokenRequest{
				RefreshToken: "non-existent-token-id",
				TenantId:     "test-tenant",
			}
			_, err := h.RefreshToken(ctx, refreshReq)

			Expect(err).NotTo(BeNil())
			// The manager maps unknown tokens to ErrInvalidRefreshToken → codes.Internal
			Expect(status.Code(err)).NotTo(Equal(codes.OK))
		})
	})

	Context("when TenantRegistry.Get returns an error", func() {
		It("returns the registry error without calling the library", func() {
			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(nil, status.Error(codes.Unauthenticated, "token expired"))

			refreshReq := &tokenv1.RefreshTokenRequest{
				RefreshToken: "some-token",
				TenantId:     "test-tenant",
			}
			_, err := h.RefreshToken(ctx, refreshReq)

			Expect(err).NotTo(BeNil())
			Expect(status.Code(err)).To(Equal(codes.Unauthenticated))
		})
	})
})

// ===== Phase 4: Observability =====

var _ = Describe("Phase 4: TokenHandler — observability", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		ctrl       *gomock.Controller
		mockReg    *testutil.MockTenantRegistry
		mockTracer *testutil.MockTracer
		mockSpan   *testutil.MockSpan
		h          *handler.TokenHandler
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockReg = testutil.NewMockTenantRegistry(ctrl)
		mockTracer = testutil.NewMockTracer(ctrl)
		mockSpan = testutil.NewMockSpan(ctrl)
		h = handler.NewTokenHandler(mockReg, audit.NewNoOpAuditStore(), obs.NewNoOpLogger(), mockTracer, obs.NewNoOpMetrics())
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	Context("IssueToken — span lifecycle", func() {
		It("opens and ends a span named 'IssueToken'", func() {
			privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
			Expect(err).NotTo(HaveOccurred())

			innerCtrl := gomock.NewController(GinkgoT())
			defer innerCtrl.Finish()
			mockKM := testutil.NewMockKeyManager(innerCtrl)
			mockKM.EXPECT().GetCurrentSigningKey(gomock.Any()).Return(privateKey, "key-1", nil).AnyTimes()
			manager := buildRealManager(innerCtrl, mockKM)

			mockTracer.EXPECT().Start(gomock.Any(), "IssueToken").Return(ctx, mockSpan)
			mockSpan.EXPECT().SetStatus(obs.StatusOK, "")
			mockSpan.EXPECT().End()

			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(manager, nil)

			req := &tokenv1.IssueTokenRequest{Sub: "user1", TenantId: "test-tenant"}
			_, err = h.IssueToken(ctx, req)
			Expect(err).To(BeNil())
		})

		It("records error on span when handler returns non-nil error", func() {
			mockTracer.EXPECT().Start(gomock.Any(), "IssueToken").Return(ctx, mockSpan)
			mockSpan.EXPECT().RecordError(gomock.Any())
			mockSpan.EXPECT().SetStatus(obs.StatusError, gomock.Any())
			mockSpan.EXPECT().End()

			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(nil, status.Error(codes.NotFound, "not found"))

			req := &tokenv1.IssueTokenRequest{Sub: "user1", TenantId: "test-tenant"}
			_, err := h.IssueToken(ctx, req)
			Expect(err).NotTo(BeNil())
		})
	})

	Context("RefreshToken — span lifecycle", func() {
		It("opens and ends a span named 'RefreshToken'", func() {
			privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
			Expect(err).NotTo(HaveOccurred())

			innerCtrl := gomock.NewController(GinkgoT())
			defer innerCtrl.Finish()
			mockKM := testutil.NewMockKeyManager(innerCtrl)
			mockKM.EXPECT().GetCurrentSigningKey(gomock.Any()).Return(privateKey, "key-1", nil).AnyTimes()
			// Need to issue a token first using a handler with NoOpTracer
			hNoOp := handler.NewTokenHandler(mockReg, audit.NewNoOpAuditStore(), obs.NewNoOpLogger(), obs.NewNoOpTracer(), obs.NewNoOpMetrics())
			manager := buildRealManager(innerCtrl, mockKM)

			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(manager, nil)
			issueResp, err := hNoOp.IssueToken(ctx, &tokenv1.IssueTokenRequest{Sub: "user1", TenantId: "test-tenant"})
			Expect(err).To(BeNil())

			// Now use the MockTracer handler for the refresh
			mockTracer.EXPECT().Start(gomock.Any(), "RefreshToken").Return(ctx, mockSpan)
			mockSpan.EXPECT().SetStatus(obs.StatusOK, "")
			mockSpan.EXPECT().End()

			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(manager, nil)
			_, err = h.RefreshToken(ctx, &tokenv1.RefreshTokenRequest{
				RefreshToken: issueResp.RefreshToken,
				TenantId:     "test-tenant",
			})
			Expect(err).To(BeNil())
		})

		It("records error on span when handler returns non-nil error", func() {
			mockTracer.EXPECT().Start(gomock.Any(), "RefreshToken").Return(ctx, mockSpan)
			mockSpan.EXPECT().RecordError(gomock.Any())
			mockSpan.EXPECT().SetStatus(obs.StatusError, gomock.Any())
			mockSpan.EXPECT().End()

			mockReg.EXPECT().Get(gomock.Any(), "test-tenant").Return(nil, status.Error(codes.Unauthenticated, "expired"))

			_, err := h.RefreshToken(ctx, &tokenv1.RefreshTokenRequest{
				RefreshToken: "some-token",
				TenantId:     "test-tenant",
			})
			Expect(err).NotTo(BeNil())
		})
	})
})

// ===== Phase 3: Unimplemented RPCs =====

var _ = Describe("Phase 3: TokenHandler — unimplemented RPCs", func() {
	var (
		ctx  context.Context
		cancel context.CancelFunc
		ctrl *gomock.Controller
		h    *handler.TokenHandler
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockReg := testutil.NewMockTenantRegistry(ctrl)
		h = handler.NewTokenHandler(mockReg, audit.NewNoOpAuditStore(), obs.NewNoOpLogger(), obs.NewNoOpTracer(), obs.NewNoOpMetrics())
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	Context("RevokeToken", func() {
		It("returns codes.Unimplemented", func() {
			_, err := h.RevokeToken(ctx, &tokenv1.RevokeTokenRequest{})
			Expect(err).NotTo(BeNil())
			Expect(status.Code(err)).To(Equal(codes.Unimplemented))
		})
	})

	Context("RevokeAllForAudience", func() {
		It("returns codes.Unimplemented", func() {
			_, err := h.RevokeAllForAudience(ctx, &tokenv1.RevokeAudienceRequest{})
			Expect(err).NotTo(BeNil())
			Expect(status.Code(err)).To(Equal(codes.Unimplemented))
		})
	})

	Context("RevokeAllUserTokens", func() {
		It("returns codes.Unimplemented", func() {
			_, err := h.RevokeAllUserTokens(ctx, &tokenv1.RevokeUserRequest{})
			Expect(err).NotTo(BeNil())
			Expect(status.Code(err)).To(Equal(codes.Unimplemented))
		})
	})
})
