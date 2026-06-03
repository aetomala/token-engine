package lock_test

import (
	"context"
	"time"

	"github.com/alicebob/miniredis/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redis/go-redis/v9"

	. "github.com/aetomala/token-engine/internal/lock"
	"github.com/aetomala/token-engine/internal/observability"
)

var _ = Describe("Lock", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		mr     *miniredis.Miniredis
		client *redis.Client
		locker *RedisLock
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		var err error
		mr, err = miniredis.Run()
		Expect(err).NotTo(HaveOccurred())
		client = redis.NewClient(&redis.Options{Addr: mr.Addr()})
		locker = NewRedisLocker(client, observability.NewNoOpLogger())
	})

	AfterEach(func() {
		cancel()
		client.Close()
		mr.Close()
	})

	// ===== PHASE 1: Constructor and Initialization =====
	Describe("Phase 1: Constructor and Initialization", func() {
		Describe("NewRedisLocker", func() {
			It("returns a non-nil RedisLock", func() {
				Expect(locker).NotTo(BeNil())
			})
		})
	})

	// ===== PHASE 2: Acquire =====
	Describe("Phase 2: Acquire", func() {
		Describe("RedisLock.Acquire", func() {
			Context("when the lock is not held", func() {
				It("acquires the lock and returns a non-nil Lock", func() {
					lock, err := locker.Acquire(ctx, "test-key", 30*time.Second)
					Expect(err).NotTo(HaveOccurred())
					Expect(lock).NotTo(BeNil())
				})
			})

			Context("when the lock is already held", func() {
				It("returns an error", func() {
					_, err := locker.Acquire(ctx, "test-key", 30*time.Second)
					Expect(err).NotTo(HaveOccurred())

					_, err = locker.Acquire(ctx, "test-key", 30*time.Second)
					Expect(err).To(HaveOccurred())
				})
			})

			Context("when Redis is unavailable", func() {
				It("returns an error", func() {
					mr.Close()
					_, err := locker.Acquire(ctx, "test-key", 30*time.Second)
					Expect(err).To(HaveOccurred())
				})
			})

			Context("when the context is cancelled", func() {
				It("returns a context error", func() {
					cancelledCtx, cancelFn := context.WithCancel(context.Background())
					cancelFn()

					_, err := locker.Acquire(cancelledCtx, "test-key", 30*time.Second)
					Expect(err).To(HaveOccurred())
				})
			})
		})
	})

	// ===== PHASE 3: Release =====
	Describe("Phase 3: Release", func() {
		Describe("Lock.Release", func() {
			Context("when the lock is held by this holder", func() {
				It("releases the lock successfully", func() {
					lock, err := locker.Acquire(ctx, "test-key", 30*time.Second)
					Expect(err).NotTo(HaveOccurred())

					err = lock.Release(ctx)
					Expect(err).NotTo(HaveOccurred())

					// Lock can be acquired again after release
					lock2, err := locker.Acquire(ctx, "test-key", 30*time.Second)
					Expect(err).NotTo(HaveOccurred())
					Expect(lock2).NotTo(BeNil())
				})
			})

			Context("when the lock has already expired", func() {
				It("returns nil", func() {
					lock, err := locker.Acquire(ctx, "test-key", 1*time.Millisecond)
					Expect(err).NotTo(HaveOccurred())

					mr.FastForward(1 * time.Second)

					err = lock.Release(ctx)
					Expect(err).NotTo(HaveOccurred())
				})
			})

			Context("when the lock is held by a different holder", func() {
				It("does not release the lock and returns nil", func() {
					lock1, err := locker.Acquire(ctx, "test-key", 30*time.Second)
					Expect(err).NotTo(HaveOccurred())

					// Manually overwrite the key so lock1's token no longer matches
					mr.Set("test-key", "different-token")

					err = lock1.Release(ctx)
					Expect(err).NotTo(HaveOccurred())

					// The key still exists (different holder)
					val, _ := mr.Get("test-key")
					Expect(val).To(Equal("different-token"))
				})
			})

			Context("when Release is called twice", func() {
				It("returns nil on the second call (idempotent)", func() {
					lock, err := locker.Acquire(ctx, "test-key", 30*time.Second)
					Expect(err).NotTo(HaveOccurred())

					err = lock.Release(ctx)
					Expect(err).NotTo(HaveOccurred())

					err = lock.Release(ctx)
					Expect(err).NotTo(HaveOccurred())
				})
			})
		})
	})
})
