package reconciliation_test

import (
	"context"
	"errors"
	"time"

	"github.com/aetomala/jwtauth/pkg/tokens"
	. "github.com/aetomala/token-engine/internal/reconciliation"
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("Reconciler", func() {
	var (
		ctx        context.Context
		cancel     context.CancelFunc
		ctrl       *gomock.Controller
		mockLocker *testutil.MockLocker
		mockLock   *testutil.MockLock
		mockTM     *testutil.MockTokenManager
		sut        *CursorReconciler
	)
	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockLocker = testutil.NewMockLocker(ctrl)
		mockLock = testutil.NewMockLock(ctrl)
		mockTM = testutil.NewMockTokenManager(ctrl)
	})
	AfterEach(func() { cancel(); ctrl.Finish() })

	// ===== NoOpReconciler =====
	Describe("NoOpReconciler", func() {
		// ===== PHASE 3: Core Operations =====
		Describe("Phase 3: Core Operations", func() {
			Context("Run", func() {
				It("returns nil immediately", func() {
					noopSut := NewNoOpReconciler()

					err := noopSut.Run(ctx)

					Expect(err).To(BeNil())
				})

				It("returns nil when context is already cancelled", func() {
					noopSut := NewNoOpReconciler()

					cancelledCtx, cancelFunc := context.WithCancel(ctx)
					cancelFunc()

					err := noopSut.Run(cancelledCtx)

					Expect(err).To(BeNil())
				})
			})

			Context("LastSuccessAt", func() {
				It("returns a time within one second of now — no-op is always healthy", func() {
					noopSut := NewNoOpReconciler()

					Expect(noopSut.LastSuccessAt()).To(BeTemporally("~", time.Now(), time.Second))
				})
			})
		})
	})

	// ===== PHASE 4: CursorReconciler =====
	Describe("NewCursorReconciler", func() {
		It("returns a non-nil CursorReconciler", func() {
			result := NewCursorReconciler(
				map[string]tokens.TokenManager{"tenant1": mockTM},
				mockLocker,
				observability.NewNoOpLogger(), observability.NewNoOpMetrics(),
				30*time.Second,
			)
			Expect(result).NotTo(BeNil())
		})
		It("satisfies the Reconciler interface", func() {
			var _ Reconciler = (*CursorReconciler)(nil)
		})
		It("initializes LastSuccessAt to a recent time — startup grace window begins at construction", func() {
			result := NewCursorReconciler(
				map[string]tokens.TokenManager{"tenant1": mockTM},
				mockLocker,
				observability.NewNoOpLogger(), observability.NewNoOpMetrics(),
				30*time.Second,
			)
			Expect(result.LastSuccessAt()).To(BeTemporally("~", time.Now(), time.Second))
		})
	})

	Describe("CursorReconciler.Run", func() {
		Context("when tenantRegistry is empty", func() {
			It("returns nil without calling CleanupExpiredTokens", func() {
				emptySut := NewCursorReconciler(
					map[string]tokens.TokenManager{},
					mockLocker,
					observability.NewNoOpLogger(), observability.NewNoOpMetrics(),
					30*time.Second,
				)
				mockTM.EXPECT().CleanupExpiredTokens(gomock.Any()).Times(0)
				err := emptySut.Run(ctx)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when lock acquisition fails for a tenant", func() {
			It("skips tenant, returns no error", func() {
				sut = NewCursorReconciler(
					map[string]tokens.TokenManager{"tenant1": mockTM},
					mockLocker,
					observability.NewNoOpLogger(), observability.NewNoOpMetrics(),
					30*time.Second,
				)
				mockLocker.EXPECT().Acquire(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("lock held"))
				mockTM.EXPECT().CleanupExpiredTokens(gomock.Any()).Times(0)
				err := sut.Run(ctx)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when lock is acquired", func() {
			It("calls CleanupExpiredTokens exactly once and releases the lock", func() {
				sut = NewCursorReconciler(
					map[string]tokens.TokenManager{"tenant1": mockTM},
					mockLocker,
					observability.NewNoOpLogger(), observability.NewNoOpMetrics(),
					30*time.Second,
				)
				mockLocker.EXPECT().Acquire(gomock.Any(), "locks:reconciliation:tenant1", 30*time.Second).Return(mockLock, nil)
				mockTM.EXPECT().CleanupExpiredTokens(gomock.Any()).Return(0, nil).Times(1)
				mockLock.EXPECT().Release(gomock.Any()).Return(nil)
				err := sut.Run(ctx)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when ctx is cancelled before processing", func() {
			It("returns nil without processing tenants", func() {
				cancelledCtx, cancelFn := context.WithCancel(context.Background())
				cancelFn()
				emptySut := NewCursorReconciler(
					map[string]tokens.TokenManager{"tenant1": mockTM},
					mockLocker,
					observability.NewNoOpLogger(), observability.NewNoOpMetrics(),
					30*time.Second,
				)
				mockTM.EXPECT().CleanupExpiredTokens(gomock.Any()).Times(0)
				err := emptySut.Run(cancelledCtx)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when CleanupExpiredTokens returns an error", func() {
			It("logs warn, releases lock, does not propagate the error", func() {
				sut = NewCursorReconciler(
					map[string]tokens.TokenManager{"tenant1": mockTM},
					mockLocker,
					observability.NewNoOpLogger(), observability.NewNoOpMetrics(),
					30*time.Second,
				)
				mockLocker.EXPECT().Acquire(gomock.Any(), gomock.Any(), gomock.Any()).Return(mockLock, nil)
				mockTM.EXPECT().CleanupExpiredTokens(gomock.Any()).Return(0, errors.New("store error"))
				mockLock.EXPECT().Release(gomock.Any()).Return(nil)
				err := sut.Run(ctx)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("LastSuccessAt after a successful Run", func() {
			It("advances after Run completes all tenants", func() {
				sut = NewCursorReconciler(
					map[string]tokens.TokenManager{"tenant1": mockTM},
					mockLocker,
					observability.NewNoOpLogger(), observability.NewNoOpMetrics(),
					30*time.Second,
				)
				before := sut.LastSuccessAt()
				mockLocker.EXPECT().Acquire(gomock.Any(), "locks:reconciliation:tenant1", 30*time.Second).Return(mockLock, nil)
				mockTM.EXPECT().CleanupExpiredTokens(gomock.Any()).Return(0, nil)
				mockLock.EXPECT().Release(gomock.Any()).Return(nil)

				err := sut.Run(ctx)

				Expect(err).NotTo(HaveOccurred())
				Expect(sut.LastSuccessAt()).To(BeTemporally(">=", before))
				Expect(sut.LastSuccessAt()).To(BeTemporally("~", time.Now(), time.Second))
			})
		})
	})
})
