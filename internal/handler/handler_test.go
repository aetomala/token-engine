package handler_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http"
	"net/http/httptest"
	"time"

	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"github.com/aetomala/token-engine/internal/audit"
	"github.com/aetomala/token-engine/internal/config"
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

var _ = Describe("TokenHandler", func() {
// ===== PHASE 3: IssueToken =====
Describe("Phase 3: IssueToken", func() {
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

// ===== PHASE 3: RefreshToken =====
Describe("Phase 3: RefreshToken", func() {
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
		It("calls RefreshAccessTokenWithClaims and returns a full token pair", func() {
			mockKM := testutil.NewMockKeyManager(ctrl)
			mockKM.EXPECT().GetCurrentSigningKey(gomock.Any()).Return(privateKey, "key-1", nil).AnyTimes()
			mockKM.EXPECT().GetPublicKey(gomock.Any(), "key-1").Return(&privateKey.PublicKey, nil).AnyTimes()
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
			Expect(resp.RefreshToken).NotTo(BeEmpty())
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

// ===== PHASE 4: Observability =====
Describe("Phase 4: Observability", func() {
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
			mockKM.EXPECT().GetPublicKey(gomock.Any(), "key-1").Return(&privateKey.PublicKey, nil).AnyTimes()
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

// ===== PHASE 3: RevokeToken =====
Describe("Phase 3: RevokeToken", func() {
	var (
		ctx            context.Context
		cancel         context.CancelFunc
		ctrl           *gomock.Controller
		mockReg        *testutil.MockTenantRegistry
		mockAuditStore *testutil.MockStore
		mockTM         *testutil.MockTokenManager
		h              *handler.TokenHandler
		req            *tokenv1.RevokeTokenRequest
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockReg = testutil.NewMockTenantRegistry(ctrl)
		mockAuditStore = testutil.NewMockStore(ctrl)
		mockTM = testutil.NewMockTokenManager(ctrl)
		h = handler.NewTokenHandler(mockReg, mockAuditStore, obs.NewNoOpLogger(), obs.NewNoOpTracer(), obs.NewNoOpMetrics())
		req = &tokenv1.RevokeTokenRequest{TenantId: "tenant-1", RefreshToken: "raw-token"}
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	Context("when audit store Ping fails", func() {
		It("returns codes.Unavailable without calling the Manager", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(errors.New("store unavailable"))
			resp, err := h.RevokeToken(ctx, req)
			Expect(resp).To(BeNil())
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
		})

		It("does not call RecordRevocation", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(errors.New("store unavailable"))
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Times(0)
			_, err := h.RevokeToken(ctx, req)
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
		})
	})

	Context("when IntrospectToken fails", func() {
		It("maps the error via MapLibraryError", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().IntrospectToken(gomock.Any(), req.RefreshToken).Return(nil, errors.New("introspect failed"))
			resp, err := h.RevokeToken(ctx, req)
			Expect(resp).To(BeNil())
			Expect(err).NotTo(BeNil())
		})

		It("does not call RevokeRefreshToken", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().IntrospectToken(gomock.Any(), req.RefreshToken).Return(nil, errors.New("introspect failed"))
			mockTM.EXPECT().RevokeRefreshToken(gomock.Any(), gomock.Any()).Times(0)
			_, err := h.RevokeToken(ctx, req)
			Expect(err).NotTo(BeNil())
		})

		It("does not call RecordRevocation", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().IntrospectToken(gomock.Any(), req.RefreshToken).Return(nil, errors.New("introspect failed"))
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Times(0)
			_, err := h.RevokeToken(ctx, req)
			Expect(err).NotTo(BeNil())
		})
	})

	Context("when RevokeRefreshToken succeeds", func() {
		It("calls RecordRevocation with Scope=token and TokenID from IntrospectToken", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().IntrospectToken(gomock.Any(), req.RefreshToken).Return(&tokens.TokenMetadata{TokenID: "tok-123"}, nil)
			mockTM.EXPECT().RevokeRefreshToken(gomock.Any(), "tok-123").Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Cond(func(v interface{}) bool {
				e, ok := v.(audit.RevocationEvent)
				return ok && e.Scope == audit.RevocationScopeToken && e.TokenID == "tok-123"
			})).Return(nil)
			resp, err := h.RevokeToken(ctx, req)
			Expect(err).To(BeNil())
			Expect(resp).NotTo(BeNil())
		})

		It("returns empty RevokeTokenResponse and nil error", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().IntrospectToken(gomock.Any(), req.RefreshToken).Return(&tokens.TokenMetadata{TokenID: "tok-123"}, nil)
			mockTM.EXPECT().RevokeRefreshToken(gomock.Any(), "tok-123").Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Return(nil)
			resp, err := h.RevokeToken(ctx, req)
			Expect(err).To(BeNil())
			Expect(resp).To(Equal(&tokenv1.RevokeTokenResponse{}))
		})
	})

	Context("when RevokeRefreshToken fails", func() {
		It("maps the error via MapLibraryError", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().IntrospectToken(gomock.Any(), req.RefreshToken).Return(&tokens.TokenMetadata{TokenID: "tok-123"}, nil)
			mockTM.EXPECT().RevokeRefreshToken(gomock.Any(), "tok-123").Return(storage.ErrTokenNotFound)
			resp, err := h.RevokeToken(ctx, req)
			Expect(resp).To(BeNil())
			Expect(err).NotTo(BeNil())
		})

		It("does not call RecordRevocation", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().IntrospectToken(gomock.Any(), req.RefreshToken).Return(&tokens.TokenMetadata{TokenID: "tok-123"}, nil)
			mockTM.EXPECT().RevokeRefreshToken(gomock.Any(), "tok-123").Return(storage.ErrTokenNotFound)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Times(0)
			_, err := h.RevokeToken(ctx, req)
			Expect(err).NotTo(BeNil())
		})
	})

	Context("when RecordRevocation fails after successful revocation", func() {
		It("returns codes.Internal", func() {
			mockLogger := testutil.NewMockLogger(ctrl)
			hWithLogger := handler.NewTokenHandler(mockReg, mockAuditStore, mockLogger, obs.NewNoOpTracer(), obs.NewNoOpMetrics())
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().IntrospectToken(gomock.Any(), req.RefreshToken).Return(&tokens.TokenMetadata{TokenID: "tok-123"}, nil)
			mockTM.EXPECT().RevokeRefreshToken(gomock.Any(), "tok-123").Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Return(errors.New("db write failed"))
			mockLogger.EXPECT().Error(gomock.Any(), "failed to record revocation audit", "error", gomock.Any())
			resp, err := hWithLogger.RevokeToken(ctx, req)
			Expect(status.Code(err)).To(Equal(codes.Internal))
			Expect(resp).To(BeNil())
		})

		It("logs ERROR", func() {
			mockLogger := testutil.NewMockLogger(ctrl)
			hWithLogger := handler.NewTokenHandler(mockReg, mockAuditStore, mockLogger, obs.NewNoOpTracer(), obs.NewNoOpMetrics())
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().IntrospectToken(gomock.Any(), req.RefreshToken).Return(&tokens.TokenMetadata{TokenID: "tok-123"}, nil)
			mockTM.EXPECT().RevokeRefreshToken(gomock.Any(), "tok-123").Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Return(errors.New("db write failed"))
			mockLogger.EXPECT().Error(gomock.Any(), "failed to record revocation audit", "error", gomock.Any())
			_, _ = hWithLogger.RevokeToken(ctx, req)
			// gomock verifies mockLogger.Error was called via ctrl.Finish() in AfterEach
			Expect(true).To(BeTrue())
		})
	})

	Context("observability", func() {
		It("opens and ends a span named 'RevokeToken'", func() {
			mockTracer := testutil.NewMockTracer(ctrl)
			mockSpan := testutil.NewMockSpan(ctrl)
			hObs := handler.NewTokenHandler(mockReg, mockAuditStore, obs.NewNoOpLogger(), mockTracer, obs.NewNoOpMetrics())
			mockTracer.EXPECT().Start(gomock.Any(), "RevokeToken").Return(ctx, mockSpan)
			mockSpan.EXPECT().SetStatus(obs.StatusOK, "")
			mockSpan.EXPECT().End()
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().IntrospectToken(gomock.Any(), req.RefreshToken).Return(&tokens.TokenMetadata{TokenID: "tok-123"}, nil)
			mockTM.EXPECT().RevokeRefreshToken(gomock.Any(), "tok-123").Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Return(nil)
			_, err := hObs.RevokeToken(ctx, req)
			Expect(err).To(BeNil())
		})

		It("records error on span when handler returns non-nil error", func() {
			mockTracer := testutil.NewMockTracer(ctrl)
			mockSpan := testutil.NewMockSpan(ctrl)
			hObs := handler.NewTokenHandler(mockReg, mockAuditStore, obs.NewNoOpLogger(), mockTracer, obs.NewNoOpMetrics())
			mockTracer.EXPECT().Start(gomock.Any(), "RevokeToken").Return(ctx, mockSpan)
			mockSpan.EXPECT().RecordError(gomock.Any())
			mockSpan.EXPECT().SetStatus(obs.StatusError, gomock.Any())
			mockSpan.EXPECT().End()
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(errors.New("unavailable"))
			_, err := hObs.RevokeToken(ctx, req)
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
		})
	})
})

// ===== PHASE 3: RevokeAllForAudience =====
Describe("Phase 3: RevokeAllForAudience", func() {
	var (
		ctx            context.Context
		cancel         context.CancelFunc
		ctrl           *gomock.Controller
		mockReg        *testutil.MockTenantRegistry
		mockAuditStore *testutil.MockStore
		mockTM         *testutil.MockTokenManager
		h              *handler.TokenHandler
		req            *tokenv1.RevokeAudienceRequest
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockReg = testutil.NewMockTenantRegistry(ctrl)
		mockAuditStore = testutil.NewMockStore(ctrl)
		mockTM = testutil.NewMockTokenManager(ctrl)
		h = handler.NewTokenHandler(mockReg, mockAuditStore, obs.NewNoOpLogger(), obs.NewNoOpTracer(), obs.NewNoOpMetrics())
		req = &tokenv1.RevokeAudienceRequest{TenantId: "tenant-1", Audience: "api"}
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	Context("when audit store Ping fails", func() {
		It("returns codes.Unavailable without calling the Manager", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(errors.New("store unavailable"))
			resp, err := h.RevokeAllForAudience(ctx, req)
			Expect(resp).To(BeNil())
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
		})

		It("does not call RecordRevocation", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(errors.New("store unavailable"))
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Times(0)
			_, err := h.RevokeAllForAudience(ctx, req)
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
		})
	})

	Context("when RevokeAllForAudience succeeds", func() {
		It("calls RecordRevocation with Scope=audience and Target=req.Audience", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllForAudience(gomock.Any(), req.Audience).Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Cond(func(v interface{}) bool {
				e, ok := v.(audit.RevocationEvent)
				return ok && e.Scope == audit.RevocationScopeAudience && e.Target == req.Audience
			})).Return(nil)
			resp, err := h.RevokeAllForAudience(ctx, req)
			Expect(err).To(BeNil())
			Expect(resp).NotTo(BeNil())
		})

		It("returns empty RevokeTokenResponse and nil error", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllForAudience(gomock.Any(), req.Audience).Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Return(nil)
			resp, err := h.RevokeAllForAudience(ctx, req)
			Expect(err).To(BeNil())
			Expect(resp).To(Equal(&tokenv1.RevokeTokenResponse{}))
		})
	})

	Context("when RevokeAllForAudience fails", func() {
		It("maps the error via MapLibraryError", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllForAudience(gomock.Any(), req.Audience).Return(errors.New("revoke failed"))
			resp, err := h.RevokeAllForAudience(ctx, req)
			Expect(resp).To(BeNil())
			Expect(err).NotTo(BeNil())
		})

		It("does not call RecordRevocation", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllForAudience(gomock.Any(), req.Audience).Return(errors.New("revoke failed"))
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Times(0)
			_, err := h.RevokeAllForAudience(ctx, req)
			Expect(err).NotTo(BeNil())
		})
	})

	Context("when RecordRevocation fails after successful revocation", func() {
		It("returns codes.Internal", func() {
			mockLogger := testutil.NewMockLogger(ctrl)
			hWithLogger := handler.NewTokenHandler(mockReg, mockAuditStore, mockLogger, obs.NewNoOpTracer(), obs.NewNoOpMetrics())
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllForAudience(gomock.Any(), req.Audience).Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Return(errors.New("db write failed"))
			mockLogger.EXPECT().Error(gomock.Any(), "failed to record revocation audit", "error", gomock.Any())
			resp, err := hWithLogger.RevokeAllForAudience(ctx, req)
			Expect(status.Code(err)).To(Equal(codes.Internal))
			Expect(resp).To(BeNil())
		})
	})

	Context("observability", func() {
		It("opens and ends a span named 'RevokeAllForAudience'", func() {
			mockTracer := testutil.NewMockTracer(ctrl)
			mockSpan := testutil.NewMockSpan(ctrl)
			hObs := handler.NewTokenHandler(mockReg, mockAuditStore, obs.NewNoOpLogger(), mockTracer, obs.NewNoOpMetrics())
			mockTracer.EXPECT().Start(gomock.Any(), "RevokeAllForAudience").Return(ctx, mockSpan)
			mockSpan.EXPECT().SetStatus(obs.StatusOK, "")
			mockSpan.EXPECT().End()
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllForAudience(gomock.Any(), req.Audience).Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Return(nil)
			_, err := hObs.RevokeAllForAudience(ctx, req)
			Expect(err).To(BeNil())
		})

		It("records error on span when handler returns non-nil error", func() {
			mockTracer := testutil.NewMockTracer(ctrl)
			mockSpan := testutil.NewMockSpan(ctrl)
			hObs := handler.NewTokenHandler(mockReg, mockAuditStore, obs.NewNoOpLogger(), mockTracer, obs.NewNoOpMetrics())
			mockTracer.EXPECT().Start(gomock.Any(), "RevokeAllForAudience").Return(ctx, mockSpan)
			mockSpan.EXPECT().RecordError(gomock.Any())
			mockSpan.EXPECT().SetStatus(obs.StatusError, gomock.Any())
			mockSpan.EXPECT().End()
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(errors.New("unavailable"))
			_, err := hObs.RevokeAllForAudience(ctx, req)
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
		})
	})
})

// ===== PHASE 3: RevokeAllUserTokens =====
Describe("Phase 3: RevokeAllUserTokens", func() {
	var (
		ctx            context.Context
		cancel         context.CancelFunc
		ctrl           *gomock.Controller
		mockReg        *testutil.MockTenantRegistry
		mockAuditStore *testutil.MockStore
		mockTM         *testutil.MockTokenManager
		h              *handler.TokenHandler
		req            *tokenv1.RevokeUserRequest
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockReg = testutil.NewMockTenantRegistry(ctrl)
		mockAuditStore = testutil.NewMockStore(ctrl)
		mockTM = testutil.NewMockTokenManager(ctrl)
		h = handler.NewTokenHandler(mockReg, mockAuditStore, obs.NewNoOpLogger(), obs.NewNoOpTracer(), obs.NewNoOpMetrics())
		req = &tokenv1.RevokeUserRequest{TenantId: "tenant-1", UserId: "user-1"}
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	Context("when audit store Ping fails", func() {
		It("returns codes.Unavailable without calling the Manager", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(errors.New("store unavailable"))
			resp, err := h.RevokeAllUserTokens(ctx, req)
			Expect(resp).To(BeNil())
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
		})

		It("does not call RecordRevocation", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(errors.New("store unavailable"))
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Times(0)
			_, err := h.RevokeAllUserTokens(ctx, req)
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
		})
	})

	Context("when RevokeAllUserTokens succeeds", func() {
		It("calls RecordRevocation with Scope=user and Target=req.UserId", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllUserTokens(gomock.Any(), req.UserId).Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Cond(func(v interface{}) bool {
				e, ok := v.(audit.RevocationEvent)
				return ok && e.Scope == audit.RevocationScopeUser && e.Target == req.UserId
			})).Return(nil)
			resp, err := h.RevokeAllUserTokens(ctx, req)
			Expect(err).To(BeNil())
			Expect(resp).NotTo(BeNil())
		})

		It("returns empty RevokeTokenResponse and nil error", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllUserTokens(gomock.Any(), req.UserId).Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Return(nil)
			resp, err := h.RevokeAllUserTokens(ctx, req)
			Expect(err).To(BeNil())
			Expect(resp).To(Equal(&tokenv1.RevokeTokenResponse{}))
		})
	})

	Context("when RevokeAllUserTokens fails", func() {
		It("maps the error via MapLibraryError", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllUserTokens(gomock.Any(), req.UserId).Return(errors.New("revoke failed"))
			resp, err := h.RevokeAllUserTokens(ctx, req)
			Expect(resp).To(BeNil())
			Expect(err).NotTo(BeNil())
		})

		It("does not call RecordRevocation", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllUserTokens(gomock.Any(), req.UserId).Return(errors.New("revoke failed"))
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Times(0)
			_, err := h.RevokeAllUserTokens(ctx, req)
			Expect(err).NotTo(BeNil())
		})
	})

	Context("when RecordRevocation fails after successful revocation", func() {
		It("returns codes.Internal", func() {
			mockLogger := testutil.NewMockLogger(ctrl)
			hWithLogger := handler.NewTokenHandler(mockReg, mockAuditStore, mockLogger, obs.NewNoOpTracer(), obs.NewNoOpMetrics())
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllUserTokens(gomock.Any(), req.UserId).Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Return(errors.New("db write failed"))
			mockLogger.EXPECT().Error(gomock.Any(), "failed to record revocation audit", "error", gomock.Any())
			resp, err := hWithLogger.RevokeAllUserTokens(ctx, req)
			Expect(status.Code(err)).To(Equal(codes.Internal))
			Expect(resp).To(BeNil())
		})
	})

	Context("observability", func() {
		It("opens and ends a span named 'RevokeAllUserTokens'", func() {
			mockTracer := testutil.NewMockTracer(ctrl)
			mockSpan := testutil.NewMockSpan(ctrl)
			hObs := handler.NewTokenHandler(mockReg, mockAuditStore, obs.NewNoOpLogger(), mockTracer, obs.NewNoOpMetrics())
			mockTracer.EXPECT().Start(gomock.Any(), "RevokeAllUserTokens").Return(ctx, mockSpan)
			mockSpan.EXPECT().SetStatus(obs.StatusOK, "")
			mockSpan.EXPECT().End()
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllUserTokens(gomock.Any(), req.UserId).Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Return(nil)
			_, err := hObs.RevokeAllUserTokens(ctx, req)
			Expect(err).To(BeNil())
		})

		It("records error on span when handler returns non-nil error", func() {
			mockTracer := testutil.NewMockTracer(ctrl)
			mockSpan := testutil.NewMockSpan(ctrl)
			hObs := handler.NewTokenHandler(mockReg, mockAuditStore, obs.NewNoOpLogger(), mockTracer, obs.NewNoOpMetrics())
			mockTracer.EXPECT().Start(gomock.Any(), "RevokeAllUserTokens").Return(ctx, mockSpan)
			mockSpan.EXPECT().RecordError(gomock.Any())
			mockSpan.EXPECT().SetStatus(obs.StatusError, gomock.Any())
			mockSpan.EXPECT().End()
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(errors.New("unavailable"))
			_, err := hObs.RevokeAllUserTokens(ctx, req)
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
		})
	})
})
// ===== PHASE 3: RevokeAllForUserAndAudience =====
Describe("Phase 3: RevokeAllForUserAndAudience handler", func() {
	var (
		ctx            context.Context
		cancel         context.CancelFunc
		ctrl           *gomock.Controller
		mockReg        *testutil.MockTenantRegistry
		mockAuditStore *testutil.MockStore
		mockTM         *testutil.MockTokenManager
		h              *handler.TokenHandler
		req            *tokenv1.RevokeUserAndAudienceRequest
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockReg = testutil.NewMockTenantRegistry(ctrl)
		mockAuditStore = testutil.NewMockStore(ctrl)
		mockTM = testutil.NewMockTokenManager(ctrl)
		h = handler.NewTokenHandler(mockReg, mockAuditStore, obs.NewNoOpLogger(), obs.NewNoOpTracer(), obs.NewNoOpMetrics())
		req = &tokenv1.RevokeUserAndAudienceRequest{TenantId: "tenant-1", UserId: "user-1", Audience: "api"}
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	Context("when audit store is available and revocation succeeds", func() {
		It("calls manager.RevokeAllForUserAndAudience with userId and audience", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllForUserAndAudience(gomock.Any(), req.UserId, req.Audience).Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Return(nil)

			_, err := h.RevokeAllForUserAndAudience(ctx, req)
			Expect(err).To(BeNil())
		})

		It("writes an audit log entry", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllForUserAndAudience(gomock.Any(), req.UserId, req.Audience).Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Cond(func(v interface{}) bool {
				e, ok := v.(audit.RevocationEvent)
				return ok && e.Scope == audit.RevocationScopeUser && e.Target == req.UserId
			})).Return(nil)

			_, err := h.RevokeAllForUserAndAudience(ctx, req)
			Expect(err).To(BeNil())
		})

		It("returns RevokeTokenResponse{} and nil error", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllForUserAndAudience(gomock.Any(), req.UserId, req.Audience).Return(nil)
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Return(nil)

			resp, err := h.RevokeAllForUserAndAudience(ctx, req)
			Expect(err).To(BeNil())
			Expect(resp).To(Equal(&tokenv1.RevokeTokenResponse{}))
		})
	})

	Context("when audit store is unavailable", func() {
		It("returns codes.Unavailable without calling the Manager", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(errors.New("store unavailable"))
			mockTM.EXPECT().RevokeAllForUserAndAudience(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

			resp, err := h.RevokeAllForUserAndAudience(ctx, req)
			Expect(resp).To(BeNil())
			Expect(status.Code(err)).To(Equal(codes.Unavailable))
		})
	})

	Context("when manager.RevokeAllForUserAndAudience returns an error", func() {
		It("returns a mapped gRPC status error", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(mockTM, nil)
			mockTM.EXPECT().RevokeAllForUserAndAudience(gomock.Any(), req.UserId, req.Audience).Return(errors.New("revoke failed"))
			mockAuditStore.EXPECT().RecordRevocation(gomock.Any(), gomock.Any()).Times(0)

			resp, err := h.RevokeAllForUserAndAudience(ctx, req)
			Expect(resp).To(BeNil())
			Expect(err).NotTo(BeNil())
		})
	})

	Context("when tenantID is unknown", func() {
		It("returns codes.NotFound", func() {
			mockAuditStore.EXPECT().Ping(gomock.Any()).Return(nil)
			mockReg.EXPECT().Get(gomock.Any(), req.TenantId).Return(nil, status.Error(codes.NotFound, "tenant not found"))

			resp, err := h.RevokeAllForUserAndAudience(ctx, req)
			Expect(resp).To(BeNil())
			Expect(status.Code(err)).To(Equal(codes.NotFound))
		})
	})
})
}) // TokenHandler

var _ = Describe("JWKSHandler", func() {
// ===== PHASE 3: Core Operations =====
Describe("Phase 3: Core Operations", func() {
	var (
		ctx         context.Context
		cancel      context.CancelFunc
		ctrl        *gomock.Controller
		mockKM      *testutil.MockKeyManager
		mockMetrics *testutil.MockMetrics
		testCfg     *config.Config
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockKM = testutil.NewMockKeyManager(ctrl)
		mockMetrics = testutil.NewMockMetrics(ctrl)
		testCfg = &config.Config{JWKSCacheMaxAge: 5 * time.Minute}
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	Context("when km.GetJWKS returns an error", func() {
		It("returns 503", func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{{}, {}}, nil)
			mockMetrics.EXPECT().SetGauge(obs.MetricJWKSKeyCount, float64(2), gomock.Any())
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(nil, errors.New("manager not running"))
			handlerFn := handler.JWKSHandler(mockKM, "test-tenant", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handlerFn(w, req)
			Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
		})

		It("writes Content-Type: application/json", func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{{}, {}}, nil)
			mockMetrics.EXPECT().SetGauge(obs.MetricJWKSKeyCount, float64(2), gomock.Any())
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(nil, errors.New("manager not running"))
			handlerFn := handler.JWKSHandler(mockKM, "test-tenant", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handlerFn(w, req)
			Expect(w.Header().Get("Content-Type")).To(Equal("application/json"))
		})

		It(`writes body {"error":"key manager unavailable"}`, func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{{}, {}}, nil)
			mockMetrics.EXPECT().SetGauge(obs.MetricJWKSKeyCount, float64(2), gomock.Any())
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(nil, errors.New("manager not running"))
			handlerFn := handler.JWKSHandler(mockKM, "test-tenant", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handlerFn(w, req)
			Expect(w.Body.String()).To(ContainSubstring("key manager unavailable"))
		})

		It("does not write Cache-Control header", func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{{}, {}}, nil)
			mockMetrics.EXPECT().SetGauge(obs.MetricJWKSKeyCount, float64(2), gomock.Any())
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(nil, errors.New("manager not running"))
			handlerFn := handler.JWKSHandler(mockKM, "test-tenant", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handlerFn(w, req)
			Expect(w.Header().Get("Cache-Control")).To(BeEmpty())
		})
	})

	Context("when GetJWKS succeeds but key set is empty", func() {
		It("returns 503", func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{{}}, nil)
			mockMetrics.EXPECT().SetGauge(obs.MetricJWKSKeyCount, float64(1), gomock.Any())
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(&keys.JWKS{Keys: []keys.JWK{}}, nil)
			handlerFn := handler.JWKSHandler(mockKM, "test-tenant", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handlerFn(w, req)
			Expect(w.Code).To(Equal(http.StatusServiceUnavailable))
		})

		It("writes Content-Type: application/json", func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{{}}, nil)
			mockMetrics.EXPECT().SetGauge(obs.MetricJWKSKeyCount, float64(1), gomock.Any())
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(&keys.JWKS{Keys: []keys.JWK{}}, nil)
			handlerFn := handler.JWKSHandler(mockKM, "test-tenant", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handlerFn(w, req)
			Expect(w.Header().Get("Content-Type")).To(Equal("application/json"))
		})

		It(`writes body {"error":"no signing keys available"}`, func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{{}}, nil)
			mockMetrics.EXPECT().SetGauge(obs.MetricJWKSKeyCount, float64(1), gomock.Any())
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(&keys.JWKS{Keys: []keys.JWK{}}, nil)
			handlerFn := handler.JWKSHandler(mockKM, "test-tenant", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handlerFn(w, req)
			Expect(w.Body.String()).To(ContainSubstring("no signing keys available"))
		})

		It("does not write Cache-Control header", func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{{}}, nil)
			mockMetrics.EXPECT().SetGauge(obs.MetricJWKSKeyCount, float64(1), gomock.Any())
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(&keys.JWKS{Keys: []keys.JWK{}}, nil)
			handlerFn := handler.JWKSHandler(mockKM, "test-tenant", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handlerFn(w, req)
			Expect(w.Header().Get("Cache-Control")).To(BeEmpty())
		})
	})

	Context("when GetJWKS succeeds with at least one key", func() {
		It("returns 200", func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{{}}, nil)
			mockMetrics.EXPECT().SetGauge(obs.MetricJWKSKeyCount, float64(1), gomock.Any())
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(&keys.JWKS{Keys: []keys.JWK{{}}}, nil)
			handlerFn := handler.JWKSHandler(mockKM, "test-tenant", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handlerFn(w, req)
			Expect(w.Code).To(Equal(http.StatusOK))
		})

		It("writes Content-Type: application/json", func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{{}}, nil)
			mockMetrics.EXPECT().SetGauge(obs.MetricJWKSKeyCount, float64(1), gomock.Any())
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(&keys.JWKS{Keys: []keys.JWK{{}}}, nil)
			handlerFn := handler.JWKSHandler(mockKM, "test-tenant", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handlerFn(w, req)
			Expect(w.Header().Get("Content-Type")).To(Equal("application/json"))
		})

		It("writes Cache-Control: public, max-age=N derived from cfg.JWKSCacheMaxAge", func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{{}}, nil)
			mockMetrics.EXPECT().SetGauge(obs.MetricJWKSKeyCount, float64(1), gomock.Any())
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(&keys.JWKS{Keys: []keys.JWK{{}}}, nil)
			handlerFn := handler.JWKSHandler(mockKM, "test-tenant", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handlerFn(w, req)
			Expect(w.Header().Get("Cache-Control")).To(Equal("public, max-age=300"))
		})

		It("writes JSON-encoded JWKS body", func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{{}}, nil)
			mockMetrics.EXPECT().SetGauge(obs.MetricJWKSKeyCount, float64(1), gomock.Any())
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(&keys.JWKS{Keys: []keys.JWK{{}}}, nil)
			handlerFn := handler.JWKSHandler(mockKM, "test-tenant", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handlerFn(w, req)
			Expect(w.Body.String()).NotTo(BeEmpty())
		})
	})

	Context("GetAllKeyInfo — key count metric (STEP 0)", func() {
		It("calls SetGauge with count and tenant_id label when GetAllKeyInfo succeeds", func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{{}, {}}, nil)
			mockMetrics.EXPECT().SetGauge(
				obs.MetricJWKSKeyCount,
				float64(2),
				map[string]string{"tenant_id": "tenant-a"},
			)
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(&keys.JWKS{Keys: []keys.JWK{{}}}, nil)
			handlerFn := handler.JWKSHandler(mockKM, "tenant-a", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			handlerFn(httptest.NewRecorder(), req)
		})

		It("does not call SetGauge when GetAllKeyInfo returns an error", func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return(nil, errors.New("key store unavailable"))
			mockMetrics.EXPECT().SetGauge(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(&keys.JWKS{Keys: []keys.JWK{{}}}, nil)
			handlerFn := handler.JWKSHandler(mockKM, "test-tenant", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handlerFn(w, req)
			Expect(w.Code).To(Equal(http.StatusOK))
		})

		It("calls SetGauge with 0 when GetAllKeyInfo returns empty slice", func() {
			mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{}, nil)
			mockMetrics.EXPECT().SetGauge(obs.MetricJWKSKeyCount, float64(0), gomock.Any())
			mockKM.EXPECT().GetJWKS(gomock.Any()).Return(&keys.JWKS{Keys: []keys.JWK{{}}}, nil)
			handlerFn := handler.JWKSHandler(mockKM, "test-tenant", testCfg, mockMetrics)
			req := httptest.NewRequest(http.MethodGet, "/.well-known/jwks.json", nil).WithContext(ctx)
			w := httptest.NewRecorder()
			handlerFn(w, req)
			Expect(w.Code).To(Equal(http.StatusOK))
		})
	})
	}) // Phase 3: Core Operations
}) // JWKSHandler

