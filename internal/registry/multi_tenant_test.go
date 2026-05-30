package registry_test

import (
	"context"
	"time"

	"github.com/alicebob/miniredis/v2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/registry"
)

var _ = Describe("MultiTenantRegistry", func() {
	var (
		ctx     context.Context
		cancel  context.CancelFunc
		mr      *miniredis.Miniredis
		client  *redis.Client
		promReg *prometheus.Registry
		logger  observability.Logger
		tracer  observability.Tracer
		metrics observability.Metrics
		reg     *registry.MultiTenantRegistry
	)

	BeforeEach(func() {
		var err error
		ctx, cancel = context.WithTimeout(context.Background(), 30*time.Second)
		mr, err = miniredis.Run()
		Expect(err).NotTo(HaveOccurred())
		client = redis.NewClient(&redis.Options{Addr: mr.Addr()})
		promReg = prometheus.NewRegistry()
		logger = observability.NewNoOpLogger()
		tracer = observability.NewNoOpTracer()
		metrics = observability.NewNoOpMetrics()
		reg = registry.NewMultiTenantRegistry(client, promReg, logger, tracer, metrics)
	})

	AfterEach(func() {
		cancel()
		_ = client.Close()
		mr.Close()
	})

	// ===== PHASE 1: Constructor and Initialization =====
	Describe("Phase 1: Constructor and Initialization", func() {
		Context("when constructed with valid dependencies", func() {
			It("returns a non-nil registry", func() {
				Expect(reg).NotTo(BeNil())
			})

			It("starts with no tenants — AllKeyManagers returns empty map", func() {
				kms := reg.AllKeyManagers()
				Expect(kms).To(BeEmpty())
			})
		})
	})

	// ===== PHASE 3: Core Operations =====

	Describe("Phase 3: Core Operations — Add", func() {
		Context("when tenant is not registered", func() {
			It("constructs the full per-tenant stack and returns nil error", func() {
				err := reg.Add(ctx, "tenant1", registry.TenantConfig{Issuer: "tenant1", Audience: "api"})
				Expect(err).NotTo(HaveOccurred())
			})

			It("makes the tenant available via Get", func() {
				err := reg.Add(ctx, "tenant1", registry.TenantConfig{Issuer: "tenant1", Audience: "api"})
				Expect(err).NotTo(HaveOccurred())

				manager, getErr := reg.Get(ctx, "tenant1")
				Expect(getErr).NotTo(HaveOccurred())
				Expect(manager).NotTo(BeNil())
			})

			It("emits metrics without panicking", func() {
				Expect(func() {
					_ = reg.Add(ctx, "tenant1", registry.TenantConfig{Issuer: "tenant1", Audience: "api"})
				}).NotTo(Panic())
			})
		})

		Context("when tenant is already registered", func() {
			It("returns codes.AlreadyExists", func() {
				Expect(reg.Add(ctx, "tenant1", registry.TenantConfig{Issuer: "tenant1", Audience: "api"})).To(Succeed())

				err := reg.Add(ctx, "tenant1", registry.TenantConfig{Issuer: "tenant1", Audience: "api"})
				Expect(status.Code(err)).To(Equal(codes.AlreadyExists))
			})
		})

		Context("when tenantID is empty", func() {
			It("returns codes.InvalidArgument", func() {
				err := reg.Add(ctx, "", registry.TenantConfig{Issuer: "tenant1", Audience: "api"})
				Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			})
		})

		Context("when cfg.Issuer is empty", func() {
			It("returns codes.InvalidArgument", func() {
				err := reg.Add(ctx, "tenant1", registry.TenantConfig{Issuer: "", Audience: "api"})
				Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			})
		})

		Context("when cfg.Audience is empty", func() {
			It("returns codes.InvalidArgument", func() {
				err := reg.Add(ctx, "tenant1", registry.TenantConfig{Issuer: "tenant1", Audience: ""})
				Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			})
		})

		Context("when stack construction fails", func() {
			It("returns a wrapped error and does not register the tenant", func() {
				badClient := redis.NewClient(&redis.Options{Addr: "localhost:1"}) // port 1 never open
				defer badClient.Close()
				badReg := registry.NewMultiTenantRegistry(badClient, prometheus.NewRegistry(),
					observability.NewNoOpLogger(), observability.NewNoOpTracer(), observability.NewNoOpMetrics())

				shortCtx, shortCancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer shortCancel()

				err := badReg.Add(shortCtx, "tenant1", registry.TenantConfig{Issuer: "tenant1", Audience: "api"})
				Expect(err).To(HaveOccurred())

				_, getErr := badReg.Get(context.Background(), "tenant1")
				Expect(status.Code(getErr)).To(Equal(codes.NotFound))
			})
		})
	})

	Describe("Phase 3: Core Operations — Drain", func() {
		BeforeEach(func() {
			Expect(reg.Add(ctx, "tenant1", registry.TenantConfig{Issuer: "tenant1", Audience: "api"})).To(Succeed())
		})

		Context("when tenant is registered and active", func() {
			It("returns nil error", func() {
				Expect(reg.Drain(ctx, "tenant1")).To(Succeed())
			})

			It("emits metrics without panicking", func() {
				Expect(func() {
					_ = reg.Drain(ctx, "tenant1")
				}).NotTo(Panic())
			})
		})

		Context("after Drain — Get", func() {
			It("returns codes.NotFound", func() {
				Expect(reg.Drain(ctx, "tenant1")).To(Succeed())

				_, err := reg.Get(ctx, "tenant1")
				Expect(status.Code(err)).To(Equal(codes.NotFound))
			})
		})

		Context("when tenantID is unknown", func() {
			It("returns codes.NotFound", func() {
				err := reg.Drain(ctx, "unknown-tenant")
				Expect(status.Code(err)).To(Equal(codes.NotFound))
			})
		})

		Context("when tenantID is empty", func() {
			It("returns codes.InvalidArgument", func() {
				err := reg.Drain(ctx, "")
				Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			})
		})
	})

	Describe("Phase 3: Core Operations — Remove", func() {
		BeforeEach(func() {
			Expect(reg.Add(ctx, "tenant1", registry.TenantConfig{Issuer: "tenant1", Audience: "api"})).To(Succeed())
		})

		Context("when tenant is drained", func() {
			BeforeEach(func() {
				Expect(reg.Drain(ctx, "tenant1")).To(Succeed())
			})

			It("returns nil error", func() {
				Expect(reg.Remove(ctx, "tenant1")).To(Succeed())
			})

			It("removes the entry — subsequent Get returns codes.NotFound", func() {
				Expect(reg.Remove(ctx, "tenant1")).To(Succeed())

				_, err := reg.Get(ctx, "tenant1")
				Expect(status.Code(err)).To(Equal(codes.NotFound))
			})

			It("emits metrics without panicking", func() {
				Expect(func() {
					_ = reg.Remove(ctx, "tenant1")
				}).NotTo(Panic())
			})
		})

		Context("when tenant is active — not drained", func() {
			It("returns codes.FailedPrecondition", func() {
				err := reg.Remove(ctx, "tenant1")
				Expect(status.Code(err)).To(Equal(codes.FailedPrecondition))
			})

			It("does not remove the entry — subsequent Get still returns the manager", func() {
				_ = reg.Remove(ctx, "tenant1")

				manager, err := reg.Get(ctx, "tenant1")
				Expect(err).NotTo(HaveOccurred())
				Expect(manager).NotTo(BeNil())
			})
		})

		Context("when tenantID is unknown", func() {
			It("returns codes.NotFound", func() {
				err := reg.Remove(ctx, "unknown-tenant")
				Expect(status.Code(err)).To(Equal(codes.NotFound))
			})
		})

		Context("when tenantID is empty", func() {
			It("returns codes.InvalidArgument", func() {
				err := reg.Remove(ctx, "")
				Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			})
		})
	})

	Describe("Phase 3: Core Operations — Get", func() {
		BeforeEach(func() {
			Expect(reg.Add(ctx, "tenant1", registry.TenantConfig{Issuer: "tenant1", Audience: "api"})).To(Succeed())
		})

		Context("when tenant is active", func() {
			It("returns the TokenManager — not nil", func() {
				manager, err := reg.Get(ctx, "tenant1")
				Expect(err).NotTo(HaveOccurred())
				Expect(manager).NotTo(BeNil())
			})
		})

		Context("when tenant is draining", func() {
			It("returns codes.NotFound", func() {
				Expect(reg.Drain(ctx, "tenant1")).To(Succeed())

				_, err := reg.Get(ctx, "tenant1")
				Expect(status.Code(err)).To(Equal(codes.NotFound))
			})
		})

		Context("when tenantID is unknown", func() {
			It("returns codes.NotFound", func() {
				_, err := reg.Get(ctx, "unknown-tenant")
				Expect(status.Code(err)).To(Equal(codes.NotFound))
			})
		})

		Context("when tenantID is empty", func() {
			It("returns codes.InvalidArgument", func() {
				_, err := reg.Get(ctx, "")
				Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
			})
		})
	})

	Describe("Phase 3: Core Operations — AllKeyManagers", func() {
		Context("with tenants registered", func() {
			It("returns a map containing registered tenants", func() {
				Expect(reg.Add(ctx, "tenant1", registry.TenantConfig{Issuer: "tenant1", Audience: "api"})).To(Succeed())

				kms := reg.AllKeyManagers()
				Expect(kms).To(HaveKey("tenant1"))
				Expect(kms["tenant1"]).NotTo(BeNil())
			})

			It("returns a snapshot — map mutations do not affect the registry", func() {
				Expect(reg.Add(ctx, "tenant1", registry.TenantConfig{Issuer: "tenant1", Audience: "api"})).To(Succeed())

				kms := reg.AllKeyManagers()
				Expect(kms).To(HaveKey("tenant1"))

				// Mutate the returned map
				delete(kms, "tenant1")

				// Registry is unaffected
				kms2 := reg.AllKeyManagers()
				Expect(kms2).To(HaveKey("tenant1"))
			})
		})

		Context("with no tenants registered", func() {
			It("returns an empty map", func() {
				kms := reg.AllKeyManagers()
				Expect(kms).To(BeEmpty())
			})
		})
	})

	// ===== PHASE 4: Error Cases =====
	Describe("Phase 4: Error Cases", func() {
		Context("when adding two tenants with different IDs", func() {
			It("both are accessible via Get", func() {
				Expect(reg.Add(ctx, "tenant1", registry.TenantConfig{Issuer: "tenant1", Audience: "api"})).To(Succeed())

				ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel2()
				Expect(reg.Add(ctx2, "tenant2", registry.TenantConfig{Issuer: "tenant2", Audience: "api"})).To(Succeed())

				m1, err1 := reg.Get(ctx, "tenant1")
				Expect(err1).NotTo(HaveOccurred())
				Expect(m1).NotTo(BeNil())

				m2, err2 := reg.Get(ctx, "tenant2")
				Expect(err2).NotTo(HaveOccurred())
				Expect(m2).NotTo(BeNil())
			})
		})

		Context("when draining a tenant that is already draining", func() {
			It("returns nil error — drain is idempotent", func() {
				Expect(reg.Add(ctx, "tenant1", registry.TenantConfig{Issuer: "tenant1", Audience: "api"})).To(Succeed())
				Expect(reg.Drain(ctx, "tenant1")).To(Succeed())
				Expect(reg.Drain(ctx, "tenant1")).To(Succeed())
			})
		})

		Context("when removing a tenant that does not exist", func() {
			It("returns codes.NotFound", func() {
				err := reg.Remove(ctx, "nonexistent")
				Expect(status.Code(err)).To(Equal(codes.NotFound))
			})
		})
	})
})
