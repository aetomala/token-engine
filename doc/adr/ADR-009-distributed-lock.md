# ADR-009: Distributed Lock Design — SET NX PX Acquisition and Lua CAS-Delete Release

**Status:** Complete — v0.6.0  
**Date:** 2026-06-03

## Context

v0.5.0 deferred distributed locks because the service ran as a single replica — concurrent key
rotation and reconciliation across replicas was not yet possible. v0.6.0 promoted `CursorReconciler`
from a NoOp stub to a real implementation and added per-tenant key rotation logic in `main.go`.
Both operations run on a timer across all replicas and must not execute concurrently against the
same tenant's data.

The existing Redis dependency — already required for token storage, key storage, and idempotency —
made Redis the natural backing store for distributed coordination.

## Decision

`RedisLock` provides per-operation mutual exclusion using the following protocol:

**Acquisition:** `SET <key> <token> NX PX <ttl_ms>`

The `NX` flag makes the command atomic and only-if-not-exists. The `PX` flag sets an expiry in
milliseconds, ensuring the lock is automatically released if the holder crashes or takes longer
than the TTL. The command returns `"OK"` on success and an empty string if the key already exists.

**Lock token:** `uuid.New().String()`

A UUID v4 string is generated per `Acquire` call and stored as the key's value. This token is
the holder's proof of ownership and is required for safe release.

**Release:** Lua CAS-delete script executed atomically at Redis:
```lua
if redis.call("GET",KEYS[1]) == ARGV[1] then return redis.call("DEL",KEYS[1]) else return 0 end
```
The script compares the stored value against the caller's token before deleting. If the key no
longer exists or holds a different token (acquired by another replica after TTL expiry), the
script returns `0` and the key is left unchanged. `Release()` always returns `nil` — the operation
is idempotent by design.

**TTL:** passed as a parameter to `Acquire(ctx, key, ttl)` by each caller. Both current lock
users — key rotation and reconciliation — pass `cfg.LockTTL` (default `30s`, configurable via
`TOKEN_ENGINE_LOCK_TTL`).

**Lock keys:**
- Key rotation: `locks:key_rotation:<tenantID>`
- Reconciliation: `locks:reconciliation:<tenantID>`

`NoOpLocker` and `NoOpLock` are exported always-success implementations that return immediately
without touching Redis, suitable for single-node deployments and unit tests.

## Rationale

**SET NX PX over Redlock (multi-key consensus).** Redlock requires acquiring locks on ≥ 3
independent Redis nodes to tolerate node failures — it is designed for environments where Redis
nodes can fail independently. token-engine uses a single Redis instance that is already a hard
dependency for token storage and idempotency. Running Redlock against a single node provides no
fault-tolerance benefit over SET NX PX. Additionally, Redlock has documented correctness
concerns under clock skew between Redis nodes (Martin Kleppmann, 2016). For the single-Redis
deployment model, SET NX PX is simpler, well-understood, and equally correct.

**UUID v4 for lock token.** A UUID is globally unique across replicas regardless of clock state.
This uniqueness is what makes the CAS release safe: only the replica that set the key with its
own UUID can delete it. A timestamp-based token would collide under clock synchronization or
restart; a monotonic counter would require coordination to avoid replay across replicas.

**Lua CAS-delete for release.** A naive GET-then-DEL sequence has a race: if the original holder's
TTL expires between the GET (which returns the expected token) and the DEL, a second replica may
have acquired the lock, and the plain DEL would incorrectly evict the new holder. A Lua script
executes atomically at the Redis server — the compare and delete are a single operation with no
observable intermediate state.

**Per-call TTL rather than a locked-in constructor value.** Key rotation and reconciliation may
eventually need different TTLs as operation complexity grows. Accepting TTL as a parameter to
`Acquire` keeps the `RedisLock` implementation general — callers decide what duration is
appropriate for their operation. Embedding a fixed TTL at construction would require a separate
`RedisLock` instance per TTL value.

**Acceptable failure modes for best-effort operations.** The two operations protected by this
lock — key rotation and reconciliation — are not safety-critical data consistency operations:
- **Key rotation** is idempotent at the jwtauth layer: rotating a key that was already rotated by
  another replica produces a no-op.
- **Reconciliation** revokes orphaned tokens: revoking an already-revoked token is idempotent.

If a lock is lost because Redis restarts (TTL-based, not persistent) or a slow operation exceeds
its TTL, the worst outcome is that two replicas perform the same idempotent work simultaneously —
not data corruption. A stronger consensus mechanism (Raft, ZooKeeper) would be operationally
disproportionate for this use case.

## Alternatives Considered

**Redlock (multi-key consensus)** — rejected. Requires ≥ 3 independent Redis nodes for meaningful
fault tolerance; token-engine operates against a single Redis instance. Redlock's documented
correctness issues under clock skew add risk without benefit in this deployment model.

**In-process `sync.Mutex`** — rejected. Prevents concurrent execution within a single process
but not across replicas. The single-replica constraint in v0.5 was the only reason this had not
already been a problem; v0.6 lifts that constraint.

## Consequences

**Positive:**
- Key rotation and reconciliation are safe to run across multiple replicas without duplicate work
  under normal conditions.
- `NoOpLocker` preserves single-node deployability — no Redis required for lock acquisition in
  single-replica environments.
- Idempotent `Release()` is safe to call redundantly (e.g., in a `defer` after early return).
- No new infrastructure dependency — Redis is already required.

**Negative:**
- The lock is TTL-based and not persistent: a Redis restart clears all held locks immediately.
  Replicas that held a lock will attempt to re-acquire on their next tick without knowing the
  restart occurred.
- Slow operations that exceed `cfg.LockTTL` lose the lock mid-operation. Operators must tune
  `TOKEN_ENGINE_LOCK_TTL` relative to expected key rotation and reconciliation duration.
- Redis is a single point of failure for lock availability. If Redis is unavailable, `Acquire`
  fails and the operation is skipped for that tick — this is acceptable for best-effort
  operations but means lock-protected work does not run during Redis outages.

## References

- [internal/lock/lock.go](../../internal/lock/lock.go) — `Locker`/`Lock` interfaces and `RedisLock` implementation
- [internal/lock/noop.go](../../internal/lock/noop.go) — `NoOpLocker` and `NoOpLock`
- [cmd/token-engine/main.go](../../cmd/token-engine/main.go) — key rotation lock usage (`locks:key_rotation:<tenantID>`)
- [internal/reconciliation/cursor_reconciler.go](../../internal/reconciliation/cursor_reconciler.go) — reconciliation lock usage (`locks:reconciliation:<tenantID>`)
- [doc/adr/ADR-011-cursor-based-reconciler.md](ADR-011-cursor-based-reconciler.md) — reconciler design that depends on this lock
