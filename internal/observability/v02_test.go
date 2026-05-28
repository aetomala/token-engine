package observability_test

import (
	"context"
	"time"

	librarymetrics "github.com/aetomala/jwtauth/pkg/metrics"
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

// ===== LibraryPrometheusMetrics =====

var _ = Describe("Phase 3: LibraryPrometheusMetrics", func() {
	var (
		ctx         context.Context
		cancel      context.CancelFunc
		ctrl        *gomock.Controller
		mockInner   *testutil.MockMetrics
		sut         *obs.LibraryPrometheusMetrics
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockInner = testutil.NewMockMetrics(ctrl)
		sut = obs.NewLibraryPrometheusMetrics(mockInner)
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	_ = ctx // suppress unused warning in non-ctx tests

	Context("satisfies librarymetrics.Metrics interface", func() {
		It("compiles with var _ librarymetrics.Metrics = (*obs.LibraryPrometheusMetrics)(nil)", func() {
			var _ librarymetrics.Metrics = (*obs.LibraryPrometheusMetrics)(nil)
			Expect(true).To(BeTrue())
		})
	})

	Context("each method delegates to inner without transformation", func() {
		It("IncrementCounter delegates to inner with exact args", func() {
			labels := map[string]string{"key": "val"}
			mockInner.EXPECT().IncrementCounter("some_metric", labels)

			sut.IncrementCounter("some_metric", labels)
		})

		It("AddCounter delegates to inner with exact args", func() {
			labels := map[string]string{"k": "v"}
			mockInner.EXPECT().AddCounter("some_counter", 3.14, labels)

			sut.AddCounter("some_counter", 3.14, labels)
		})

		It("SetGauge delegates to inner with exact args", func() {
			labels := map[string]string{"env": "test"}
			mockInner.EXPECT().SetGauge("some_gauge", 42.0, labels)

			sut.SetGauge("some_gauge", 42.0, labels)
		})

		It("RecordHistogram delegates to inner with exact args", func() {
			labels := map[string]string{}
			mockInner.EXPECT().RecordHistogram("some_histogram", 0.5, labels)

			sut.RecordHistogram("some_histogram", 0.5, labels)
		})

		It("RecordDuration delegates to inner with exact args", func() {
			labels := map[string]string{"rpc": "test"}
			mockInner.EXPECT().RecordDuration("some_duration", 2*time.Second, labels)

			sut.RecordDuration("some_duration", 2*time.Second, labels)
		})
	})
})

// ===== CorrelationInterceptor — metric emission =====

var _ = Describe("Phase 3: CorrelationInterceptor — metric emission", func() {
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
		info = &grpc.UnaryServerInfo{FullMethod: "/test.Service/Method"}
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	Context("success path", func() {
		It("increments MetricGRPCRequests with rpc_method=info.FullMethod and status='OK'", func() {
			ctxWithMeta := metadata.NewIncomingContext(ctx, metadata.MD{})
			interceptorFn := obs.NewCorrelationInterceptor(mockLogger, mockMetrics)
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return nil, nil
			}

			mockMetrics.EXPECT().IncrementCounter(obs.MetricGRPCRequests, map[string]string{
				"rpc_method": "/test.Service/Method",
				"status":     codes.OK.String(),
			})
			mockMetrics.EXPECT().RecordDuration(obs.MetricGRPCDuration, gomock.Any(), map[string]string{
				"rpc_method": "/test.Service/Method",
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
				"rpc_method": "/test.Service/Method",
			})

			_, _ = interceptorFn(ctxWithMeta, nil, info, handler)
		})
	})

	Context("error path", func() {
		It("increments MetricGRPCRequests with status matching the gRPC error code string", func() {
			ctxWithMeta := metadata.NewIncomingContext(ctx, metadata.MD{})
			interceptorFn := obs.NewCorrelationInterceptor(mockLogger, mockMetrics)
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return nil, status.Error(codes.NotFound, "not found")
			}

			mockMetrics.EXPECT().IncrementCounter(obs.MetricGRPCRequests, map[string]string{
				"rpc_method": "/test.Service/Method",
				"status":     codes.NotFound.String(),
			})
			mockMetrics.EXPECT().RecordDuration(obs.MetricGRPCDuration, gomock.Any(), gomock.Any())

			_, _ = interceptorFn(ctxWithMeta, nil, info, handler)
		})

		It("records MetricGRPCDuration even when handler returns error", func() {
			ctxWithMeta := metadata.NewIncomingContext(ctx, metadata.MD{})
			interceptorFn := obs.NewCorrelationInterceptor(mockLogger, mockMetrics)
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return nil, status.Error(codes.Internal, "oops")
			}

			mockMetrics.EXPECT().IncrementCounter(obs.MetricGRPCRequests, gomock.Any())
			mockMetrics.EXPECT().RecordDuration(obs.MetricGRPCDuration, gomock.Any(), map[string]string{
				"rpc_method": "/test.Service/Method",
			})

			_, _ = interceptorFn(ctxWithMeta, nil, info, handler)
		})
	})
})
