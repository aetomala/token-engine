# ADR-016: Run Validation Before the Idempotency Claim

**Status:** Accepted (amends ADR-006)
**Date:** 2026-09-30

## Context

ADR-006 placed the idempotency interceptor ahead of the validation interceptor so that a duplicate
`IssueToken` or `RefreshToken` request could return its cached response before any field
validation ran. That rationale assumed idempotency was a read-only cache check: a request that
later failed validation left nothing behind.

ADR-012 changed that. The idempotency interceptor now writes a pending claim (`SetNX`) before
calling the next handler, and that claim lives for `TOKEN_ENGINE_LOCK_TTL` (30s by default). With
validation running after the claim, a request that carries an idempotency key and then fails
validation still leaves a pending claim behind:

- A caller that corrects an invalid request and retries with the same key within the lock TTL
  receives `codes.Aborted` instead of reaching the handler.
- A request with an empty `tenant_id` passes caller authorization (`StaticCallerRegistry.IsPermitted`
  returns `true` for an empty tenant), is claimed under a `"default"` tenant fallback, and its
  same-key retries receive `codes.Aborted` instead of `codes.InvalidArgument`. That partially undid
  the v1.1.0 fix that made `tenant_id` errors consistent.

A request that fails validation must never create a claim.

## Decision

**The chain order becomes:**

```
1. otelgrpc.UnaryServerInterceptor — trace span creation
2. Correlation ID + request count and duration metrics
3. Authentication
4. Caller authorization
5. Validation
6. Idempotency (IssueToken and RefreshToken)
```

**The `"default"` tenant fallback is removed** from the idempotency interceptor. After the reorder,
an empty `tenant_id` cannot reach it. If one does, the interceptor logs at Error and returns
`codes.Internal` without touching the store or the handler. That condition means an upstream
invariant was violated, which is a wiring bug. This mirrors the caller-authorization interceptor,
which returns `codes.Internal` when no caller identity is present in the context.

## Rationale

**Validation before idempotency.** Validation is a pure function of the request with no store
access. Moving it ahead of the claim means an invalid request is rejected with
`codes.InvalidArgument` before any state is written, so the rejection is the same on every attempt
and a corrected retry reaches the handler.

**Caller authorization stays ahead of validation.** Unauthorized callers must not receive
validation feedback about request shape. ADR-006's authentication → authorization ordering and its
rationale are unchanged.

**At-most-once semantics are unchanged.** Validation cannot cause a handler execution; moving it
ahead of the claim can only prevent executions, never add one. ADR-012's claim, promotion, and
concurrent-duplicate behavior are untouched.

**`codes.Internal` instead of a fallback tenant.** Silently keying an unvalidated request under a
shared `"default"` tenant hid a wiring error and mixed idempotency keys across callers that omitted
`tenant_id`. With validation guaranteed to run first, the only way to reach the interceptor with an
empty tenant is a broken chain, and failing loudly makes that visible.

## Consequences

**Positive:**
- A request that fails validation leaves no idempotency claim behind; a corrected retry with the
  same key succeeds.
- An empty `tenant_id` is rejected with `codes.InvalidArgument` on every attempt, restoring the
  v1.1.0 behavior regardless of whether an idempotency key is set.
- Idempotency keys are always scoped to a real tenant.

**Negative (accepted):**
- A genuine duplicate is validated before its cached response is returned. The cost is
  microseconds with no store access.
- If a future release tightens validation rules, a retry of a request that succeeded before the
  change is rejected rather than served from cache.

**Compatibility:** patch-level. Only requests that were already invalid change behavior, and they
now receive `codes.InvalidArgument` consistently. No proto or configuration change.

## References

- [ADR-006](ADR-006-interceptor-chain-order.md) — the original chain order, amended by this ADR
- [ADR-012](ADR-012-idempotency-concurrency-claim.md) — the claim-before-call design that made idempotency a write
- [internal/interceptor/idempotency.go](../../internal/interceptor/idempotency.go)
- [internal/interceptor/validation.go](../../internal/interceptor/validation.go)
- [cmd/token-engine/main.go](../../cmd/token-engine/main.go) — chain assembly
- Issue #154
