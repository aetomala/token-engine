# ADR-003: Static Caller Keys for v0.1

**Status:** Superseded — mTLS delivered in v0.5.0; see ADR-008  
**Date:** 2026-05-26

## Context

token-engine needs to authenticate callers — services that send gRPC requests. Authentication serves two purposes: identity establishment (which caller is this?) and access control (is this caller allowed to operate on this tenant?).

Options evaluated:
1. Static API key → caller identity map, configured at startup via environment variable
2. Dynamic caller registry backed by Redis, allowing key rotation without restart
3. mTLS client certificates with certificate identity extraction

## Decision

Static API key → caller identity map for v0.1. Keys are configured via `TOKEN_ENGINE_STATIC_CALLER_KEYS` at startup. The `StaticKeyAuthenticator` holds the map in memory and performs constant-time lookup on every request.

## Rationale

- **Simplicity over flexibility for v0.1.** The service needs to be runnable without Redis. A static map is sufficient for environments where key rotation can tolerate a restart.
- **Interface seam preserved.** The `Authenticator` interface abstracts the lookup. Swapping the static implementation for a Redis-backed one in v0.2 requires no changes to the interceptor.
- **Constant-time comparison.** API key comparison uses `subtle.ConstantTimeCompare` to prevent timing attacks — the static implementation has the same security property as a dynamic one.

## Consequences

**Positive:**
- Zero external dependencies in v0.1 — service runs without Redis or a key management service.
- Simple to reason about: the caller identity for any API key is determined at startup and does not change at runtime.

**Negative:**
- Key rotation requires a process restart with updated `TOKEN_ENGINE_STATIC_CALLER_KEYS`.
- Key revocation is not immediate — the service must be restarted to revoke a key.
- Static keys stored in environment variables require secrets management discipline at the deployment layer (Kubernetes Secrets, Vault agent injection, etc.).

## Outcome

mTLS authentication (`TLS_MODE=mtls`) was delivered in v0.5.0 via `MTLSAuthenticator`, which extracts caller identity from the TLS client certificate Common Name. The full mTLS design rationale is documented in ADR-008.

Static key authentication remains the production path when `TLS_MODE=disabled`. The two modes are mutually exclusive at startup — `TLS_MODE` determines which `Authenticator` implementation is wired in `main.go`.

The Redis-backed `CallerRegistry` referenced in the original Target Version note was not pursued — `StaticCallerRegistry` meets current requirements without the operational overhead of Redis key management.

## References

- [internal/interceptor/auth.go](../../internal/interceptor/auth.go) — StaticKeyAuthenticator
- [internal/registry/caller.go](../../internal/registry/caller.go) — StaticCallerRegistry
- [ADR-006](ADR-006-interceptor-chain-order.md) — where in the chain authentication runs
- [ADR-008](ADR-008-mtls-auth-model.md) — mTLS design rationale and CN-based identity model
