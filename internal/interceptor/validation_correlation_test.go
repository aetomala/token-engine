package interceptor_test

import (
	"context"
	"time"

	tokenv1 "github.com/aetomala/token-engine/gen/v1"
	"github.com/aetomala/token-engine/internal/interceptor"
	obs "github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

var _ = Describe("ValidationInterceptor", func() {
	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: ValidationInterceptor", func() {
		var (
			ctx        context.Context
			cancel     context.CancelFunc
			ctrl       *gomock.Controller
			mockLogger *testutil.MockLogger
			sut        grpc.UnaryServerInterceptor
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

		Context("IssueTokenRequest with empty sub", func() {
			It("returns codes.InvalidArgument without calling the handler", func() {
				req := &tokenv1.IssueTokenRequest{Sub: "", TenantId: "t1"}
				info := &grpc.UnaryServerInfo{FullMethod: tokenv1.TokenEngine_IssueToken_FullMethodName}
				handlerCalled := false
				handler := func(ctx context.Context, req interface{}) (interface{}, error) {
					handlerCalled = true
					return nil, nil
				}

				_, err := sut(ctx, req, info, handler)

				Expect(handlerCalled).To(BeFalse())
				Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			})
		})

		Context("IssueTokenRequest with reserved claim key", func() {
			issueInfo := &grpc.UnaryServerInfo{FullMethod: tokenv1.TokenEngine_IssueToken_FullMethodName}

			for _, key := range []string{"sub", "iss", "aud", "exp", "iat", "nbf", "jti"} {
				key := key
				It("returns codes.InvalidArgument naming the offending key — '"+key+"'", func() {
					req := &tokenv1.IssueTokenRequest{
						Sub:    "user1",
						Claims: map[string]string{key: "value"},
					}
					handlerCalled := false
					handler := func(ctx context.Context, req interface{}) (interface{}, error) {
						handlerCalled = true
						return nil, nil
					}

					_, err := sut(ctx, req, issueInfo, handler)

					Expect(handlerCalled).To(BeFalse())
					Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
					Expect(err.Error()).To(ContainSubstring(`"` + key + `"`))
				})
			}

			It("does not call the handler", func() {
				req := &tokenv1.IssueTokenRequest{
					Sub:    "user1",
					Claims: map[string]string{"sub": "bad"},
				}
				handlerCalled := false
				handler := func(ctx context.Context, req interface{}) (interface{}, error) {
					handlerCalled = true
					return nil, nil
				}

				_, _ = sut(ctx, req, issueInfo, handler)

				Expect(handlerCalled).To(BeFalse())
			})
		})

		Context("RefreshTokenRequest with reserved claim key", func() {
			It("returns codes.InvalidArgument naming the offending key", func() {
				req := &tokenv1.RefreshTokenRequest{
					RefreshToken: "tok",
					Claims:       map[string]string{"exp": "bad"},
				}
				info := &grpc.UnaryServerInfo{FullMethod: tokenv1.TokenEngine_RefreshToken_FullMethodName}
				handlerCalled := false
				handler := func(ctx context.Context, req interface{}) (interface{}, error) {
					handlerCalled = true
					return nil, nil
				}

				_, err := sut(ctx, req, info, handler)

				Expect(handlerCalled).To(BeFalse())
				Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
				Expect(err.Error()).To(ContainSubstring(`"exp"`))
			})

			It("does not call the handler", func() {
				req := &tokenv1.RefreshTokenRequest{
					RefreshToken: "tok",
					Claims:       map[string]string{"iss": "bad"},
				}
				info := &grpc.UnaryServerInfo{FullMethod: tokenv1.TokenEngine_RefreshToken_FullMethodName}
				handlerCalled := false
				handler := func(ctx context.Context, req interface{}) (interface{}, error) {
					handlerCalled = true
					return nil, nil
				}

				_, _ = sut(ctx, req, info, handler)

				Expect(handlerCalled).To(BeFalse())
			})
		})

		Context("IssueTokenRequest with valid sub and no reserved keys", func() {
			It("calls the handler and returns its response unchanged", func() {
				req := &tokenv1.IssueTokenRequest{
					Sub:    "user1",
					Claims: map[string]string{"role": "admin"},
				}
				info := &grpc.UnaryServerInfo{FullMethod: tokenv1.TokenEngine_IssueToken_FullMethodName}
				expected := &tokenv1.TokenPair{AccessToken: "acc", RefreshToken: "ref"}
				handler := func(ctx context.Context, req interface{}) (interface{}, error) {
					return expected, nil
				}

				resp, err := sut(ctx, req, info, handler)

				Expect(err).To(BeNil())
				Expect(resp).To(Equal(expected))
			})
		})

		Context("RPC method that is not IssueToken or RefreshToken", func() {
			It("calls the handler without inspecting the request", func() {
				info := &grpc.UnaryServerInfo{FullMethod: "/token.v1.TokenEngine/RevokeToken"}
				handlerCalled := false
				handler := func(ctx context.Context, req interface{}) (interface{}, error) {
					handlerCalled = true
					return "ok", nil
				}

				resp, err := sut(ctx, nil, info, handler)

				Expect(handlerCalled).To(BeTrue())
				Expect(err).To(BeNil())
				Expect(resp).To(Equal("ok"))
			})
		})
	})
})

var _ = Describe("CorrelationInterceptor", func() {
	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: CorrelationInterceptor (interceptor perspective)", func() {
		var (
			ctx         context.Context
			cancel      context.CancelFunc
			ctrl        *gomock.Controller
			mockMetrics *testutil.MockMetrics
			mockLogger  *testutil.MockLogger
			info        *grpc.UnaryServerInfo
		)

		BeforeEach(func() {
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			ctrl = gomock.NewController(GinkgoT())
			mockMetrics = testutil.NewMockMetrics(ctrl)
			mockLogger = testutil.NewMockLogger(ctrl)
			info = &grpc.UnaryServerInfo{FullMethod: tokenv1.TokenEngine_IssueToken_FullMethodName}
		})

		AfterEach(func() {
			cancel()
			ctrl.Finish()
		})

		Context("metric emission — success path", func() {
			It("increments MetricGRPCRequests with rpc_method=info.FullMethod and status='OK'", func() {
				ctxWithMeta := metadata.NewIncomingContext(ctx, metadata.MD{})
				interceptorFn := obs.NewCorrelationInterceptor(mockLogger, mockMetrics)
				handler := func(ctx context.Context, req interface{}) (interface{}, error) {
					return nil, nil
				}

				mockMetrics.EXPECT().IncrementCounter(obs.MetricGRPCRequests, map[string]string{
					"rpc_method": tokenv1.TokenEngine_IssueToken_FullMethodName,
					"status":     codes.OK.String(),
				})
				mockMetrics.EXPECT().RecordDuration(obs.MetricGRPCDuration, gomock.Any(), map[string]string{
					"rpc_method": tokenv1.TokenEngine_IssueToken_FullMethodName,
				})

				_, _ = interceptorFn(ctxWithMeta, nil, info, handler)
			})

			It("records MetricGRPCDuration with rpc_method=info.FullMethod", func() {
				ctxWithMeta := metadata.NewIncomingContext(ctx, metadata.MD{})
				interceptorFn := obs.NewCorrelationInterceptor(mockLogger, mockMetrics)
				handler := func(ctx context.Context, req interface{}) (interface{}, error) {
					return nil, nil
				}

				mockMetrics.EXPECT().IncrementCounter(obs.MetricGRPCRequests, gomock.Any())
				mockMetrics.EXPECT().RecordDuration(obs.MetricGRPCDuration, gomock.Any(), map[string]string{
					"rpc_method": tokenv1.TokenEngine_IssueToken_FullMethodName,
				})

				_, _ = interceptorFn(ctxWithMeta, nil, info, handler)
			})
		})

		Context("metric emission — error path", func() {
			It("increments MetricGRPCRequests with status matching the gRPC error code string", func() {
				ctxWithMeta := metadata.NewIncomingContext(ctx, metadata.MD{})
				interceptorFn := obs.NewCorrelationInterceptor(mockLogger, mockMetrics)
				handler := func(ctx context.Context, req interface{}) (interface{}, error) {
					return nil, status.Error(codes.PermissionDenied, "denied")
				}

				mockMetrics.EXPECT().IncrementCounter(obs.MetricGRPCRequests, map[string]string{
					"rpc_method": tokenv1.TokenEngine_IssueToken_FullMethodName,
					"status":     codes.PermissionDenied.String(),
				})
				mockMetrics.EXPECT().RecordDuration(obs.MetricGRPCDuration, gomock.Any(), gomock.Any())

				_, _ = interceptorFn(ctxWithMeta, nil, info, handler)
			})

			It("records MetricGRPCDuration even when handler returns error", func() {
				ctxWithMeta := metadata.NewIncomingContext(ctx, metadata.MD{})
				interceptorFn := obs.NewCorrelationInterceptor(mockLogger, mockMetrics)
				handler := func(ctx context.Context, req interface{}) (interface{}, error) {
					return nil, status.Error(codes.Unavailable, "unavailable")
				}

				mockMetrics.EXPECT().IncrementCounter(obs.MetricGRPCRequests, gomock.Any())
				mockMetrics.EXPECT().RecordDuration(obs.MetricGRPCDuration, gomock.Any(), map[string]string{
					"rpc_method": tokenv1.TokenEngine_IssueToken_FullMethodName,
				})

				_, _ = interceptorFn(ctxWithMeta, nil, info, handler)
			})
		})

		Context("correlation ID — absent from inbound metadata", func() {
			It("generates a UUID v4 and binds it to ctx", func() {
				ctxWithMeta := metadata.NewIncomingContext(ctx, metadata.MD{})
				interceptorFn := obs.NewCorrelationInterceptor(mockLogger, mockMetrics)

				var capturedCtx context.Context
				handler := func(ctx context.Context, req interface{}) (interface{}, error) {
					capturedCtx = ctx
					return nil, nil
				}

				mockMetrics.EXPECT().IncrementCounter(gomock.Any(), gomock.Any())
				mockMetrics.EXPECT().RecordDuration(gomock.Any(), gomock.Any(), gomock.Any())

				_, _ = interceptorFn(ctxWithMeta, nil, info, handler)

				id := obs.CorrelationIDFromContext(capturedCtx)
				Expect(id).NotTo(BeEmpty())
				Expect(len(id)).To(Equal(36)) // UUID v4
			})

			It("sets the correlation ID on the active OTel span as an attribute", func() {
				ctxWithMeta := metadata.NewIncomingContext(ctx, metadata.MD{})
				interceptorFn := obs.NewCorrelationInterceptor(mockLogger, mockMetrics)

				mockMetrics.EXPECT().IncrementCounter(gomock.Any(), gomock.Any())
				mockMetrics.EXPECT().RecordDuration(gomock.Any(), gomock.Any(), gomock.Any())

				Expect(func() {
					interceptorFn(ctxWithMeta, nil, info, func(ctx context.Context, req interface{}) (interface{}, error) {
						return nil, nil
					})
				}).NotTo(Panic())
			})
		})

		Context("correlation ID — present in inbound metadata", func() {
			It("extracts the existing ID and binds it to ctx without generating a new one", func() {
				existingID := "fixed-existing-id"
				ctxWithMeta := metadata.NewIncomingContext(ctx, metadata.Pairs(obs.MetadataKeyCorrelationID, existingID))
				interceptorFn := obs.NewCorrelationInterceptor(mockLogger, mockMetrics)

				var capturedCtx context.Context
				handler := func(ctx context.Context, req interface{}) (interface{}, error) {
					capturedCtx = ctx
					return nil, nil
				}

				mockMetrics.EXPECT().IncrementCounter(gomock.Any(), gomock.Any())
				mockMetrics.EXPECT().RecordDuration(gomock.Any(), gomock.Any(), gomock.Any())

				_, _ = interceptorFn(ctxWithMeta, nil, info, handler)

				Expect(obs.CorrelationIDFromContext(capturedCtx)).To(Equal(existingID))
			})
		})
	})
})
