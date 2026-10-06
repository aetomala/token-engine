package integration_test

import (
	"context"
	"sync/atomic"

	"github.com/aetomala/token-engine/internal/audit"
)

// pingCountingAuditStore is a test-only audit.Store that delegates to an embedded Store and counts
// Ping calls. The integration harness wires it in place of the bare NoOp store so specs can assert
// that a rejected revocation request never reached the handler's audit-store availability check.
// All methods are safe for concurrent use.
type pingCountingAuditStore struct {
	audit.Store              // Delegate for RecordRevocation and Ping.
	pings       atomic.Int64 // Number of Ping calls observed.
}

// Ping records the call and delegates to the embedded Store.
func (s *pingCountingAuditStore) Ping(ctx context.Context) error {
	s.pings.Add(1)
	return s.Store.Ping(ctx)
}

// Pings returns the number of Ping calls observed so far.
func (s *pingCountingAuditStore) Pings() int64 {
	return s.pings.Load()
}
