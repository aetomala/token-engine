# ADR-011: Cursor-Based Reconciler

**Status:** Complete — v0.6.0
**Date:** —

## Context

The idempotency store holds `SET NX` records for each `IssueToken` and (in v0.6+) `RefreshToken`
call. These records have a finite TTL (default 24h). In a healthy system, every idempotency record
corresponds to a live token in the refresh store. However, clock skew, Redis eviction under memory
pressure, or a service restart between `SetNX` and the handler response can create orphaned tokens:
refresh tokens in the store with no corresponding idempotency record. These orphans are harmless
for correctness but accumulate silently.

The `Reconciler` interface has been present since v0.1 with a `NoOpReconciler` stub. v0.6 promotes
it to a real implementation.

## Decision

Implement a cursor-based, best-effort reconciler that:

1. Pages through the refresh store for a given tenant using a persistent cursor stored in Redis
   at `reconciliation:cursor:{tenant_id}`
2. For each token found, checks validity — orphaned tokens are revoked
3. Holds a per-operation distributed lock (`locks:reconciliation:{tenant_id}`) during the scan
   to prevent concurrent reconciliation runs across replicas
4. Resumes from the stored cursor position on restart — the scan is safe to interrupt at any
   page boundary
5. Does not retry failed individual revocations — best-effort, async model

## Rationale

**Cursor-based pagination:** Scanning the full refresh store in a single pass would block Redis
under high token volume. A cursor allows the reconciler to yield between pages and be interrupted
safely.

**Persistent cursor in Redis:** Storing the cursor at `reconciliation:cursor:{tenant_id}` ensures
the reconciler resumes from where it stopped after a pod restart rather than restarting the full
scan.

**Best-effort, not compensating:** Orphaned tokens do not represent a security risk — they are
valid tokens that lack an idempotency record. A compensating rollback model would add complexity
with minimal benefit.

**Distributed lock:** Multiple replicas running concurrent scans against the same tenant's store
would produce duplicate revocations (idempotent) but unnecessary Redis load. A short-lived lock
prevents this without requiring a leader election mechanism.

**Single-replica constraint lifted at v0.6:** Pre-v0.6 deployments must run as single replica.
The cursor-based reconciler, combined with per-operation locks for key rotation, lifts this
constraint.

## Consequences

**Positive:**
- Orphaned tokens are eventually cleaned up without operator intervention.
- Reconciliation is resumable across restarts — no full rescan on restart.
- Multiple replicas can coexist safely.
- Best-effort model keeps implementation simple.

**Negative:**
- Adds a background goroutine and Redis cursor state per tenant.
- Cursor TTL must be tuned relative to key rotation interval and expected token volume.
- A bug that incorrectly identifies live tokens as orphans would silently revoke valid tokens
  — conservative orphan detection heuristic and test coverage are essential.

## Outcome

The reconciler shipped in v0.6.0 as `CursorReconciler`, but implements a narrower scope than
this ADR decided: it acquires the per-tenant distributed lock (item 3) and calls
`TokenManager.CleanupExpiredTokens` once per tenant per `Run` pass — a full expiry-based sweep
of the tenant's refresh store. It does not page through `ListTokens` or perform per-token
orphan detection against idempotency records (item 2). That per-token work was never built,
and no reverse mapping from a refresh token to the idempotency key that produced it exists to
support it — idempotency keys are derived from tenant, method, and a caller-supplied key, not
the resulting token.

The shipped implementation retained the `CursorReconciler` name for historical continuity, but
the cursor persistence (items 1 and 4) was removed in v1.0.1 once it was confirmed to serve no
purpose without per-token work to resume — `CleanupExpiredTokens` is a self-contained
full-namespace scan independent of any pagination cursor, so re-running it once per page (the
original bug: issue #97) provided no benefit and added Redis load proportional to page count.

## References

- `internal/reconciliation/reconciler.go` — `Reconciler` interface
- `internal/reconciliation/lock.go` — distributed lock implementation (v0.6)
- `doc/ARCHITECTURE.md` — Roadmap, Interface Seams section
