# ADR-012: Idempotency Interceptor Concurrent-Request Claim

**Status:** Accepted
**Date:** 2026-09-21

## Context

ADR-005 established `IdempotencyStore` and the pre-handler ordering invariant for `RefreshToken`. Neither ADR-005 nor the shipped interceptor addressed concurrent requests sharing the same idempotency key. `handleIssueTokenIdempotency` and `handleRefreshTokenIdempotency` ran `Get` → (on miss) handler → (on success) `SetNX`. Nothing claimed the key before the handler ran, so two requests with the same key that both `Get` before either `SetNX` completes both reach the handler. For `RefreshToken` this can produce a revocation race (jwtauth revokes the old refresh token on invocation); for `IssueToken` it can issue two token pairs for one key. This amends ADR-005's ordering invariant: the invariant is no longer "check the cache before the handler," it is "atomically claim the key before the handler."

This design also had to account for two issues filed alongside this one, #128 (binding the idempotency key to request content via a fingerprint) and #117 (per-token orphan detection), both of which the issue's own tracking comment noted would extend the same stored record — the record format needed to be designed once, not reworked per issue.

## Decision

**Claim-before-call, using the store itself.** Before calling the handler, the interceptor atomically claims the idempotency key: `SetNX(ctx, key, pendingRecord)`.

- If the claim succeeds, the request proceeds to the handler. On success, the claim is promoted to a completed record via a new `Set` method (unconditional overwrite).
- If the claim fails (key already exists), the interceptor reads the existing record. A completed record (or a legacy bare-`TokenPair` record, predating this change) is returned as a cache hit. A still-pending record means a genuine concurrent duplicate — the request is rejected immediately with `codes.Aborted`, not blocked or retried internally.

**Versioned record envelope.** The stored value is now a small internal, JSON-encoded, magic-prefixed envelope (`state`, `response`) rather than a bare marshaled `TokenPair`. Bytes without the magic prefix are treated as a legacy record and read directly as a `TokenPair`, preserving backward compatibility for the remainder of their original TTL after this change deploys. `#128`'s request-content fingerprint and `#117`'s token reference extend this same envelope with additional fields — they do not introduce a second record format.

**Two TTLs, no new config variable.** The pending claim uses a short TTL (`TOKEN_ENGINE_LOCK_TTL`, already 30s by default and already used elsewhere in this codebase for "how long before an operation is assumed abandoned"). The completed record keeps using `TOKEN_ENGINE_IDEMPOTENCY_TTL`. `IdempotencyStore.SetNX` applies the short TTL; the new `Set` applies the long one. No `Delete` method was added — `IdempotencyStore`'s existing design deliberately relies on TTL expiry for cleanup, and a failed or crashed claim simply expires within the short pending TTL, after which the key is available again.

**Concurrent duplicates fail fast, they do not wait.** A losing request receives `codes.Aborted` immediately rather than blocking until the winning request completes. This matches the existing `lock.Locker` interface already used elsewhere in this codebase (`RedisLock.Acquire` is a single, non-blocking `SET NX PX` attempt — there is no polling/wait primitive anywhere in this codebase today), avoids holding a server goroutine and gRPC connection for an unbounded wait, and is consistent with `doc/operator-guide.md` §9 already framing gateway single-flight as the preferred mitigation and this interceptor as a secondary layer behind it.

## Rationale

**Why a store-level claim instead of `lock.Locker`.** `lock.Locker` was considered and rejected: it would put the "is a request in flight" state in a separate keyspace from the idempotency record itself, which conflicts with the explicit requirement (from the issue's own tracking comment) that this, #128, and #117 all extend one versioned record. A store-level pending state keeps that state where the future fields live.

**Why fail-fast over blocking-wait.** A blocking-wait design (poll the store until the winning request's result appears) was considered. It was rejected because it requires new machinery this codebase does not otherwise have (poll interval, timeout, thundering-herd risk under many concurrent duplicates) for a case `doc/operator-guide.md` §9 already treats as secondary to gateway single-flight. Fail-fast pushes retry responsibility to the caller, consistent with that framing, and needs no new primitives.

**Why no explicit delete/cleanup on handler failure.** Adding a `Delete` method was considered so a failed handler could immediately free its claim. It was rejected to preserve `IdempotencyStore`'s existing TTL-only cleanup design (stated explicitly in its interface doc comment). The accepted cost: a duplicate request arriving within the short pending TTL window after a crash or handler failure will itself be rejected with `codes.Aborted`, even though the original attempt is no longer actually in flight. This window is bounded by `TOKEN_ENGINE_LOCK_TTL` (30s default) — self-healing, not indefinite.

## Consequences

- Callers using `X-Idempotency-Key` now get real protection against concurrent duplicates, not just sequential retries. A concurrent duplicate must handle `codes.Aborted` as a retryable response.
- `IdempotencyStore` gained one method (`Set`) and a second construction-time TTL parameter (`pendingTTL`); `Get` and `SetNX`'s signatures are unchanged.
- Records written before this change (bare `TokenPair` bytes) are still read correctly as completed hits for the remainder of their original TTL — no migration or backfill is required.
- A crashed or failed in-flight request leaves duplicates rejected for up to `TOKEN_ENGINE_LOCK_TTL` before self-healing, rather than failing over to the handler immediately.
- Future work: #128 adds a request-content fingerprint to the record envelope defined here; #117 adds a token reference to the same envelope for orphan detection. Neither introduces a new record format.

## References

- [internal/store/idempotency.go](../../internal/store/idempotency.go)
- [internal/store/redis.go](../../internal/store/redis.go)
- [internal/interceptor/idempotency.go](../../internal/interceptor/idempotency.go)
- [internal/lock/lock.go](../../internal/lock/lock.go) — the non-blocking claim pattern this design mirrors
- [doc/operator-guide.md](../operator-guide.md) §9 — TOCTOU Concurrent Refresh Race and Single-Flight Mitigation
- ADR-005 — In-Memory Idempotency Store for v0.1 (the ordering invariant this ADR amends)
- Issue #127, #128, #117
