package registry_test

import (
	"context"

	"github.com/aetomala/token-engine/internal/registry"
	"github.com/aetomala/token-engine/internal/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ = Describe("StaticTenantRegistry", func() {
	var (
		ctx  context.Context
		ctrl *gomock.Controller
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("Get with blank tenant_id (empty string)", func() {
		It("returns codes.Unimplemented in v0.1", func() {
			mockLogger := testutil.NewMockLogger(ctrl)
			sut := registry.NewStaticTenantRegistry(nil, mockLogger)

			manager, err := sut.Get(ctx, "")

			Expect(manager).To(BeNil())
			Expect(err).NotTo(BeNil())
			Expect(status.Code(err)).To(Equal(codes.Unimplemented))
		})
	})

	Context("Get with non-blank tenant_id", func() {
		It("returns codes.Unimplemented in v0.1", func() {
			mockLogger := testutil.NewMockLogger(ctrl)
			sut := registry.NewStaticTenantRegistry(nil, mockLogger)

			manager, err := sut.Get(ctx, "tenant-1")

			Expect(manager).To(BeNil())
			Expect(err).NotTo(BeNil())
			Expect(status.Code(err)).To(Equal(codes.Unimplemented))
		})
	})
})

var _ = Describe("StaticCallerRegistry", func() {
	var (
		ctx  context.Context
		ctrl *gomock.Controller
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("IsPermitted", func() {
		It("returns true for all inputs in v0.1 stub", func() {
			mockLogger := testutil.NewMockLogger(ctrl)
			sut := registry.NewStaticCallerRegistry(mockLogger)

			permitted, err := sut.IsPermitted(ctx, "caller-1", "tenant-1")

			Expect(err).To(BeNil())
			Expect(permitted).To(BeTrue())
		})

		It("returns true for blank inputs in v0.1 stub", func() {
			mockLogger := testutil.NewMockLogger(ctrl)
			sut := registry.NewStaticCallerRegistry(mockLogger)

			permitted, err := sut.IsPermitted(ctx, "", "")

			Expect(err).To(BeNil())
			Expect(permitted).To(BeTrue())
		})
	})
})
