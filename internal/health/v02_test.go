package health_test

import (
	"context"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/aetomala/jwtauth/pkg/keys"
	"github.com/aetomala/token-engine/internal/health"
	"github.com/aetomala/token-engine/internal/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redis/go-redis/v9"
	"go.uber.org/mock/gomock"
)

var _ = Describe("RedisChecker", func() {
	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: RedisChecker", func() {
		var (
			ctrl *gomock.Controller
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
		})

		AfterEach(func() {
			ctrl.Finish()
		})

		Context("Name()", func() {
			It("returns 'redis'", func() {
				checker := health.NewRedisChecker(nil)
				Expect(checker.Name()).To(Equal(health.CheckerNameRedis))
			})
		})

		Context("when Redis PING succeeds", func() {
			It("returns nil", func() {
				mr, err := miniredis.Run()
				Expect(err).NotTo(HaveOccurred())
				DeferCleanup(mr.Close)

				client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
				checker := health.NewRedisChecker(client)

				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				Expect(checker.Check(ctx)).To(BeNil())
			})
		})

		Context("when Redis PING fails", func() {
			It("returns a non-nil error", func() {
				client := redis.NewClient(&redis.Options{Addr: "127.0.0.1:1"})
				checker := health.NewRedisChecker(client)

				ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
				defer cancel()

				Expect(checker.Check(ctx)).NotTo(BeNil())
			})
		})
	})
})

var _ = Describe("KeyAvailabilityChecker", func() {
	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: KeyAvailabilityChecker", func() {
		var (
			ctrl   *gomock.Controller
			mockKM *testutil.MockKeyManager
		)

		BeforeEach(func() {
			ctrl = gomock.NewController(GinkgoT())
			mockKM = testutil.NewMockKeyManager(ctrl)
		})

		AfterEach(func() {
			ctrl.Finish()
		})

		Context("Name()", func() {
			It("returns 'key_availability'", func() {
				checker := health.NewKeyAvailabilityChecker(mockKM)
				Expect(checker.Name()).To(Equal(health.CheckerNameKeyAvailability))
			})
		})

		Context("when GetAllKeyInfo returns a non-empty slice", func() {
			It("returns nil", func() {
				checker := health.NewKeyAvailabilityChecker(mockKM)
				mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{{KeyID: "key1"}}, nil)

				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				Expect(checker.Check(ctx)).To(BeNil())
			})
		})

		Context("when GetAllKeyInfo returns an empty slice", func() {
			It("returns a non-nil error", func() {
				checker := health.NewKeyAvailabilityChecker(mockKM)
				mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return([]keys.KeyInfo{}, nil)

				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				Expect(checker.Check(ctx)).NotTo(BeNil())
			})
		})

		Context("when GetAllKeyInfo returns an error", func() {
			It("returns a non-nil error", func() {
				checker := health.NewKeyAvailabilityChecker(mockKM)
				mockKM.EXPECT().GetAllKeyInfo(gomock.Any()).Return(nil, keys.ErrManagerNotRunning)

				ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()

				Expect(checker.Check(ctx)).NotTo(BeNil())
			})
		})
	})
})
