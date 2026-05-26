package interceptor_test

import (
	"context"
	"errors"
	"time"

	"github.com/aetomala/token-engine/internal/interceptor"
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/store"
	"github.com/aetomala/token-engine/internal/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

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
})

var _ = Describe("IdempotencyInterceptor (v0.1 stub)", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		ctrl   *gomock.Controller
		mockLogger *testutil.MockLogger
		mockMetrics *testutil.MockMetrics
		sut grpc.UnaryServerInterceptor
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockLogger = testutil.NewMockLogger(ctrl)
		mockMetrics = testutil.NewMockMetrics(ctrl)
		// For v0.1 stub, use NoOpIdempotencyStore
		noOpStore := store.NewNoOpIdempotencyStore()
		sut = interceptor.NewIdempotencyInterceptor(noOpStore, mockLogger, mockMetrics)
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	Context("always", func() {
		It("calls the handler and returns its result unchanged", func() {
			expectedResp := map[string]string{"response": "data"}
			expectedErr := errors.New("some error")

			handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
				return expectedResp, expectedErr
			}

			resp, err := sut(ctx, nil, &grpc.UnaryServerInfo{}, handler)

			Expect(resp).To(Equal(expectedResp))
			Expect(err).To(Equal(expectedErr))
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
})

var _ = Describe("CallerAuthorizationInterceptor (v0.1 stub)", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		ctrl   *gomock.Controller
		mockRegistry *testutil.MockCallerRegistry
		mockLogger *testutil.MockLogger
		sut grpc.UnaryServerInterceptor
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

	Context("always", func() {
		It("calls the handler and returns its result unchanged", func() {
			expectedResp := "authorized"
			expectedErr := errors.New("authorization error")

			handler := func(ctxIn context.Context, req interface{}) (interface{}, error) {
				return expectedResp, expectedErr
			}

			resp, err := sut(ctx, nil, &grpc.UnaryServerInfo{}, handler)

			Expect(resp).To(Equal(expectedResp))
			Expect(err).To(Equal(expectedErr))
		})
	})
})
