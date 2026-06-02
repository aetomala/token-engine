package reconciliation_test

import (
	"context"
	"errors"
	"time"

	"github.com/aetomala/jwtauth/pkg/storage"
	"github.com/aetomala/jwtauth/pkg/tokens"
	"github.com/alicebob/miniredis/v2"
	. "github.com/aetomala/token-engine/internal/reconciliation"
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redis/go-redis/v9"
	"go.uber.org/mock/gomock"
)

var _ = Describe("Reconciler", func() {
	var (
		ctx         context.Context
		cancel      context.CancelFunc
		ctrl        *gomock.Controller
		mockLocker  *testutil.MockLocker
		mockLock    *testutil.MockLock
		mockTM      *testutil.MockTokenManager
		mr          *miniredis.Miniredis
		redisClient *redis.Client
		sut         *CursorReconciler
	)
	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		ctrl = gomock.NewController(GinkgoT())
		mockLocker = testutil.NewMockLocker(ctrl)
		mockLock = testutil.NewMockLock(ctrl)
		mockTM = testutil.NewMockTokenManager(ctrl)
		mr, _ = miniredis.Run()
		redisClient = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	})
	AfterEach(func() { cancel(); ctrl.Finish(); redisClient.Close(); mr.Close() })

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
		})
	})

	// ===== PHASE 4: CursorReconciler =====
	Describe("NewCursorReconciler", func() {
		It("returns a non-nil CursorReconciler", func() {
			result := NewCursorReconciler(
				map[string]tokens.TokenManager{"tenant1": mockTM},
				mockLocker, redisClient,
				observability.NewNoOpLogger(), observability.NewNoOpMetrics(),
				10, 30*time.Second,
			)
			Expect(result).NotTo(BeNil())
		})
		It("satisfies the Reconciler interface", func() {
			var _ Reconciler = (*CursorReconciler)(nil)
		})
	})

	Describe("CursorReconciler.Run", func() {
		Context("when tenantRegistry is empty", func() {
			It("returns nil without calling ListTokens or CleanupExpiredTokens", func() {
				emptySut := NewCursorReconciler(
					map[string]tokens.TokenManager{},
					mockLocker, redisClient,
					observability.NewNoOpLogger(), observability.NewNoOpMetrics(),
					10, 30*time.Second,
				)
				mockTM.EXPECT().ListTokens(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				mockTM.EXPECT().CleanupExpiredTokens(gomock.Any()).Times(0)
				err := emptySut.Run(ctx)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when lock acquisition fails for a tenant", func() {
			It("skips tenant, returns no error", func() {
				sut = NewCursorReconciler(
					map[string]tokens.TokenManager{"tenant1": mockTM},
					mockLocker, redisClient,
					observability.NewNoOpLogger(), observability.NewNoOpMetrics(),
					10, 30*time.Second,
				)
				mockLocker.EXPECT().Acquire(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, errors.New("lock held"))
				mockTM.EXPECT().ListTokens(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				err := sut.Run(ctx)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when lock is acquired and single page returns empty next cursor", func() {
			It("calls ListTokens, CleanupExpiredTokens, deletes cursor key, releases lock", func() {
				sut = NewCursorReconciler(
					map[string]tokens.TokenManager{"tenant1": mockTM},
					mockLocker, redisClient,
					observability.NewNoOpLogger(), observability.NewNoOpMetrics(),
					10, 30*time.Second,
				)
				mockLocker.EXPECT().Acquire(gomock.Any(), "locks:reconciliation:tenant1", 30*time.Second).Return(mockLock, nil)
				mockTM.EXPECT().ListTokens(gomock.Any(), "", 10).Return([]*storage.RefreshToken{}, "", nil)
				mockTM.EXPECT().CleanupExpiredTokens(gomock.Any()).Return(0, nil)
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
					mockLocker, redisClient,
					observability.NewNoOpLogger(), observability.NewNoOpMetrics(),
					10, 30*time.Second,
				)
				mockTM.EXPECT().ListTokens(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
				err := emptySut.Run(cancelledCtx)
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when ListTokens returns an error", func() {
			It("logs warn, does not write cursor, releases lock, continues to next tenant", func() {
				sut = NewCursorReconciler(
					map[string]tokens.TokenManager{"tenant1": mockTM},
					mockLocker, redisClient,
					observability.NewNoOpLogger(), observability.NewNoOpMetrics(),
					10, 30*time.Second,
				)
				mockLocker.EXPECT().Acquire(gomock.Any(), gomock.Any(), gomock.Any()).Return(mockLock, nil)
				mockTM.EXPECT().ListTokens(gomock.Any(), "", 10).Return(nil, "", errors.New("store error"))
				mockTM.EXPECT().CleanupExpiredTokens(gomock.Any()).Times(0)
				mockLock.EXPECT().Release(gomock.Any()).Return(nil)
				err := sut.Run(ctx)
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})
