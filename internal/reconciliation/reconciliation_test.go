package reconciliation_test

import (
	"context"
	"time"

	"github.com/aetomala/token-engine/internal/reconciliation"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NoOpReconciler", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
	})

	AfterEach(func() {
		cancel()
	})

	Context("Run", func() {
		It("returns nil immediately", func() {
			sut := reconciliation.NewNoOpReconciler()

			err := sut.Run(ctx)

			Expect(err).To(BeNil())
		})

		It("returns nil when context is already cancelled", func() {
			sut := reconciliation.NewNoOpReconciler()

			cancelledCtx, cancelFunc := context.WithCancel(ctx)
			cancelFunc()

			err := sut.Run(cancelledCtx)

			Expect(err).To(BeNil())
		})
	})
})
