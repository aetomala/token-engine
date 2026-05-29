package audit_test

import (
	"context"
	"time"

	"github.com/aetomala/token-engine/internal/audit"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("NoOpAuditStore", func() {
	var (
		ctx context.Context
	)

	BeforeEach(func() {
		ctx = context.Background()
	})

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: Core Operations", func() {
		Context("RecordRevocation", func() {
			It("returns nil for any event", func() {
				sut := audit.NewNoOpAuditStore()

				event := audit.RevocationEvent{
					TenantID:       "tenant-1",
					CallerIdentity: "caller-1",
					TokenID:        "token-1",
					Scope:          "token",
					OccurredAt:     time.Now(),
				}

				err := sut.RecordRevocation(ctx, event)

				Expect(err).To(BeNil())
			})
		})

		Context("Ping", func() {
			It("returns nil", func() {
				sut := audit.NewNoOpAuditStore()

				err := sut.Ping(ctx)

				Expect(err).To(BeNil())
			})
		})
	})
})
