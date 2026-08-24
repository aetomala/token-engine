package registry_test

import (
	"context"
	"errors"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"

	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/registry"
	"github.com/aetomala/token-engine/internal/testutil"
)

var _ = Describe("RunExpiryIndexBackfill", func() {
	var (
		ctx    context.Context
		ctrl   *gomock.Controller
		logger observability.Logger
	)

	BeforeEach(func() {
		ctx = context.Background()
		ctrl = gomock.NewController(GinkgoT())
		logger = observability.NewNoOpLogger()
	})

	AfterEach(func() {
		ctrl.Finish()
	})

	Context("with an empty map", func() {
		It("does nothing", func() {
			Expect(func() {
				registry.RunExpiryIndexBackfill(ctx, map[string]registry.ExpiryIndexBackfiller{}, logger)
			}).NotTo(Panic())
		})
	})

	Context("when every tenant's backfill succeeds", func() {
		It("calls BackfillExpiryIndex exactly once per tenant", func() {
			mockA := testutil.NewMockExpiryIndexBackfiller(ctrl)
			mockB := testutil.NewMockExpiryIndexBackfiller(ctrl)
			mockA.EXPECT().BackfillExpiryIndex(gomock.Any()).Return(2, 10, nil).Times(1)
			mockB.EXPECT().BackfillExpiryIndex(gomock.Any()).Return(0, 5, nil).Times(1)

			registry.RunExpiryIndexBackfill(ctx, map[string]registry.ExpiryIndexBackfiller{
				"tenant-a": mockA,
				"tenant-b": mockB,
			}, logger)
		})
	})

	Context("when one tenant's backfill fails", func() {
		It("logs the error and still runs the backfill for the remaining tenants", func() {
			mockFailing := testutil.NewMockExpiryIndexBackfiller(ctrl)
			mockSucceeding := testutil.NewMockExpiryIndexBackfiller(ctrl)
			mockFailing.EXPECT().BackfillExpiryIndex(gomock.Any()).Return(0, 0, errors.New("redis error")).Times(1)
			mockSucceeding.EXPECT().BackfillExpiryIndex(gomock.Any()).Return(1, 3, nil).Times(1)

			registry.RunExpiryIndexBackfill(ctx, map[string]registry.ExpiryIndexBackfiller{
				"tenant-failing":    mockFailing,
				"tenant-succeeding": mockSucceeding,
			}, logger)
		})
	})
})
