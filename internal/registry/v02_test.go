package registry_test

import (
	"context"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/aetomala/token-engine/internal/config"
	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/registry"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var _ = Describe("Phase 3: StaticTenantRegistry", func() {
	var (
		ctx     context.Context
		cancel  context.CancelFunc
		mr      *miniredis.Miniredis
		client  *redis.Client
		cfg     *config.Config
		promReg *prometheus.Registry
		logger  observability.Logger
		tracer  observability.Tracer
		metrics observability.Metrics
		reg     *registry.StaticTenantRegistry
	)

	BeforeEach(func() {
		// Allow enough time for RSA key generation during Start.
		ctx, cancel = context.WithTimeout(context.Background(), 15*time.Second)

		var err error
		mr, err = miniredis.Run()
		Expect(err).NotTo(HaveOccurred())

		client = redis.NewClient(&redis.Options{Addr: mr.Addr()})
		cfg = &config.Config{Issuer: "test-tenant", Audience: "api"}
		promReg = prometheus.NewRegistry()
		logger = observability.NewNoOpLogger()
		tracer = observability.NewNoOpTracer()
		metrics = observability.NewNoOpMetrics()

		reg, err = registry.NewStaticTenantRegistry(ctx, client, cfg, promReg, logger, tracer, metrics)
		Expect(err).NotTo(HaveOccurred())
	})

	AfterEach(func() {
		cancel()
		mr.Close()
	})

	Context("Get with empty string tenant_id", func() {
		It("returns codes.InvalidArgument", func() {
			_, err := reg.Get(ctx, "")
			Expect(err).NotTo(BeNil())
			Expect(status.Code(err)).To(Equal(codes.InvalidArgument))
		})
	})

	Context("Get with tenant_id matching cfg.Issuer", func() {
		It("returns the Manager", func() {
			manager, err := reg.Get(ctx, "test-tenant")
			Expect(err).To(BeNil())
			Expect(manager).NotTo(BeNil())
		})

		It("returns nil error", func() {
			_, err := reg.Get(ctx, "test-tenant")
			Expect(err).To(BeNil())
		})
	})

	Context("Get with non-empty tenant_id not matching cfg.Issuer", func() {
		It("returns codes.NotFound", func() {
			_, err := reg.Get(ctx, "other-tenant")
			Expect(err).NotTo(BeNil())
			Expect(status.Code(err)).To(Equal(codes.NotFound))
		})
	})

	Context("KeyManager()", func() {
		It("returns a non-nil KeyManager", func() {
			km := reg.KeyManager()
			Expect(km).NotTo(BeNil())
		})
	})
})

var _ = Describe("Phase 1: StaticTenantRegistry — constructor errors", func() {
	Context("when redis client is nil", func() {
		It("returns an error", func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			cfg := &config.Config{Issuer: "test-tenant", Audience: "api"}
			_, err := registry.NewStaticTenantRegistry(ctx, nil, cfg, prometheus.NewRegistry(),
				observability.NewNoOpLogger(), observability.NewNoOpTracer(), observability.NewNoOpMetrics())

			Expect(err).NotTo(BeNil())
		})
	})
})
