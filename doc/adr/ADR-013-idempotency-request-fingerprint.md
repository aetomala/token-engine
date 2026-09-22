# ADR-013: Idempotency Key Bound to Request Content via Fingerprint

**Status:** Accepted
**Date:** 2026-09-22

## Context

The idempotency store key is `idempotency:{tenantID}:{method}:{clientKey}`. On a cache hit, the
interceptor returned the stored `TokenPair` without comparing the incoming request to the request
that produced it. Because `clientKey` is caller-supplied, a caller that reuses or collides keys
across different subjects (`IssueToken`) or different refresh tokens (`RefreshToken`) silently
received another request's token pair. ADR-012 introduced the versioned `idempotencyRecord`
envelope (`{state, response}`) specifically so this issue and #117 could extend it with additional
fields rather than reworking the record format.

## Decision

**Fingerprint the request content, store it in the same envelope.** A completed record now also
carries `fingerprint`: the hex-encoded SHA-256 hash of a canonical JSON encoding of the fields that
determine the result.

- `IssueToken`: `sub`, `tenant_id`, `claims`, `audiences` — excludes `idempotency_key` itself.
  `audiences` is sorted before hashing so differently ordered but equivalent requests still match.
- `RefreshToken`: `refresh_token`, `tenant_id`, `claims`. The raw refresh token is only ever held
  in memory during hashing — only the resulting digest is persisted, so no raw credential is
  stored in the fingerprint.
- `encoding/json` sorts map keys alphabetically when marshaling, so `claims` hashes deterministically
  without extra sorting code.

**Compare on cache hit, reject on mismatch.** In the "lost the claim, existing record is completed"
branch of both handlers, the interceptor computes the incoming request's fingerprint and compares
it against the stored one. A match returns the cached response as before. A mismatch returns
`codes.FailedPrecondition` instead of the cached response, and increments
`token_engine_idempotency_total` with a new `result=mismatch` label value.

**`codes.FailedPrecondition`, not `codes.InvalidArgument`.** The request itself is well-formed in
isolation — each field is individually valid. What's invalid is the combination: this key was
already bound to different content. `FailedPrecondition` signals the caller must change something
about its own state (use a new idempotency key) before retrying, whereas `InvalidArgument` would
suggest the request's fields themselves are malformed, which they are not. This also keeps the
mismatch case distinguishable from the existing `codes.Aborted` used for a genuine concurrent
duplicate (still-pending claim): `Aborted` means "retry the same call, it's still in flight";
`FailedPrecondition` means "retry will fail again unless you change the key."

**Records without a fingerprint are not rejected.** Records written before this change — legacy
bare-`TokenPair` bytes predating ADR-012's envelope, and #127-era versioned-envelope records
written before this field existed — have an empty `fingerprint`. The interceptor treats an empty
stored fingerprint as "no mismatch check possible" and returns the cached response unchanged, for
the remainder of that record's original TTL. No migration or backfill is required, consistent with
ADR-012's handling of its own legacy format.

## Rationale

**Why extend the envelope instead of a separate fingerprint store.** A separate keyspace (e.g.
`idempotency-fp:{key}`) was considered and rejected for the same reason ADR-012 rejected a
separate lock keyspace: it splits state that belongs together and adds a second read/write path
with its own failure modes (partial writes, independent TTL drift) for no benefit — the fingerprint
is only ever read alongside the response it validates.

**Why hash instead of storing raw fields.** Storing the raw subject/audiences/claims alongside the
response was considered simpler (no hashing, human-readable for debugging) but rejected: the issue
requires "no raw credential in the fingerprint," and hashing generalizes to `RefreshToken`, where
the compared field (the refresh token) is itself a credential. Using one mechanism for both RPCs is
simpler than a credential-safe path for `RefreshToken` and a raw-storage path for `IssueToken`.

**Why `FailedPrecondition` over `InvalidArgument`.** Both were viable per the issue's own framing.
`InvalidArgument` was rejected because gRPC's convention reserves it for requests that are invalid
independent of server state — these requests are not; they conflict with prior state tied to the
same key. `FailedPrecondition`'s gRPC guidance — "the client should not retry until the system
state has been explicitly fixed" — matches exactly: the caller's fix is to supply a new key, not to
alter the request fields.

## Consequences

- Callers that reuse an idempotency key with different request content now get an explicit,
  distinguishable error (`codes.FailedPrecondition`) instead of silently receiving a mismatched
  response. This is the correctness fix the issue requires.
- `idempotencyRecord` gained one field (`Fingerprint`); `encodeCompletedRecord` and
  `resolveExistingRecord`'s signatures changed accordingly (internal, package-private — no
  `IdempotencyStore` interface change, no mock regeneration needed).
- `token_engine_idempotency_total` gained a third `result` label value (`mismatch`), alongside the
  existing `hit`/`miss`.
- Pre-existing records (legacy or #127-era) are unaffected — they keep returning cache hits as
  before until their TTL expires, with no fingerprint protection during that window.
- Future work: #117 adds a token reference to the same envelope for orphan detection; it does not
  introduce a new record format either.

## References

- [internal/interceptor/idempotency.go](../../internal/interceptor/idempotency.go)
- [ADR-012](ADR-012-idempotency-concurrency-claim.md) — introduces the versioned envelope this ADR extends
- [doc/operator-guide.md](../operator-guide.md) — idempotency key content-binding contract
- Issue #128
