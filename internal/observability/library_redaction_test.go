package observability_test

import (
	"context"
	"time"

	obs "github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.uber.org/mock/gomock"
)

const (
	redacted     = "[REDACTED]"
	secretValue  = "raw-refresh-token-secret"
	secretCursor = "raw-refresh-token-cursor"
)

var _ = Describe("LibraryLoggerAdapter — credential redaction", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		ctrl       *gomock.Controller
		mockLogger *testutil.MockLogger
		sut        *obs.LibraryLoggerAdapter
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockLogger = testutil.NewMockLogger(ctrl)
		sut = obs.NewLibraryLoggerAdapter(mockLogger)
	})

	AfterEach(func() {
		cancel()
		ctrl.Finish()
	})

	// captureInfo expects one Info call with msg and stores the forwarded keysAndValues in dst.
	captureInfo := func(msg string, dst *[]interface{}) {
		mockLogger.EXPECT().Info(gomock.Any(), msg, gomock.Any()).Do(
			func(_ context.Context, _ string, kv ...interface{}) { *dst = kv })
	}

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {
		Context("when the library passes a leading context", func() {
			It("redacts deny-listed values at odd key positions and keeps the context in place", func() {
				var got []interface{}
				captureInfo("refresh token issued", &got)

				sut.Info("refresh token issued", ctx, "userID", "user-1", "token", secretValue)

				Expect(got).To(Equal([]interface{}{ctx, "userID", "user-1", "token", redacted}))
			})

			It("treats a value equal to a deny-listed key name as a value, not a key", func() {
				var got []interface{}
				captureInfo("m", &got)

				sut.Info("m", ctx, "reason", "token", "userID", "user-1")

				Expect(got).To(Equal([]interface{}{ctx, "reason", "token", "userID", "user-1"}))
			})
		})

		Context("when the library passes no context", func() {
			It("redacts deny-listed values at even key positions", func() {
				var got []interface{}
				captureInfo("m", &got)

				sut.Info("m", "token", secretValue, "userID", "user-1")

				Expect(got).To(Equal([]interface{}{"token", redacted, "userID", "user-1"}))
			})
		})

		DescribeTable("redacts every deny-listed key",
			func(key string, value interface{}) {
				var got []interface{}
				captureInfo("m", &got)

				sut.Info("m", ctx, key, value)

				Expect(got).To(Equal([]interface{}{ctx, key, redacted}))
			},
			Entry("token with a string value", "token", secretValue),
			Entry("key with a string value", "key", "refresh_token:"+secretValue),
			Entry("cursor with a string value", "cursor", secretCursor),
			Entry("next_cursor with a string value", "next_cursor", secretCursor),
			Entry("cursor with a non-string value", "cursor", uint64(42)),
		)

		DescribeTable("leaves non-deny-listed keys untouched",
			func(key string) {
				var got []interface{}
				captureInfo("m", &got)

				sut.Info("m", ctx, key, "some-value")

				Expect(got).To(Equal([]interface{}{ctx, key, "some-value"}))
			},
			Entry("tokenID — carries only a jti from jwtauth v1.1.1", "tokenID"),
			Entry("token_id — carries only a jti from jwtauth v1.1.1", "token_id"),
			Entry("tokenRef", "tokenRef"),
			Entry("userID", "userID"),
		)

		DescribeTable("redacts on every log level",
			func(call func(a *obs.LibraryLoggerAdapter, kv ...interface{}), expect func(dst *[]interface{})) {
				var got []interface{}
				expect(&got)

				call(sut, ctx, "token", secretValue)

				Expect(got).To(Equal([]interface{}{ctx, "token", redacted}))
			},
			Entry("Debug",
				func(a *obs.LibraryLoggerAdapter, kv ...interface{}) { a.Debug("m", kv...) },
				func(dst *[]interface{}) {
					mockLogger.EXPECT().Debug(gomock.Any(), "m", gomock.Any()).Do(
						func(_ context.Context, _ string, kv ...interface{}) { *dst = kv })
				}),
			Entry("Info",
				func(a *obs.LibraryLoggerAdapter, kv ...interface{}) { a.Info("m", kv...) },
				func(dst *[]interface{}) { captureInfo("m", dst) }),
			Entry("Warn",
				func(a *obs.LibraryLoggerAdapter, kv ...interface{}) { a.Warn("m", kv...) },
				func(dst *[]interface{}) {
					mockLogger.EXPECT().Warn(gomock.Any(), "m", gomock.Any()).Do(
						func(_ context.Context, _ string, kv ...interface{}) { *dst = kv })
				}),
			Entry("Error",
				func(a *obs.LibraryLoggerAdapter, kv ...interface{}) { a.Error("m", kv...) },
				func(dst *[]interface{}) {
					mockLogger.EXPECT().Error(gomock.Any(), "m", gomock.Any()).Do(
						func(_ context.Context, _ string, kv ...interface{}) { *dst = kv })
				}),
		)

		Context("With", func() {
			It("redacts deny-listed values before binding fields", func() {
				// ===== STEP 1: Capture With-bound fields =====
				var bound []interface{}
				mockLogger.EXPECT().With(gomock.Any()).DoAndReturn(func(kv ...interface{}) obs.Logger {
					bound = kv
					return mockLogger
				})

				// ===== STEP 2: Bind and log through the derived adapter =====
				child := sut.With("tenant", "t-1", "token", secretValue)
				var got []interface{}
				captureInfo("m", &got)
				child.Info("m", ctx, "cursor", secretCursor)

				// ===== STEP 3: Assert both bound and per-call values are redacted =====
				Expect(bound).To(Equal([]interface{}{"tenant", "t-1", "token", redacted}))
				Expect(got).To(Equal([]interface{}{ctx, "cursor", redacted}))
			})
		})

		Context("caller slice", func() {
			It("is not modified when a value is redacted", func() {
				args := []interface{}{ctx, "token", secretValue, "key", secretValue}
				var got []interface{}
				captureInfo("m", &got)

				sut.Info("m", args...)

				Expect(args).To(Equal([]interface{}{ctx, "token", secretValue, "key", secretValue}))
				Expect(got).To(Equal([]interface{}{ctx, "token", redacted, "key", redacted}))
			})

			It("is not modified by With", func() {
				args := []interface{}{"token", secretValue}
				mockLogger.EXPECT().With(gomock.Any()).Return(mockLogger)

				sut.With(args...)

				Expect(args).To(Equal([]interface{}{"token", secretValue}))
			})
		})
	})

	// ===== PHASE 6: Edge Cases =====
	Describe("Phase 6: Edge Cases", func() {
		DescribeTable("odd-length argument lists do not panic and forward every element in order",
			func(args ...interface{}) {
				var got []interface{}
				captureInfo("m", &got)

				Expect(func() { sut.Info("m", args...) }).NotTo(Panic())

				Expect(got).To(HaveLen(len(args)))
			},
			Entry("trailing deny-listed key without a value, no context", "token"),
			Entry("pair followed by a trailing deny-listed key, no context", "userID", "u", "token"),
			Entry("context followed by a trailing deny-listed key", context.Background(), "token"),
		)

		It("forwards an empty argument list unchanged", func() {
			var got []interface{}
			captureInfo("m", &got)

			Expect(func() { sut.Info("m") }).NotTo(Panic())

			Expect(got).To(BeEmpty())
		})

		It("redacts the value of a complete pair when a trailing key follows", func() {
			var got []interface{}
			captureInfo("m", &got)

			sut.Info("m", ctx, "token", secretValue, "key")

			Expect(got).To(Equal([]interface{}{ctx, "token", redacted, "key"}))
		})
	})
})

var _ = Describe("LibraryOtelSpan — credential redaction", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		exp    *tracetest.InMemoryExporter
		tracer *obs.LibraryOtelTracer
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		exp = tracetest.NewInMemoryExporter()
		tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp))
		tracer = obs.NewLibraryOtelTracer(tp.Tracer("test"))
	})

	AfterEach(func() {
		cancel()
	})

	// exportedAttrs returns the attributes of the single exported span as a key → value map.
	exportedAttrs := func() map[string]string {
		spans := exp.GetSpans()
		Expect(spans).To(HaveLen(1))
		out := make(map[string]string)
		for _, kv := range spans[0].Attributes {
			out[string(kv.Key)] = kv.Value.Emit()
		}
		return out
	}

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {
		DescribeTable("SetAttribute redacts every deny-listed key",
			func(key string, value interface{}) {
				_, span := tracer.Start(ctx, "test-span")
				span.SetAttribute(key, value)
				span.End()

				Expect(exportedAttrs()).To(Equal(map[string]string{key: redacted}))
			},
			Entry("token with a string value", "token", secretValue),
			Entry("key with a string value", "key", "refresh_token:"+secretValue),
			Entry("cursor with a string value", "cursor", secretCursor),
			Entry("next_cursor with a string value", "next_cursor", secretCursor),
			Entry("cursor with a non-string value", "cursor", uint64(42)),
		)

		DescribeTable("SetAttribute leaves non-deny-listed keys untouched",
			func(key string) {
				_, span := tracer.Start(ctx, "test-span")
				span.SetAttribute(key, "some-value")
				span.End()

				Expect(exportedAttrs()).To(Equal(map[string]string{key: "some-value"}))
			},
			Entry("tokenID", "tokenID"),
			Entry("token_id", "token_id"),
			Entry("token_ref", "token_ref"),
		)

		It("SetAttributes redacts deny-listed keys, keeps others, and does not modify the caller's map", func() {
			// ===== STEP 1: Set mixed attributes =====
			attrs := map[string]interface{}{
				"token":     secretValue,
				"cursor":    secretCursor,
				"token_ref": "5d699dd34a86ef68",
				"count":     3,
			}
			_, span := tracer.Start(ctx, "test-span")
			span.SetAttributes(attrs)
			span.End()

			// ===== STEP 2: Assert exported values =====
			Expect(exportedAttrs()).To(Equal(map[string]string{
				"token":     redacted,
				"cursor":    redacted,
				"token_ref": "5d699dd34a86ef68",
				"count":     "3",
			}))

			// ===== STEP 3: Assert caller map unchanged =====
			Expect(attrs).To(HaveKeyWithValue("token", secretValue))
			Expect(attrs).To(HaveKeyWithValue("cursor", secretCursor))
		})

		It("exports string-typed redacted attributes", func() {
			_, span := tracer.Start(ctx, "test-span")
			span.SetAttribute("cursor", uint64(42))
			span.End()

			spans := exp.GetSpans()
			Expect(spans).To(HaveLen(1))
			Expect(spans[0].Attributes).To(ContainElement(attribute.String("cursor", redacted)))
		})
	})
})
