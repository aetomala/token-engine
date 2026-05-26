# ADR-002: jwtauth Library Delegation

**Status:** Accepted  
**Date:** 2026-05-26

## Context

token-engine must implement JWT token issuance, refresh, revocation, and validation. These operations involve key management (rotation, loading, JWKS), token signing (RS256), refresh token storage, and a range of security invariants (reserved claims protection, audience validation, kid validation, replay prevention).

The jwtauth library (`github.com/aetomala/jwtauth`) already implements all of these operations with production-grade observability, integration tests, and documented security decisions.

## Decision

Delegate all token business logic to jwtauth v0.7.0. token-engine implements only the transport layer, multi-tenancy, authentication, and observability wiring that jwtauth does not provide.

token-engine contains no JWT signing code, no key management code, and no refresh token storage code. All of these live in jwtauth.

## Rationale

- **Avoid duplication.** Reimplementing JWT operations would duplicate the security-sensitive logic already tested and documented in jwtauth, including 11 ADRs covering algorithm confusion prevention, kid validation, reserved claims protection, and replay prevention.
- **Single point of change.** Security fixes in jwtauth propagate to token-engine via a version bump. There is no parallel implementation to keep in sync.
- **Interface-first adapter.** The jwtauth `metrics.Metrics` interface (5 methods) differs from the token-engine `observability.Metrics` interface (5 methods). An adapter in `internal/observability` bridges the two — the adapter is the only file that knows about both interfaces.

## Consequences

**Positive:**
- token-engine stays thin and focused on service concerns.
- jwtauth security guarantees apply automatically.
- jwtauth's observability model (logging, metrics, tracing) is available in token-engine via adapters.

**Negative:**
- jwtauth version must be pinned explicitly. API-breaking changes in jwtauth require token-engine updates.
- The adapter layer (`LibraryLoggerAdapter`, `LibraryOtelTracer`, `LibraryPrometheusMetrics`) must be maintained when jwtauth's interfaces change.
- Error sentinel package ownership must be verified against the specific jwtauth version in use — sentinel package assignments changed between jwtauth v0.6.0 and v0.7.0.

## Known Version Constraints

| Dependency | Pinned Version | Reason |
|---|---|---|
| `github.com/aetomala/jwtauth` | v0.7.0 | Sentinel package layout (v0.7.0 specific) |
| `go.uber.org/mock` | v0.6.0 | Required by jwtauth v0.7.0 |
| `go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc` | v0.52.0 | v0.60.0+ dropped `UnaryServerInterceptor`; incompatible with `ChainUnaryInterceptor` pattern |

## References

- [internal/observability/errors.go](../../internal/observability/errors.go) — MapLibraryError sentinel table
- [jwtauth SECURITY.md](https://github.com/aetomala/jwtauth/blob/main/SECURITY.md)
- [jwtauth ADR directory](https://github.com/aetomala/jwtauth/tree/main/doc/adr)
