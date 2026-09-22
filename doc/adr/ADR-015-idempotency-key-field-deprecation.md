# ADR-015: Deprecate the `idempotency_key` Request Field

**Status:** Accepted
**Date:** 2026-09-22

## Context

Two independent, historically un-reconciled mechanisms exist for supplying an idempotency key: the
`idempotency_key` request field on `IssueTokenRequest`/`RefreshTokenRequest`, and the
`x-idempotency-key` gRPC metadata header. The field was drafted into the proto schema at v0.1
before the interceptor had a real implementation; the header was chosen independently three days
later when the real implementation landed at v0.4, following the same header-driven convention
already used by the rest of the interceptor chain for cross-cutting concerns (correlation ID,
API-key auth — see ADR-006). Neither ADR-005 (the original idempotency ADR) nor the v0.4 commit
that introduced the header ever discusses the field or reconciles the two. #129 made the field
functional as a fallback for the header, and ADR-014 decided the precedence rule between them.

Now that both mechanisms work correctly, having two ways to do the same thing is worth resolving
in favor of one. The header is the better long-term single source of truth — it matches the
chain's existing convention (every other cross-cutting concern in this interceptor chain is header-
driven) and is supplied out-of-band from the request payload the same way.

This is a **released, tagged API** — `v1.0.0` and `v1.1.0` are already shipped — so this ADR
decides deprecation only. Removing the field outright is a `buf breaking`-flagged, semver-major
change and a separate future decision, gated on real usage data (see Consequences).

## Decision

**Mark the field deprecated; change nothing about its behavior.** `idempotency_key` is marked
`[deprecated = true]` in `proto/token_engine.proto` on both `IssueTokenRequest` and
`RefreshTokenRequest`, with a doc comment pointing callers to the header and to ADR-014. The
server-side resolution logic (#129, ADR-014) is unchanged — the field keeps working exactly as it
does today, including as a fallback and in the both-present-and-equal case. Deprecation is a
forward-looking signal, not a functional change; changing behavior now would be a second churn on
code that was only just built and tested.

**Add a usage signal.** The interceptor now logs, at Info level, whenever the field contributes to
resolving the effective key — distinguishing "field only, header absent" from "field and header
both present and equal" (a conflicting pair never reaches this log call; `resolveIdempotencyKey`
already rejects it with `codes.InvalidArgument` before either handler gets this far). This exists
specifically to replace assumption with data: "no one is using this field" was an unverifiable
guess before this change: it's now an observable, queryable fact operators can check via their log
pipeline.

**No metric was added for this.** A log line was chosen over a new Prometheus label or counter
because this is expected to be low-cardinality, low-frequency, and diagnostic — the question it
answers ("has usage stopped, and when") is best served by log-based alerting or a one-off log
search when a removal decision is actually being considered, not a permanently-scraped time series
tracking a field the project intends to delete.

## Rationale

**Why deprecate instead of removing outright, given "we're not far from 1.0 anyway."** That framing
turned out to be incorrect — `v1.0.0` and `v1.1.0` are already tagged releases, so this field has
been part of a versioned, public API surface since before this milestone began. Removing it now
would be a breaking change to already-released behavior, not a pre-1.0 cleanup. `buf breaking`
(configured with the `FILE` ruleset in `buf.yaml`) confirms marking the field deprecated is
non-breaking; removing it would not be — that gate is treated here as the intended discipline
forcing removal to be a deliberate, versioned decision rather than an incidental one.

**Why not skip the usage signal and just wait out an arbitrary deprecation window.** A fixed
timeline (e.g. "remove in two minor versions") was considered and rejected — it substitutes one
guess ("no one uses it") for another ("N versions is long enough"). Logging actual usage lets a
future removal decision be based on observed behavior instead of either guess.

**Why Info level, not Warn.** This isn't a degraded or error condition — a caller using the
documented, still-fully-functional field is doing nothing wrong. `Warn` is reserved elsewhere in
this codebase for recoverable failures and degraded operation; using it here would misrepresent a
supported (if soon-to-be-superseded) code path as a problem.

## Consequences

- No behavior change for any existing caller — field-based, header-based, and both-present-and-
  equal requests all continue to work exactly as #129/ADR-014 implemented them.
- Operators who search their logs for the new Info line get a direct answer to "is this field
  actually used" instead of guessing.
- **Removal criteria for a future decision, not committed to here:** no earlier than the next major
  version (per `buf breaking` and semver discipline), and only after the usage log shows sustained
  non-use over a meaningful observation window. Both conditions are necessary; neither alone is
  sufficient — a major version bump without usage evidence would be removing on the same
  unverified assumption this ADR set out to replace.
- Generated Go code (`gen/v1/token_engine.pb.go`) now carries `// Deprecated:` doc comments on the
  field, its getter, and the struct field itself, which surface automatically in IDEs and in any
  `staticcheck`-class linting a consumer runs on their own generated stubs (this repo's own
  `.golangci.yml` does not enable `staticcheck`/`SA1019`, so internal server-side usage of the
  field in `idempotency.go` is unaffected).

## References

- [proto/token_engine.proto](../../proto/token_engine.proto)
- [internal/interceptor/idempotency.go](../../internal/interceptor/idempotency.go)
- [ADR-014](ADR-014-idempotency-key-precedence.md) — the precedence rule this ADR does not change
- [buf.yaml](../../buf.yaml) — `breaking: use: FILE`, the gate actual removal must pass
- Issue #139
