# ADR-004: NoOp Stubs for v0.1

**Status:** Accepted  
**Date:** 2026-05-26

## Context

Several planned components require Redis to be useful: audit logging (persist a record of every token operation), token reconciliation (periodically clean up expired or orphaned tokens), and the dynamic tenant registry. Redis is out of scope for v0.1.

Two approaches were considered:
1. Omit these components entirely and wire them in later.
2. Define interfaces and NoOp implementations now; wire the real implementations later.

## Decision

Define interfaces and NoOp implementations for all three components. Wire the NoOp implementations in `main.go` for v0.1. The real implementations are added in v0.2 by replacing the NoOp with the Redis-backed version at the wiring site — no other files change.

## Rationale

- **Seam-first design.** Defining the interface now prevents future API churn. If the `AuditStore` interface is not defined until v0.2, adding it requires threading a new parameter through every constructor that needs it — a much larger change than swapping a NoOp for a real implementation.
- **Tests can stub.** With an interface defined, test suites for other components can inject a mock `AuditStore` and verify the correct calls are made, even before the real implementation exists.
- **No behavior change.** A NoOp that returns `nil` on every call is indistinguishable from no component at all, except it satisfies the interface. The service runs correctly in v0.1 without audit records or reconciliation.

## Components

| Interface | NoOp | Behavior |
|---|---|---|
| `audit.AuditStore` | `NoOpAuditStore` | `RecordRevocation` and `Ping` always return `nil` |
| `reconciliation.TokenReconciler` | `NoOpReconciler` | `Run` returns `nil` immediately (does not block; does not respect context cancellation) |
| `registry.TenantRegistry` | `StaticTenantRegistry` | Returns `codes.Unimplemented` for all methods — tenant lookup is handled via static caller registry in v0.1 |

## Consequences

**Positive:**
- v0.2 implementation is a drop-in replacement — only `main.go` changes.
- Test coverage of the interface contract is possible in v0.1.

**Negative:**
- No audit trail in v0.1 — token operations are not recorded anywhere beyond log lines.
- No automatic cleanup of expired tokens in v0.1 — orphaned refresh tokens accumulate in the jwtauth `RefreshStore` until it is restarted or the store is manually cleared.

## Outcome

Two of the three NoOp stubs have been superseded by real implementations:

- **`SlogAuditStore`** replaced `NoOpAuditStore` in v0.3.0 — writes structured log lines for
  all revocation events and is wired in `main.go`. `NoOpAuditStore` remains available as a
  test utility for components that need an `AuditStore` without a real logger.
- **`CursorReconciler`** replaced `NoOpReconciler` in v0.6.0 — performs cursor-based
  reconciliation of the Redis-backed refresh token store on a configurable interval and is
  wired in `main.go`. `NoOpReconciler` remains available as a test utility.
- **`MultiTenantRegistry`** replaced `StaticTenantRegistry` in v0.5.0.

## References

- [internal/audit/store.go](../../internal/audit/store.go)
- [internal/reconciliation/reconciler.go](../../internal/reconciliation/reconciler.go)
- [internal/registry/tenant.go](../../internal/registry/tenant.go)
