package interceptor_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/alicebob/miniredis/v2"
	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"github.com/aetomala/token-engine/internal/interceptor"
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/store"
	"github.com/aetomala/token-engine/internal/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redis/go-redis/v9"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// peerCtxWithCN builds a context containing a fake TLS peer with the given Common Name.
func peerCtxWithCN(cn string) context.Context {
	cert := &x509.Certificate{Subject: pkix.Name{CommonName: cn}}
	tlsInfo := credentials.TLSInfo{
		State: tls.ConnectionState{PeerCertificates: []*x509.Certificate{cert}},
	}
	return peer.NewContext(context.Background(), &peer.Peer{AuthInfo: tlsInfo})
}

var _ = Describe("AuthInterceptor", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		ctrl   *gomock.Controller
		mockAuth *testutil.MockAuthenticator
		mockLogger *testutil.MockLogger
		sut grpc.UnaryServerInterceptor
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockAuth = testutil.NewMockAuthenticator(ctrl)
		mockLogger = testutil.NewMockLogger(ctrl)
		sut = interceptor.NewAuthInterceptor(mockAuth, mockLogger)
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {
	Context("when Authenticator.Authenticate succeeds", func() {
		It("binds caller identity to ctx via WithCallerIdentity", func() {
			expectedIdentity := "test-caller-123"
			mockAuth.EXPECT().Authenticate(gomock.Any()).Return(expectedIdentity, nil)

			var capturedCtx context.Context
			handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
				capturedCtx = ctxIn
				return "response", nil
			}

			_, _ = sut(ctx, nil, &grpc.UnaryServerInfo{}, handler)

			identity := observability.CallerIdentityFromContext(capturedCtx)
			Expect(identity).To(Equal(expectedIdentity))
		})

		It("calls the handler", func() {
			mockAuth.EXPECT().Authenticate(gomock.Any()).Return("caller-456", nil)
			handlerCalled := false
			handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
				handlerCalled = true
				return "response", nil
			}

			_, _ = sut(ctx, nil, &grpc.UnaryServerInfo{}, handler)

			Expect(handlerCalled).To(BeTrue())
		})

		It("returns the handler's response", func() {
			mockAuth.EXPECT().Authenticate(gomock.Any()).Return("caller-789", nil)
			expectedResp := map[string]string{"key": "value"}
			handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
				return expectedResp, nil
			}

			resp, err := sut(ctx, nil, &grpc.UnaryServerInfo{}, handler)

			Expect(err).NotTo(HaveOccurred())
			Expect(resp).To(Equal(expectedResp))
		})
	})

	Context("when Authenticator.Authenticate returns a status error", func() {
		It("returns the status error without calling the handler", func() {
			authErr := status.Error(codes.Unauthenticated, "invalid api key")
			mockAuth.EXPECT().Authenticate(gomock.Any()).Return("", authErr)

			handlerCalled := false
			handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
				handlerCalled = true
				return "response", nil
			}

			_, err := sut(ctx, nil, &grpc.UnaryServerInfo{}, handler)

			Expect(handlerCalled).To(BeFalse())
			Expect(err).To(Equal(authErr))
		})
	})
	}) // Phase 3
})

var _ = Describe("StaticKeyAuthenticator", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		sut    *interceptor.StaticKeyAuthenticator
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		keys := map[string]string{
			"key1": "caller1",
			"key2": "caller2",
		}
		sut = interceptor.NewStaticKeyAuthenticator(keys)
	})

	AfterEach(func() {
		cancel()
	})

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {
	Context("when x-api-key header is present and matches a configured key", func() {
		It("returns the mapped caller identity", func() {
			md := metadata.Pairs(observability.MetadataKeyAPIKey, "key1")
			ctxWithMD := metadata.NewIncomingContext(ctx, md)

			identity, err := sut.Authenticate(ctxWithMD)

			Expect(err).NotTo(HaveOccurred())
			Expect(identity).To(Equal("caller1"))
		})

		It("returns the correct identity for key2", func() {
			md := metadata.Pairs(observability.MetadataKeyAPIKey, "key2")
			ctxWithMD := metadata.NewIncomingContext(ctx, md)

			identity, err := sut.Authenticate(ctxWithMD)

			Expect(err).NotTo(HaveOccurred())
			Expect(identity).To(Equal("caller2"))
		})
	})

	Context("when x-api-key header is absent", func() {
		It("returns codes.Unauthenticated", func() {
			ctxWithMD := metadata.NewIncomingContext(ctx, metadata.MD{})

			_, err := sut.Authenticate(ctxWithMD)

			Expect(err).To(HaveOccurred())
			st, ok := status.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(codes.Unauthenticated))
		})
	})

	Context("when x-api-key does not match any configured key", func() {
		It("returns codes.Unauthenticated", func() {
			md := metadata.Pairs(observability.MetadataKeyAPIKey, "unknown-key")
			ctxWithMD := metadata.NewIncomingContext(ctx, md)

			_, err := sut.Authenticate(ctxWithMD)

			Expect(err).To(HaveOccurred())
			st, ok := status.FromError(err)
			Expect(ok).To(BeTrue())
			Expect(st.Code()).To(Equal(codes.Unauthenticated))
		})
	})
	}) // Phase 3
})

var _ = Describe("IdempotencyInterceptor", func() {
	var (
		ctx         context.Context
		cancel      context.CancelFunc
		ctrl        *gomock.Controller
		mockStore   *testutil.MockIdempotencyStore
		mockLogger  *testutil.MockLogger
		mockMetrics *testutil.MockMetrics
		sut         grpc.UnaryServerInterceptor
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockStore = testutil.NewMockIdempotencyStore(ctrl)
		mockLogger = testutil.NewMockLogger(ctrl)
		mockMetrics = testutil.NewMockMetrics(ctrl)
		sut = interceptor.NewIdempotencyInterceptor(mockStore, mockLogger, mockMetrics)
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	Describe("resolveIdempotencyKey precedence (issue #129 / ADR-014)", func() {
		DescribeTable("resolving the effective client key",
			func(headerKey, fieldKey, expectedKey string, expectError bool) {
				key, err := interceptor.ResolveIdempotencyKeyForTest(headerKey, fieldKey)
				if expectError {
					Expect(err).To(HaveOccurred())
					Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
				} else {
					Expect(err).NotTo(HaveOccurred())
				}
				Expect(key).To(Equal(expectedKey))
			},
			Entry("neither set", "", "", "", false),
			Entry("header only", "header-key", "", "header-key", false),
			Entry("field only", "", "field-key", "field-key", false),
			Entry("both set and equal", "same-key", "same-key", "same-key", false),
			Entry("both set and differ", "header-key", "field-key", "", true),
		)
	})

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {

		Context("when method is not IssueToken or RefreshToken", func() {
			It("calls the handler without checking the store", func() {
				handlerCalled := false
				handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
					handlerCalled = true
					return "resp", nil
				}
				mockStore.EXPECT().Get(gomock.Any(), gomock.Any()).Times(0)
				mockStore.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

				_, _ = sut(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/RevokeToken"}, handler)

				Expect(handlerCalled).To(BeTrue())
			})

			It("does not increment token_engine_idempotency_total", func() {
				handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
					return "resp", nil
				}
				mockMetrics.EXPECT().IncrementCounter(gomock.Any(), gomock.Any()).Times(0)

				_, _ = sut(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/RevokeToken"}, handler)
			})
		})

		Context("when x-idempotency-key metadata is absent", func() {
			It("calls the handler without checking the store", func() {
				req := &tokenv1.IssueTokenRequest{TenantId: "t1"}
				handlerCalled := false
				handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
					handlerCalled = true
					return &tokenv1.TokenPair{}, nil
				}
				mockStore.EXPECT().Get(gomock.Any(), gomock.Any()).Times(0)
				mockStore.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

				ctxNoMD := ctx
				_, _ = sut(ctxNoMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)

				Expect(handlerCalled).To(BeTrue())
			})

			It("does not increment token_engine_idempotency_total", func() {
				req := &tokenv1.IssueTokenRequest{TenantId: "t1"}
				handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
					return &tokenv1.TokenPair{}, nil
				}
				mockMetrics.EXPECT().IncrementCounter(gomock.Any(), gomock.Any()).Times(0)

				_, _ = sut(ctx, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
			})
		})

		Context("when x-idempotency-key is present — claim succeeds, handler succeeds", func() {
			var (
				ctxWithMD   context.Context
				req         *tokenv1.IssueTokenRequest
				expectedKey string
			)

			BeforeEach(func() {
				req = &tokenv1.IssueTokenRequest{TenantId: "tenant1"}
				ctxWithMD = metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "client-key-1"))
				expectedKey = "idempotency:tenant1:IssueToken:client-key-1"
			})

			It("calls store.SetNX to claim the key before calling the handler", func() {
				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(true, nil)
				mockStore.EXPECT().Get(gomock.Any(), gomock.Any()).Times(0)
				mockStore.EXPECT().Set(gomock.Any(), expectedKey, gomock.Any()).Return(nil)
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

				handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
					return &tokenv1.TokenPair{}, nil
				}
				_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
				Expect(err).NotTo(HaveOccurred())
			})

			It("calls store.Set to promote the claim with an envelope wrapping the marshaled response", func() {
				expectedResp := &tokenv1.TokenPair{}
				marshaledBytes, _ := proto.Marshal(expectedResp)
				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(true, nil)
				mockStore.EXPECT().Set(gomock.Any(), expectedKey, gomock.Any()).DoAndReturn(
					func(_ context.Context, _ string, value []byte) error {
						resp, _, isCompleted, err := interceptor.ResolveExistingRecordForTest(value)
						Expect(err).NotTo(HaveOccurred())
						Expect(isCompleted).To(BeTrue())
						respBytes, _ := proto.Marshal(resp)
						Expect(respBytes).To(Equal(marshaledBytes))
						return nil
					})
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return expectedResp, nil
				}
				_, _ = sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
			})

			It("increments token_engine_idempotency_total with result=miss", func() {
				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(true, nil)
				mockStore.EXPECT().Set(gomock.Any(), expectedKey, gomock.Any()).Return(nil)
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, map[string]string{
					"result":     "miss",
					"rpc_method": "/token.v1.TokenEngine/IssueToken",
				})

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return &tokenv1.TokenPair{}, nil
				}
				_, _ = sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
			})

			It("returns the handler's response", func() {
				expectedResp := &tokenv1.TokenPair{}
				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(true, nil)
				mockStore.EXPECT().Set(gomock.Any(), expectedKey, gomock.Any()).Return(nil)
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return expectedResp, nil
				}
				resp, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
				Expect(err).NotTo(HaveOccurred())
				Expect(resp).To(Equal(expectedResp))
			})
		})

		Context("when idempotency_key is present on the request field only, no header (issue #129)", func() {
			It("claims and promotes the key exactly as the header-only path does", func() {
				req := &tokenv1.IssueTokenRequest{TenantId: "tenant1", IdempotencyKey: "field-key-1"}
				expectedKey := "idempotency:tenant1:IssueToken:field-key-1"

				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(true, nil)
				mockStore.EXPECT().Set(gomock.Any(), expectedKey, gomock.Any()).Return(nil)
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return &tokenv1.TokenPair{}, nil
				}
				_, err := sut(ctx, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when the header and the idempotency_key field are both present and equal (issue #129)", func() {
			It("claims and promotes the key normally", func() {
				req := &tokenv1.IssueTokenRequest{TenantId: "tenant1", IdempotencyKey: "same-key"}
				ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "same-key"))
				expectedKey := "idempotency:tenant1:IssueToken:same-key"

				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(true, nil)
				mockStore.EXPECT().Set(gomock.Any(), expectedKey, gomock.Any()).Return(nil)
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return &tokenv1.TokenPair{}, nil
				}
				_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when the header and the idempotency_key field are both present and differ (issue #129 / ADR-014)", func() {
			It("returns codes.InvalidArgument without touching the store or calling the handler", func() {
				req := &tokenv1.IssueTokenRequest{TenantId: "tenant1", IdempotencyKey: "field-key"}
				ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "header-key"))

				mockStore.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockStore.EXPECT().Get(gomock.Any(), gomock.Any()).Times(0)
				mockStore.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockMetrics.EXPECT().IncrementCounter(gomock.Any(), gomock.Any()).Times(0)

				handlerCalled := false
				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					handlerCalled = true
					return &tokenv1.TokenPair{}, nil
				}
				_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
				Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
				Expect(handlerCalled).To(BeFalse())
			})
		})

		Context("when x-idempotency-key is present — claim succeeds, handler returns error", func() {
			var (
				ctxWithMD   context.Context
				req         *tokenv1.IssueTokenRequest
				expectedKey string
				handlerErr  error
			)

			BeforeEach(func() {
				req = &tokenv1.IssueTokenRequest{TenantId: "tenant1"}
				ctxWithMD = metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "client-key-1"))
				expectedKey = "idempotency:tenant1:IssueToken:client-key-1"
				handlerErr = status.Error(codes.Internal, "handler failed")
			})

			It("does not call store.Set", func() {
				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(true, nil)
				mockStore.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return nil, handlerErr
				}
				_, _ = sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
			})

			It("returns the handler error", func() {
				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(true, nil)
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return nil, handlerErr
				}
				_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
				Expect(err).To(Equal(handlerErr))
			})

			It("increments token_engine_idempotency_total with result=miss", func() {
				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(true, nil)
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, map[string]string{
					"result":     "miss",
					"rpc_method": "/token.v1.TokenEngine/IssueToken",
				})

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return nil, handlerErr
				}
				_, _ = sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
			})
		})

		Context("when x-idempotency-key is present — claim fails, existing record is completed (legacy bare-TokenPair)", func() {
			var (
				ctxWithMD   context.Context
				req         *tokenv1.IssueTokenRequest
				expectedKey string
				cachedResp  *tokenv1.TokenPair
				cachedBytes []byte
			)

			BeforeEach(func() {
				req = &tokenv1.IssueTokenRequest{TenantId: "tenant1"}
				ctxWithMD = metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "client-key-1"))
				expectedKey = "idempotency:tenant1:IssueToken:client-key-1"
				cachedResp = &tokenv1.TokenPair{}
				var err error
				cachedBytes, err = proto.Marshal(cachedResp)
				Expect(err).NotTo(HaveOccurred())
			})

			It("returns the unmarshaled cached response without calling the handler", func() {
				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(false, nil)
				mockStore.EXPECT().Get(gomock.Any(), expectedKey).Return(cachedBytes, true, nil)
				mockStore.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

				handlerCalled := false
				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					handlerCalled = true
					return &tokenv1.TokenPair{}, nil
				}
				_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
				Expect(err).NotTo(HaveOccurred())
				Expect(handlerCalled).To(BeFalse())
			})

			It("increments token_engine_idempotency_total with result=hit", func() {
				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(false, nil)
				mockStore.EXPECT().Get(gomock.Any(), expectedKey).Return(cachedBytes, true, nil)
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, map[string]string{
					"result":     "hit",
					"rpc_method": "/token.v1.TokenEngine/IssueToken",
				})

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return &tokenv1.TokenPair{}, nil
				}
				_, _ = sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
			})
		})

		Context("when x-idempotency-key is present — claim fails, existing record is completed (versioned envelope)", func() {
			It("returns the unmarshaled cached response without calling the handler when the fingerprint matches", func() {
				req := &tokenv1.IssueTokenRequest{TenantId: "tenant1", Sub: "user-a"}
				ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "client-key-1"))
				expectedKey := "idempotency:tenant1:IssueToken:client-key-1"
				cachedResp := &tokenv1.TokenPair{AccessToken: "cached-token"}
				respBytes, err := proto.Marshal(cachedResp)
				Expect(err).NotTo(HaveOccurred())
				fingerprint := interceptor.ComputeIssueFingerprintForTest(req)
				envelopeBytes := interceptor.EncodeCompletedRecordForTest(respBytes, fingerprint)

				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(false, nil)
				mockStore.EXPECT().Get(gomock.Any(), expectedKey).Return(envelopeBytes, true, nil)
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

				handlerCalled := false
				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					handlerCalled = true
					return &tokenv1.TokenPair{}, nil
				}
				resp, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
				Expect(err).NotTo(HaveOccurred())
				Expect(handlerCalled).To(BeFalse())
				respTokenPair, ok := resp.(*tokenv1.TokenPair)
				Expect(ok).To(BeTrue())
				Expect(respTokenPair.AccessToken).To(Equal(cachedResp.AccessToken))
			})
		})

		Context("when x-idempotency-key is present — claim fails, existing record's fingerprint does not match (issue #128)", func() {
			It("returns codes.FailedPrecondition without calling the handler, for a different subject", func() {
				originalReq := &tokenv1.IssueTokenRequest{TenantId: "tenant1", Sub: "user-a"}
				replayReq := &tokenv1.IssueTokenRequest{TenantId: "tenant1", Sub: "user-b"}
				ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "client-key-1"))
				expectedKey := "idempotency:tenant1:IssueToken:client-key-1"
				cachedResp := &tokenv1.TokenPair{AccessToken: "user-a-token"}
				respBytes, err := proto.Marshal(cachedResp)
				Expect(err).NotTo(HaveOccurred())
				envelopeBytes := interceptor.EncodeCompletedRecordForTest(respBytes, interceptor.ComputeIssueFingerprintForTest(originalReq))

				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(false, nil)
				mockStore.EXPECT().Get(gomock.Any(), expectedKey).Return(envelopeBytes, true, nil)
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, map[string]string{
					"result":     "mismatch",
					"rpc_method": "/token.v1.TokenEngine/IssueToken",
				})

				handlerCalled := false
				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					handlerCalled = true
					return &tokenv1.TokenPair{}, nil
				}
				_, err = sut(ctxWithMD, replayReq, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
				Expect(status.Code(err)).To(Equal(codes.FailedPrecondition))
				Expect(handlerCalled).To(BeFalse())
			})
		})

		Context("when x-idempotency-key is present — claim fails, existing record is still pending (concurrent duplicate)", func() {
			It("returns codes.Aborted without calling the handler", func() {
				req := &tokenv1.IssueTokenRequest{TenantId: "tenant1"}
				ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "client-key-1"))
				expectedKey := "idempotency:tenant1:IssueToken:client-key-1"
				pendingBytes := interceptor.PendingRecordForTest()

				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(false, nil)
				mockStore.EXPECT().Get(gomock.Any(), expectedKey).Return(pendingBytes, true, nil)
				mockStore.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any()).Times(0)

				handlerCalled := false
				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					handlerCalled = true
					return &tokenv1.TokenPair{}, nil
				}
				_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
				Expect(status.Code(err)).To(Equal(codes.Aborted))
				Expect(handlerCalled).To(BeFalse())
			})
		})

		Context("when claim fails and store.Get returns an error", func() {
			It("logs the error at Warn level and returns codes.Aborted without calling the handler", func() {
				req := &tokenv1.IssueTokenRequest{TenantId: "tenant1"}
				ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "client-key-1"))
				storeErr := errors.New("redis connection failed")

				mockStore.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil)
				mockStore.EXPECT().Get(gomock.Any(), gomock.Any()).Return(nil, false, storeErr)
				mockLogger.EXPECT().Warn(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())

				handlerCalled := false
				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					handlerCalled = true
					return &tokenv1.TokenPair{}, nil
				}
				_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
				Expect(status.Code(err)).To(Equal(codes.Aborted))
				Expect(handlerCalled).To(BeFalse())
			})
		})

		Context("when claim fails and the existing record cannot be unmarshaled", func() {
			It("logs the error at Warn level and returns codes.Aborted without calling the handler", func() {
				req := &tokenv1.IssueTokenRequest{TenantId: "tenant1"}
				ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "ck"))
				invalidBytes := []byte("not-proto")

				mockStore.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil)
				mockStore.EXPECT().Get(gomock.Any(), gomock.Any()).Return(invalidBytes, true, nil)
				mockLogger.EXPECT().Warn(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())

				handlerCalled := false
				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					handlerCalled = true
					return &tokenv1.TokenPair{}, nil
				}
				_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
				Expect(status.Code(err)).To(Equal(codes.Aborted))
				Expect(handlerCalled).To(BeFalse())
			})
		})

		Context("when store.SetNX (claim) returns an error", func() {
			var (
				ctxWithMD context.Context
				req       *tokenv1.IssueTokenRequest
				setNXErr  error
			)

			BeforeEach(func() {
				req = &tokenv1.IssueTokenRequest{TenantId: "tenant1"}
				ctxWithMD = metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "client-key-1"))
				setNXErr = errors.New("redis write failed")
			})

			It("logs the error at Warn level and degrades open — calls the handler anyway", func() {
				mockStore.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any()).Return(false, setNXErr)
				mockStore.EXPECT().Get(gomock.Any(), gomock.Any()).Times(0)
				mockStore.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				mockLogger.EXPECT().Warn(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

				handlerCalled := false
				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					handlerCalled = true
					return &tokenv1.TokenPair{}, nil
				}
				_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
				Expect(err).NotTo(HaveOccurred())
				Expect(handlerCalled).To(BeTrue())
			})

			It("returns the handler's response", func() {
				expectedResp := &tokenv1.TokenPair{}
				mockStore.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any()).Return(false, setNXErr)
				mockStore.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				mockLogger.EXPECT().Warn(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return expectedResp, nil
				}
				resp, _ := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
				Expect(resp).To(Equal(expectedResp))
			})

			It("increments token_engine_idempotency_total with result=miss", func() {
				mockStore.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any()).Return(false, setNXErr)
				mockStore.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				mockLogger.EXPECT().Warn(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, map[string]string{
					"result":     "miss",
					"rpc_method": "/token.v1.TokenEngine/IssueToken",
				})

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return &tokenv1.TokenPair{}, nil
				}
				_, _ = sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
			})
		})

		Context("key construction", func() {
			It("constructs key as: idempotency:tenantID:IssueToken:clientKey", func() {
				req := &tokenv1.IssueTokenRequest{TenantId: "acme"}
				ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "req-abc"))
				expectedKey := "idempotency:acme:IssueToken:req-abc"

				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(true, nil)
				mockStore.EXPECT().Set(gomock.Any(), expectedKey, gomock.Any()).Return(nil)
				mockMetrics.EXPECT().IncrementCounter(gomock.Any(), gomock.Any())

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return &tokenv1.TokenPair{}, nil
				}
				_, _ = sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
			})

			It("uses 'default' as tenantID when IssueTokenRequest.TenantId is empty", func() {
				req := &tokenv1.IssueTokenRequest{TenantId: ""}
				ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "req-xyz"))
				expectedKey := "idempotency:default:IssueToken:req-xyz"

				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(true, nil)
				mockStore.EXPECT().Set(gomock.Any(), expectedKey, gomock.Any()).Return(nil)
				mockMetrics.EXPECT().IncrementCounter(gomock.Any(), gomock.Any())

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return &tokenv1.TokenPair{}, nil
				}
				_, _ = sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
			})

			It("uses IssueTokenRequest.TenantId when non-empty", func() {
				req := &tokenv1.IssueTokenRequest{TenantId: "globalcorp"}
				ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "req-123"))
				expectedKey := "idempotency:globalcorp:IssueToken:req-123"

				mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(true, nil)
				mockStore.EXPECT().Set(gomock.Any(), expectedKey, gomock.Any()).Return(nil)
				mockMetrics.EXPECT().IncrementCounter(gomock.Any(), gomock.Any())

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return &tokenv1.TokenPair{}, nil
				}
				_, _ = sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
			})
		})

		Context("metric label values", func() {
			It("sets rpc_method label to info.FullMethod", func() {
				req := &tokenv1.IssueTokenRequest{TenantId: "t"}
				ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "k"))

				mockStore.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil)
				mockStore.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
				mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, map[string]string{
					"result":     "miss",
					"rpc_method": "/token.v1.TokenEngine/IssueToken",
				})

				handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
					return &tokenv1.TokenPair{}, nil
				}
				_, _ = sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
			})
		})

		Context("RefreshToken — promoted in v0.6", func() {
			const refreshMethod = "/token.v1.TokenEngine/RefreshToken"

			Context("when x-idempotency-key is absent", func() {
				It("passes through to handler without checking the store", func() {
					req := &tokenv1.RefreshTokenRequest{TenantId: "t1"}
					handlerCalled := false
					handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
						handlerCalled = true
						return &tokenv1.TokenPair{}, nil
					}
					mockStore.EXPECT().Get(gomock.Any(), gomock.Any()).Times(0)
					mockStore.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

					_, _ = sut(ctx, req, &grpc.UnaryServerInfo{FullMethod: refreshMethod}, handler)

					Expect(handlerCalled).To(BeTrue())
				})
			})

			Context("when x-idempotency-key is present — claim succeeds", func() {
				It("calls SetNX (claim) BEFORE handler, then Set (promote) after handler success", func() {
					req := &tokenv1.RefreshTokenRequest{TenantId: "tenant1"}
					ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "refresh-key-1"))
					expectedKey := "idempotency:tenant1:RefreshToken:refresh-key-1"

					claimOrder := 0
					handlerOrder := 0
					var claimCallOrder int

					mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).DoAndReturn(
						func(_ context.Context, _ string, _ []byte) (bool, error) {
							claimCallOrder = claimOrder
							claimOrder++
							return true, nil
						})
					mockStore.EXPECT().Set(gomock.Any(), expectedKey, gomock.Any()).Return(nil)
					mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

					handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
						handlerOrder = claimOrder
						return &tokenv1.TokenPair{}, nil
					}
					_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: refreshMethod}, handler)

					Expect(err).NotTo(HaveOccurred())
					Expect(claimCallOrder).To(Equal(0))
					Expect(handlerOrder).To(Equal(1))
				})
			})

			Context("when idempotency_key is present on the request field only, no header (issue #129)", func() {
				It("claims and promotes the key exactly as the header-only path does", func() {
					req := &tokenv1.RefreshTokenRequest{TenantId: "tenant1", IdempotencyKey: "field-refresh-key"}
					expectedKey := "idempotency:tenant1:RefreshToken:field-refresh-key"

					mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(true, nil)
					mockStore.EXPECT().Set(gomock.Any(), expectedKey, gomock.Any()).Return(nil)
					mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

					handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
						return &tokenv1.TokenPair{}, nil
					}
					_, err := sut(ctx, req, &grpc.UnaryServerInfo{FullMethod: refreshMethod}, handler)
					Expect(err).NotTo(HaveOccurred())
				})
			})

			Context("when the header and the idempotency_key field are both present and equal (issue #129)", func() {
				It("claims and promotes the key normally", func() {
					req := &tokenv1.RefreshTokenRequest{TenantId: "tenant1", IdempotencyKey: "same-refresh-key"}
					ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "same-refresh-key"))
					expectedKey := "idempotency:tenant1:RefreshToken:same-refresh-key"

					mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(true, nil)
					mockStore.EXPECT().Set(gomock.Any(), expectedKey, gomock.Any()).Return(nil)
					mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

					handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
						return &tokenv1.TokenPair{}, nil
					}
					_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: refreshMethod}, handler)
					Expect(err).NotTo(HaveOccurred())
				})
			})

			Context("when the header and the idempotency_key field are both present and differ (issue #129 / ADR-014)", func() {
				It("returns codes.InvalidArgument without touching the store or calling the handler", func() {
					req := &tokenv1.RefreshTokenRequest{TenantId: "tenant1", IdempotencyKey: "field-refresh-key"}
					ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "header-refresh-key"))

					mockStore.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
					mockStore.EXPECT().Get(gomock.Any(), gomock.Any()).Times(0)
					mockStore.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
					mockMetrics.EXPECT().IncrementCounter(gomock.Any(), gomock.Any()).Times(0)

					handlerCalled := false
					handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
						handlerCalled = true
						return &tokenv1.TokenPair{}, nil
					}
					_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: refreshMethod}, handler)
					Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
					Expect(handlerCalled).To(BeFalse())
				})
			})

			Context("when x-idempotency-key is present — claim fails, existing record completed", func() {
				It("returns cached response without calling handler", func() {
					req := &tokenv1.RefreshTokenRequest{TenantId: "tenant1"}
					ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "refresh-key-1"))
					expectedKey := "idempotency:tenant1:RefreshToken:refresh-key-1"
					cachedResp := &tokenv1.TokenPair{}
					cachedBytes, _ := proto.Marshal(cachedResp)

					mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(false, nil)
					mockStore.EXPECT().Get(gomock.Any(), expectedKey).Return(cachedBytes, true, nil)
					mockStore.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
					mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

					handlerCalled := false
					handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
						handlerCalled = true
						return &tokenv1.TokenPair{}, nil
					}
					_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: refreshMethod}, handler)

					Expect(err).NotTo(HaveOccurred())
					Expect(handlerCalled).To(BeFalse())
				})
			})

			Context("when x-idempotency-key is present — claim fails, existing record's fingerprint does not match (issue #128)", func() {
				It("returns codes.FailedPrecondition without calling the handler, for a different refresh token", func() {
					originalReq := &tokenv1.RefreshTokenRequest{TenantId: "tenant1", RefreshToken: "original-rt"}
					replayReq := &tokenv1.RefreshTokenRequest{TenantId: "tenant1", RefreshToken: "different-rt"}
					ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "refresh-key-1"))
					expectedKey := "idempotency:tenant1:RefreshToken:refresh-key-1"
					cachedResp := &tokenv1.TokenPair{AccessToken: "original-token"}
					respBytes, err := proto.Marshal(cachedResp)
					Expect(err).NotTo(HaveOccurred())
					envelopeBytes := interceptor.EncodeCompletedRecordForTest(respBytes, interceptor.ComputeRefreshFingerprintForTest(originalReq))

					mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(false, nil)
					mockStore.EXPECT().Get(gomock.Any(), expectedKey).Return(envelopeBytes, true, nil)
					mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, map[string]string{
						"result":     "mismatch",
						"rpc_method": refreshMethod,
					})

					handlerCalled := false
					handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
						handlerCalled = true
						return &tokenv1.TokenPair{}, nil
					}
					_, err = sut(ctxWithMD, replayReq, &grpc.UnaryServerInfo{FullMethod: refreshMethod}, handler)

					Expect(status.Code(err)).To(Equal(codes.FailedPrecondition))
					Expect(handlerCalled).To(BeFalse())
				})
			})

			Context("when x-idempotency-key is present — claim fails, existing record still pending (concurrent duplicate)", func() {
				It("returns codes.Aborted without calling the handler", func() {
					req := &tokenv1.RefreshTokenRequest{TenantId: "tenant1"}
					ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "refresh-key-1"))
					expectedKey := "idempotency:tenant1:RefreshToken:refresh-key-1"
					pendingBytes := interceptor.PendingRecordForTest()

					mockStore.EXPECT().SetNX(gomock.Any(), expectedKey, gomock.Any()).Return(false, nil)
					mockStore.EXPECT().Get(gomock.Any(), expectedKey).Return(pendingBytes, true, nil)

					handlerCalled := false
					handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
						handlerCalled = true
						return &tokenv1.TokenPair{}, nil
					}
					_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: refreshMethod}, handler)

					Expect(status.Code(err)).To(Equal(codes.Aborted))
					Expect(handlerCalled).To(BeFalse())
				})
			})

			Context("when claim fails and store Get returns an error", func() {
				It("logs Warn and returns codes.Aborted without calling the handler", func() {
					req := &tokenv1.RefreshTokenRequest{TenantId: "tenant1"}
					ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "refresh-key-1"))

					mockStore.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any()).Return(false, nil)
					mockStore.EXPECT().Get(gomock.Any(), gomock.Any()).Return(nil, false, errors.New("redis down"))
					mockLogger.EXPECT().Warn(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any())

					handlerCalled := false
					handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
						handlerCalled = true
						return &tokenv1.TokenPair{}, nil
					}
					_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: refreshMethod}, handler)

					Expect(status.Code(err)).To(Equal(codes.Aborted))
					Expect(handlerCalled).To(BeFalse())
				})
			})

			Context("when claim succeeds and handler returns error", func() {
				It("does not call Set", func() {
					req := &tokenv1.RefreshTokenRequest{TenantId: "tenant1"}
					ctxWithMD := metadata.NewIncomingContext(ctx, metadata.Pairs(observability.MetadataKeyIdempotencyKey, "refresh-key-1"))

					mockStore.EXPECT().SetNX(gomock.Any(), gomock.Any(), gomock.Any()).Return(true, nil)
					mockStore.EXPECT().Set(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
					mockMetrics.EXPECT().IncrementCounter(observability.MetricIdempotencyTotal, gomock.Any())

					handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
						return nil, errors.New("handler error")
					}
					_, err := sut(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: refreshMethod}, handler)

					Expect(err).To(HaveOccurred())
				})
			})
		})

	}) // Phase 3

	// ===== PHASE 5: Concurrency =====
	Describe("Phase 5: Concurrency — real store, concurrent same-key requests", func() {
		var (
			mr        *miniredis.Miniredis
			client    *redis.Client
			realStore *store.RedisIdempotencyStore
			realSUT   grpc.UnaryServerInterceptor
		)

		BeforeEach(func() {
			var err error
			mr, err = miniredis.Run()
			Expect(err).NotTo(HaveOccurred())
			client = redis.NewClient(&redis.Options{Addr: mr.Addr()})
			realStore = store.NewRedisIdempotencyStore(client, time.Minute, time.Minute)
			realSUT = interceptor.NewIdempotencyInterceptor(realStore, observability.NewNoOpLogger(), observability.NewNoOpMetrics())
		})

		AfterEach(func() {
			_ = client.Close()
			mr.Close()
		})

		It("invokes the handler exactly once for two concurrent IssueToken requests sharing an idempotency key; the loser receives codes.Aborted", func() {
			req := &tokenv1.IssueTokenRequest{TenantId: "tenant1"}
			ctxWithMD := metadata.NewIncomingContext(context.Background(), metadata.Pairs(observability.MetadataKeyIdempotencyKey, "concurrent-key"))

			var handlerCalls int32
			release := make(chan struct{})
			handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
				atomic.AddInt32(&handlerCalls, 1)
				<-release
				return &tokenv1.TokenPair{AccessToken: "tok"}, nil
			}

			type result struct {
				resp interface{}
				err  error
			}
			results := make(chan result, 2)
			var wg sync.WaitGroup
			wg.Add(2)
			for i := 0; i < 2; i++ {
				go func() {
					defer wg.Done()
					resp, err := realSUT(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
					results <- result{resp, err}
				}()
			}

			// Give both goroutines a chance to reach the claim/blocked-handler point before releasing.
			time.Sleep(100 * time.Millisecond)
			close(release)
			wg.Wait()
			close(results)

			Expect(atomic.LoadInt32(&handlerCalls)).To(Equal(int32(1)))

			var succeeded, aborted int
			for r := range results {
				if r.err != nil {
					Expect(status.Code(r.err)).To(Equal(codes.Aborted))
					aborted++
				} else {
					Expect(r.resp).To(BeAssignableToTypeOf(&tokenv1.TokenPair{}))
					succeeded++
				}
			}
			Expect(succeeded).To(Equal(1))
			Expect(aborted).To(Equal(1))
		})

		It("invokes the handler exactly once for two concurrent RefreshToken requests sharing an idempotency key; the loser receives codes.Aborted", func() {
			req := &tokenv1.RefreshTokenRequest{TenantId: "tenant1"}
			ctxWithMD := metadata.NewIncomingContext(context.Background(), metadata.Pairs(observability.MetadataKeyIdempotencyKey, "concurrent-refresh-key"))

			var handlerCalls int32
			release := make(chan struct{})
			handler := func(ctxIn context.Context, r interface{}) (interface{}, error) {
				atomic.AddInt32(&handlerCalls, 1)
				<-release
				return &tokenv1.TokenPair{AccessToken: "tok"}, nil
			}

			type result struct {
				resp interface{}
				err  error
			}
			results := make(chan result, 2)
			var wg sync.WaitGroup
			wg.Add(2)
			for i := 0; i < 2; i++ {
				go func() {
					defer wg.Done()
					resp, err := realSUT(ctxWithMD, req, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/RefreshToken"}, handler)
					results <- result{resp, err}
				}()
			}

			time.Sleep(100 * time.Millisecond)
			close(release)
			wg.Wait()
			close(results)

			Expect(atomic.LoadInt32(&handlerCalls)).To(Equal(int32(1)))

			var succeeded, aborted int
			for r := range results {
				if r.err != nil {
					Expect(status.Code(r.err)).To(Equal(codes.Aborted))
					aborted++
				} else {
					succeeded++
				}
			}
			Expect(succeeded).To(Equal(1))
			Expect(aborted).To(Equal(1))
		})
	})
})

var _ = Describe("ValidationInterceptor (v0.1 stub)", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		ctrl   *gomock.Controller
		mockLogger *testutil.MockLogger
		sut grpc.UnaryServerInterceptor
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockLogger = testutil.NewMockLogger(ctrl)
		sut = interceptor.NewValidationInterceptor(mockLogger)
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {
	Context("always", func() {
		It("calls the handler and returns its result unchanged", func() {
			expectedResp := map[string]string{"result": "ok"}
			expectedErr := errors.New("validation error")

			handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
				return expectedResp, expectedErr
			}

			resp, err := sut(ctx, nil, &grpc.UnaryServerInfo{}, handler)

			Expect(resp).To(Equal(expectedResp))
			Expect(err).To(Equal(expectedErr))
		})
	})
	}) // Phase 3
})

var _ = Describe("MTLSAuthenticator", func() {
	var sut *interceptor.MTLSAuthenticator

	BeforeEach(func() {
		sut = interceptor.NewMTLSAuthenticator()
	})

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {
	Context("when peer has a valid TLS certificate with non-empty CN", func() {
		It("returns the CN as caller identity", func() {
			ctx := peerCtxWithCN("service-a")

			identity, err := sut.Authenticate(ctx)

			Expect(err).NotTo(HaveOccurred())
			Expect(identity).To(Equal("service-a"))
		})
	})

	Context("when no peer information in context", func() {
		It("returns codes.Unauthenticated", func() {
			_, err := sut.Authenticate(context.Background())

			Expect(status.Code(err)).To(Equal(codes.Unauthenticated))
		})
	})

	Context("when peer has no TLS credentials", func() {
		It("returns codes.Unauthenticated", func() {
			ctx := peer.NewContext(context.Background(), &peer.Peer{
				AuthInfo: nil,
			})

			_, err := sut.Authenticate(ctx)

			Expect(status.Code(err)).To(Equal(codes.Unauthenticated))
		})
	})

	Context("when peer has no certificates", func() {
		It("returns codes.Unauthenticated", func() {
			tlsInfo := credentials.TLSInfo{
				State: tls.ConnectionState{PeerCertificates: nil},
			}
			ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: tlsInfo})

			_, err := sut.Authenticate(ctx)

			Expect(status.Code(err)).To(Equal(codes.Unauthenticated))
		})
	})

	Context("when leaf certificate has empty Common Name", func() {
		It("returns codes.Unauthenticated", func() {
			ctx := peerCtxWithCN("")

			_, err := sut.Authenticate(ctx)

			Expect(status.Code(err)).To(Equal(codes.Unauthenticated))
		})
	})
	}) // Phase 3
})

var _ = Describe("CallerAuthorizationInterceptor", func() {
	var (
		ctx          context.Context
		cancel       context.CancelFunc
		ctrl         *gomock.Controller
		mockRegistry *testutil.MockCallerRegistry
		mockLogger   *testutil.MockLogger
		sut          grpc.UnaryServerInterceptor
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockRegistry = testutil.NewMockCallerRegistry(ctrl)
		mockLogger = testutil.NewMockLogger(ctrl)
		sut = interceptor.NewCallerAuthorizationInterceptor(mockRegistry, mockLogger)
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {
	Context("when method is gRPC health protocol", func() {
		It("calls the handler without checking the registry", func() {
			mockRegistry.EXPECT().IsPermitted(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			handlerCalled := false
			handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
				handlerCalled = true
				return "ok", nil
			}

			_, _ = sut(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/grpc.health.v1.Health/Check"}, handler)

			Expect(handlerCalled).To(BeTrue())
		})
	})

	Context("when caller identity is empty in context", func() {
		It("returns codes.Internal without calling the registry", func() {
			mockRegistry.EXPECT().IsPermitted(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)

			_, err := sut(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, nil)

			Expect(status.Code(err)).To(Equal(codes.Internal))
		})
	})

	Context("when caller is permitted for the tenant", func() {
		BeforeEach(func() {
			ctx = observability.WithCallerIdentity(ctx, "service-a")
		})

		It("calls the handler", func() {
			mockRegistry.EXPECT().IsPermitted(gomock.Any(), "service-a", gomock.Any()).Return(true, nil)
			handlerCalled := false
			handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
				handlerCalled = true
				return "resp", nil
			}

			_, _ = sut(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)

			Expect(handlerCalled).To(BeTrue())
		})

		It("returns the handler's response", func() {
			mockRegistry.EXPECT().IsPermitted(gomock.Any(), "service-a", gomock.Any()).Return(true, nil)
			expected := "the-response"
			handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
				return expected, nil
			}

			resp, err := sut(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)

			Expect(err).NotTo(HaveOccurred())
			Expect(resp).To(Equal(expected))
		})
	})

	Context("when IsPermitted returns codes.PermissionDenied", func() {
		BeforeEach(func() {
			ctx = observability.WithCallerIdentity(ctx, "service-b")
		})

		It("returns the error without calling the handler", func() {
			permErr := status.Error(codes.PermissionDenied, "caller not authorized")
			mockRegistry.EXPECT().IsPermitted(gomock.Any(), "service-b", gomock.Any()).Return(false, permErr)
			handlerCalled := false
			handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
				handlerCalled = true
				return nil, nil
			}

			_, err := sut(ctx, nil, &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)

			Expect(status.Code(err)).To(Equal(codes.PermissionDenied))
			Expect(handlerCalled).To(BeFalse())
		})
	})

	Context("when req does not implement TenantAwareRequest", func() {
		BeforeEach(func() {
			ctx = observability.WithCallerIdentity(ctx, "service-a")
		})

		It("passes tenantID as empty string to IsPermitted", func() {
			mockRegistry.EXPECT().IsPermitted(gomock.Any(), "service-a", "").Return(true, nil)
			handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
				return "ok", nil
			}

			// Pass a non-TenantAwareRequest value (plain string, not a proto message)
			_, _ = sut(ctx, "not-a-tenant-aware-req", &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/IssueToken"}, handler)
		})
	})
	}) // Phase 3
})
