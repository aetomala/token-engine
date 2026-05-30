package registry_test

import (
	"context"
	"os"
	"path/filepath"

	"github.com/aetomala/token-engine/internal/observability"
	"github.com/aetomala/token-engine/internal/registry"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// StaticTenantRegistry tests are in PHASE-9C (require miniredis for real Redis-backed construction).

var _ = Describe("StaticCallerRegistry", func() {
	var (
		ctx    context.Context
		logger observability.Logger
	)

	BeforeEach(func() {
		ctx = context.Background()
		logger = observability.NewNoOpLogger()
	})

	// ===== PHASE 3: Core Operations — IsPermitted =====
	Describe("Phase 3: Core Operations — IsPermitted", func() {
		Context("when callerIdentity is in registry and tenantID is permitted", func() {
			It("returns (true, nil)", func() {
				cfg := &registry.CallerRegistryConfig{
					Version: 1,
					Callers: []registry.CallerEntry{
						{Identity: "caller-1", PermittedTenants: []string{"tenant-a", "tenant-b"}},
					},
				}
				sut := registry.NewStaticCallerRegistry(cfg, logger)

				permitted, err := sut.IsPermitted(ctx, "caller-1", "tenant-a")

				Expect(err).To(BeNil())
				Expect(permitted).To(BeTrue())
			})
		})

		Context("when callerIdentity is in registry but tenantID is not in permitted_tenants", func() {
			It("returns (false, codes.PermissionDenied error)", func() {
				cfg := &registry.CallerRegistryConfig{
					Version: 1,
					Callers: []registry.CallerEntry{
						{Identity: "caller-1", PermittedTenants: []string{"tenant-a"}},
					},
				}
				sut := registry.NewStaticCallerRegistry(cfg, logger)

				permitted, err := sut.IsPermitted(ctx, "caller-1", "tenant-z")

				Expect(permitted).To(BeFalse())
				Expect(status.Code(err)).To(Equal(codes.PermissionDenied))
			})
		})

		Context("when callerIdentity is not in registry", func() {
			It("returns (false, codes.PermissionDenied error)", func() {
				cfg := &registry.CallerRegistryConfig{Version: 1, Callers: []registry.CallerEntry{}}
				sut := registry.NewStaticCallerRegistry(cfg, logger)

				permitted, err := sut.IsPermitted(ctx, "unknown-caller", "tenant-a")

				Expect(permitted).To(BeFalse())
				Expect(status.Code(err)).To(Equal(codes.PermissionDenied))
			})
		})

		Context("when tenantID is empty", func() {
			It("returns (true, nil) without checking permitted_tenants", func() {
				cfg := &registry.CallerRegistryConfig{Version: 1, Callers: []registry.CallerEntry{}}
				sut := registry.NewStaticCallerRegistry(cfg, logger)

				permitted, err := sut.IsPermitted(ctx, "any-caller", "")

				Expect(err).To(BeNil())
				Expect(permitted).To(BeTrue())
			})
		})
	})

	// ===== PHASE 3: Core Operations — LoadCallerRegistryConfig =====
	Describe("Phase 3: Core Operations — LoadCallerRegistryConfig", func() {
		Context("when file exists and is valid YAML", func() {
			It("decodes version, callers, identities, and permitted_tenants", func() {
				dir := GinkgoT().TempDir()
				path := filepath.Join(dir, "callers.yaml")
				content := `version: 1
callers:
  - identity: service-a
    permitted_tenants:
      - tenant-1
      - tenant-2
  - identity: service-b
    permitted_tenants:
      - tenant-3
`
				Expect(os.WriteFile(path, []byte(content), 0600)).To(Succeed())

				cfg, err := registry.LoadCallerRegistryConfig(path)

				Expect(err).To(BeNil())
				Expect(cfg.Version).To(Equal(1))
				Expect(cfg.Callers).To(HaveLen(2))
				Expect(cfg.Callers[0].Identity).To(Equal("service-a"))
				Expect(cfg.Callers[0].PermittedTenants).To(ConsistOf("tenant-1", "tenant-2"))
				Expect(cfg.Callers[1].Identity).To(Equal("service-b"))
				Expect(cfg.Callers[1].PermittedTenants).To(ConsistOf("tenant-3"))
			})
		})

		Context("when file does not exist", func() {
			It("returns an error", func() {
				_, err := registry.LoadCallerRegistryConfig("/nonexistent/path/callers.yaml")
				Expect(err).To(HaveOccurred())
			})
		})

		Context("when file contains invalid YAML", func() {
			It("returns an error", func() {
				dir := GinkgoT().TempDir()
				path := filepath.Join(dir, "bad.yaml")
				Expect(os.WriteFile(path, []byte(":\t invalid yaml\n"), 0600)).To(Succeed())

				_, err := registry.LoadCallerRegistryConfig(path)
				Expect(err).To(HaveOccurred())
			})
		})
	})
})
