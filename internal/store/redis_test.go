package store_test

import (
	"context"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/aetomala/token-engine/internal/store"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/redis/go-redis/v9"
)

var _ = Describe("RedisIdempotencyStore", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		mr     *miniredis.Miniredis
		client *redis.Client
		sut    *store.RedisIdempotencyStore
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		var err error
		mr, err = miniredis.Run()
		Expect(err).NotTo(HaveOccurred())
		client = redis.NewClient(&redis.Options{Addr: mr.Addr()})
		sut = store.NewRedisIdempotencyStore(client, 1*time.Minute)
	})

	AfterEach(func() {
		cancel()
		mr.Close()
	})

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {

		Context("when key exists in Redis", func() {
			It("returns the stored bytes, true, and nil error", func() {
				mr.Set("testkey", "testvalue")

				val, hit, err := sut.Get(ctx, "testkey")

				Expect(err).NotTo(HaveOccurred())
				Expect(hit).To(BeTrue())
				Expect(val).To(Equal([]byte("testvalue")))
			})
		})

		Context("when key does not exist (redis.Nil)", func() {
			It("returns nil, false, and nil error", func() {
				val, hit, err := sut.Get(ctx, "missing-key")

				Expect(err).NotTo(HaveOccurred())
				Expect(hit).To(BeFalse())
				Expect(val).To(BeNil())
			})

			It("does not treat redis.Nil as an error", func() {
				_, _, err := sut.Get(ctx, "another-missing-key")

				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("when Redis returns a non-Nil error", func() {
			It("returns nil, false, and the error", func() {
				// Use a separate miniredis instance so closing it does not affect the shared mr
				errorMr, err := miniredis.Run()
				Expect(err).NotTo(HaveOccurred())
				errorClient := redis.NewClient(&redis.Options{Addr: errorMr.Addr()})
				errorSut := store.NewRedisIdempotencyStore(errorClient, 1*time.Minute)
				errorMr.Close() // Close before the call to force a connection error

				val, hit, getErr := errorSut.Get(ctx, "key")

				Expect(val).To(BeNil())
				Expect(hit).To(BeFalse())
				Expect(getErr).To(HaveOccurred())
			})
		})

		Context("when key does not exist", func() {
			It("stores the value in Redis and returns true, nil", func() {
				ok, err := sut.SetNX(ctx, "newkey", []byte("newvalue"))

				Expect(err).NotTo(HaveOccurred())
				Expect(ok).To(BeTrue())

				stored, _ := mr.Get("newkey")
				Expect(stored).To(Equal("newvalue"))
			})

			It("applies the TTL configured at construction", func() {
				_, err := sut.SetNX(ctx, "ttlkey", []byte("v"))
				Expect(err).NotTo(HaveOccurred())

				ttl := mr.TTL("ttlkey")
				Expect(ttl).To(BeNumerically(">", 0))
			})
		})

		Context("when key already exists", func() {
			It("returns false, nil and does not overwrite the existing value", func() {
				mr.Set("existingkey", "original")

				ok, err := sut.SetNX(ctx, "existingkey", []byte("overwrite"))

				Expect(err).NotTo(HaveOccurred())
				Expect(ok).To(BeFalse())

				stored, _ := mr.Get("existingkey")
				Expect(stored).To(Equal("original"))
			})
		})

		Context("when Redis returns an error on SetNX", func() {
			It("returns false and the error", func() {
				// Use a separate miniredis instance so closing it does not affect the shared mr
				errorMr, err := miniredis.Run()
				Expect(err).NotTo(HaveOccurred())
				errorClient := redis.NewClient(&redis.Options{Addr: errorMr.Addr()})
				errorSut := store.NewRedisIdempotencyStore(errorClient, 1*time.Minute)
				errorMr.Close() // Close before the call to force a connection error

				ok, setErr := errorSut.SetNX(ctx, "key", []byte("v"))

				Expect(ok).To(BeFalse())
				Expect(setErr).To(HaveOccurred())
			})
		})
	})
})

var _ = Describe("NoOpIdempotencyStore", func() {
	var (
		ctx    context.Context
		cancel context.CancelFunc
		sut    *store.NoOpIdempotencyStore
	)

	BeforeEach(func() {
		ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
		sut = store.NewNoOpIdempotencyStore()
	})

	AfterEach(func() {
		cancel()
	})

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {
		Context("Get", func() {
			It("always returns nil, false, nil", func() {
				val, hit, err := sut.Get(ctx, "any-key")

				Expect(val).To(BeNil())
				Expect(hit).To(BeFalse())
				Expect(err).NotTo(HaveOccurred())
			})
		})

		Context("SetNX", func() {
			It("always returns true, nil", func() {
				ok, err := sut.SetNX(ctx, "any-key", []byte("v"))

				Expect(ok).To(BeTrue())
				Expect(err).NotTo(HaveOccurred())
			})
		})
	})
})
