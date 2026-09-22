# ADR-014: Idempotency Key Precedence Between Request Field and Metadata Header

**Status:** Accepted
**Date:** 2026-09-22

## Context

The README documents an `idempotency_key` field on `IssueTokenRequest` and `RefreshTokenRequest`
("same key returns same tokens within TTL"). The server has never read it — the idempotency
interceptor reads the client key only from the `x-idempotency-key` gRPC metadata header (see
#129). #129 proposes honoring the field as a fallback when the header is absent, and its
acceptance criteria defers the precedence rule for "both present and differ" to "the ADR from
#127" (ADR-012). ADR-012 is entirely about the atomic-claim record layout and does not mention the
field or any precedence rule — the decision #129 depends on did not exist. This ADR makes it, so
#129 can implement against a settled rule instead of deciding it as a side effect of unrelated
field-wiring work.

This decision determines which string becomes `clientKey` in the store key
(`idempotency:{tenantID}:{method}:{clientKey}`, see ADR-012) before the atomic claim (ADR-012) and
fingerprint check (ADR-013) run. It does not affect what the fingerprint hashes — `idempotency_key`
is already excluded from `issueFingerprintInput`/`refreshFingerprintInput` (ADR-013) — only which
value the claim and fingerprint logic are keyed on.

## Decision

**Reject the request when both are present and differ.** When a request sets both the
`idempotency_key` field and the `x-idempotency-key` header, and their values are not equal, the
interceptor returns `codes.InvalidArgument` before any store interaction — no claim is attempted,
no handler is called.

- **Only the header is set:** use it (today's behavior, unchanged).
- **Only the field is set:** use it (the #129 fallback case).
- **Both are set and equal:** use that value; no ambiguity, no error.
- **Both are set and differ:** `codes.InvalidArgument`. Neither value is preferred over the other.
- **Neither is set:** pass through without store interaction (today's behavior, unchanged).

## Rationale

**Why reject instead of a silent precedence order (header-wins or field-wins).** A silent order
was considered and rejected. Header-wins would silently discard a value the caller explicitly set
on the request message — the one place the README documents as the API contract — with no signal
that it was ignored. Field-wins would silently override a header that may originate from
infrastructure the caller doesn't control (a gateway or sidecar attaching `x-idempotency-key`
independently of the application). Either choice hides a genuine integration bug: a caller sending
two different values for what it believes is one logical idempotency key has a broken assumption
somewhere in its own stack, and picking a winner for it doesn't fix that — it just makes the bug
harder to notice. This matches the precedent #128/ADR-013 already set for this exact class of
problem: a request-content mismatch against an idempotency key is surfaced as an explicit error
(there, `codes.FailedPrecondition` against a stored fingerprint; here, `codes.InvalidArgument`
against two conflicting inputs on the same request), not resolved by silently trusting one source
over another.

**Why `InvalidArgument` and not `FailedPrecondition`.** ADR-013 used `FailedPrecondition` because
the request was well-formed in isolation and conflicted with *prior stored state* the caller
couldn't see without querying it. Here there is no stored state involved yet — the conflict is
entirely within the single incoming request, between two fields the caller set directly. That is
exactly gRPC's convention for `InvalidArgument`: a problem with the request itself, detectable
without consulting any external state, occurring before the store is touched at all.

**Why this carries effectively no rollout risk.** The field has never been read by the server, so
no request in production today can produce a differing pair — this code path can only be exercised
once #129 ships the field-reading logic that makes the field meaningful in the first place. There
is no existing traffic this decision could break.

## Consequences

- #129 implements this rule directly: read the field, read the header, compare only when both are
  non-empty, reject on mismatch before constructing the store key or attempting a claim.
- #129 needs specs for all five cases above (header-only, field-only, both-equal, both-differ,
  neither), in both `IssueToken` and `RefreshToken`.
- A caller that intentionally or accidentally sends two different idempotency keys now gets an
  explicit, fixable error instead of an unpredictable choice between them.
- No change to ADR-012's record layout or ADR-013's fingerprint computation — this ADR only
  decides which value flows into the key construction step that precedes both.

## References

- [ADR-012](ADR-012-idempotency-concurrency-claim.md) — the record layout and key construction this ADR feeds
- [ADR-013](ADR-013-idempotency-request-fingerprint.md) — confirms `idempotency_key` is excluded from the fingerprint's hashed fields
- [internal/interceptor/idempotency.go](../../internal/interceptor/idempotency.go)
- Issue #129, #135
