package audit_test

import (
	"context"
	"time"

	"github.com/aetomala/token-engine/internal/audit"
	"github.com/aetomala/token-engine/internal/testutil"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
)

var _ = Describe("SlogAuditStore", func() {
	// ===== PHASE 1: Constructor and Initialization =====
	Describe("Phase 1: SlogAuditStore — Constructor and Initialization", func() {
		It("satisfies audit.Store interface", func() {
			var _ audit.Store = (*audit.SlogAuditStore)(nil)
			Expect(true).To(BeTrue())
		})
	})

	// ===== PHASE 3: Core Operations =====
	Describe("Phase 3: SlogAuditStore — Core Operations", func() {
		var (
			ctx        context.Context
			cancel     context.CancelFunc
			ctrl       *gomock.Controller
			mockLogger *testutil.MockLogger
			store      *audit.SlogAuditStore
		)

		BeforeEach(func() {
			ctx, cancel = context.WithTimeout(context.Background(), 5*time.Second)
			ctrl = gomock.NewController(GinkgoT())
			mockLogger = testutil.NewMockLogger(ctrl)
			store = audit.NewSlogAuditStore(mockLogger)
		})

		AfterEach(func() {
			cancel()
			ctrl.Finish()
		})

		Context("Ping", func() {
			It("always returns nil", func() {
				err := store.Ping(ctx)
				Expect(err).To(BeNil())
			})
		})

		Context("RecordRevocation — Scope=token", func() {
			It("calls logger.Info with msg 'token revoked' and all key-value pairs, returns nil", func() {
				event := audit.RevocationEvent{
					TenantID:       "tenant-1",
					CallerIdentity: "caller-1",
					TokenID:        "tok-abc",
					Target:         "",
					Scope:          audit.RevocationScopeToken,
					OccurredAt:     time.Now().UTC(),
				}
				mockLogger.EXPECT().Info(gomock.Any(), "token revoked",
					"tenant_id", event.TenantID,
					"caller_identity", event.CallerIdentity,
					"token_id", event.TokenID,
					"target", event.Target,
					"scope", event.Scope,
					"occurred_at", event.OccurredAt.Format(time.RFC3339),
				)
				err := store.RecordRevocation(ctx, event)
				Expect(err).To(BeNil())
			})
		})

		Context("RecordRevocation — Scope=audience", func() {
			It("emits target as audience value and token_id as empty string, returns nil", func() {
				event := audit.RevocationEvent{
					TenantID:       "tenant-1",
					CallerIdentity: "caller-1",
					TokenID:        "",
					Target:         "api",
					Scope:          audit.RevocationScopeAudience,
					OccurredAt:     time.Now().UTC(),
				}
				mockLogger.EXPECT().Info(gomock.Any(), "token revoked",
					"tenant_id", event.TenantID,
					"caller_identity", event.CallerIdentity,
					"token_id", event.TokenID,
					"target", event.Target,
					"scope", event.Scope,
					"occurred_at", event.OccurredAt.Format(time.RFC3339),
				)
				err := store.RecordRevocation(ctx, event)
				Expect(err).To(BeNil())
			})
		})

		Context("RecordRevocation — Scope=user", func() {
			It("emits target as user ID value and token_id as empty string, returns nil", func() {
				event := audit.RevocationEvent{
					TenantID:       "tenant-1",
					CallerIdentity: "caller-1",
					TokenID:        "",
					Target:         "user-1",
					Scope:          audit.RevocationScopeUser,
					OccurredAt:     time.Now().UTC(),
				}
				mockLogger.EXPECT().Info(gomock.Any(), "token revoked",
					"tenant_id", event.TenantID,
					"caller_identity", event.CallerIdentity,
					"token_id", event.TokenID,
					"target", event.Target,
					"scope", event.Scope,
					"occurred_at", event.OccurredAt.Format(time.RFC3339),
				)
				err := store.RecordRevocation(ctx, event)
				Expect(err).To(BeNil())
			})
		})
	})
})
