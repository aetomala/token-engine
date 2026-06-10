# ADR-007: MultiTenantRegistry — Add/Drain/Remove Lifecycle and Per-Tenant Namespace Isolation

**Status:** Complete — v0.5.0  
**Date:** 2026-05-30

## Context

v0.1 through v0.2 wired a single static tenant directly in `main.go` using `StaticTenantRegistry`.
Tenant identity was fixed at startup: the Issuer, Audience, and Redis connection were read from
environment variables and a single `KeyManager` + `TokenManager` pair was created once.

v0.5.0 introduced two new requirements:

1. **Runtime tenant lifecycle** — tenants must be onboardable and offboardable without restarting
   the service. A static map locked at startup cannot support this.
2. **Per-tenant data isolation** — each tenant's JWT keys, refresh tokens, and observability
   signals must be strictly separated. Sharing Redis key space across tenants risks key ID
   collisions and cross-tenant metric aggregation.

## Decision

Replace `StaticTenantRegistry` with `MultiTenantRegistry`, which exposes a three-operation
lifecycle: `Add`, `Drain`, and `Remove`.

**`Add(ctx, tenantID, TenantConfig)`**

Constructs four per-tenant components using the tenantID as both the Redis `KeyPrefix` and the
jwtauth library `Namespace`:

- `keys.RedisKeyStore` — Redis-backed key store; keys namespaced under `tenantID:`
- `keys.Manager` — key lifecycle manager (rotation, loading, JWKS); namespace `tenantID`
- `storage.RedisRefreshStore` — refresh token store; keys namespaced under `tenantID:`
- `tokens.Manager` — token issuance, refresh, and revocation; namespace and issuer from config

Starts the `KeyManager` before returning. Returns `codes.AlreadyExists` if the tenant is already
registered.

**`Drain(ctx, tenantID)`**

Sets a boolean `draining` flag on the tenant's entry. Subsequent `Get` calls return
`codes.NotFound`. In-flight requests that already resolved a `TokenManager` reference from a
prior `Get` are unaffected — they hold the reference directly and do not go back through the
registry.

**`Remove(ctx, tenantID)`**

Requires `Drain` first — returns `codes.FailedPrecondition` if the entry is not draining.
Calls `km.Shutdown(ctx)` to cleanly stop the `KeyManager`, then deletes the entry from the map.

## Rationale

**Dynamic lifecycle over static map.** Operators need to onboard new tenants and drain retiring
ones without a service restart. A static map initialized from environment variables has no
mechanism for runtime mutation. `MultiTenantRegistry` provides explicit lifecycle control while
keeping the `TenantRegistry` interface unchanged from the caller's perspective.

**Two-phase shutdown — Drain then Remove.** A single-step `Remove` that immediately deletes the
entry would race with in-flight requests: a handler that resolved a `TokenManager` via `Get`
before the `Remove` call holds a valid reference, but any concurrent `Get` after `Remove` would
fail. Separating the phases means Drain stops new requests from reaching the tenant while Remove
waits for the operator to decide that in-flight work has completed — the timing of `Remove` is
the operator's signal that the window has closed.

**Natural token expiry over force-revocation.** Force-revoking all of a tenant's tokens at
`Remove` time would require a full scan of the refresh store, add complexity, and create a
thundering-herd of revocation events. Tokens already issued carry their own TTL. The operator
controls the effective offboarding window by choosing how long to wait between `Drain` and
`Remove` — long enough that any unexpired tokens the tenant was actively using have either
expired or been refreshed under the new tenancy arrangement.

**Namespace isolation via key prefix at construction.** A shared Redis key space would require
every call site to construct a compound key from a tenant prefix — adding logic to `KeyManager`,
`RefreshStore`, and all Redis commands. Assigning the tenantID as the `KeyPrefix` at
construction time isolates each tenant's Redis key space without any call-site logic. The same
tenantID flows into the jwtauth library `Namespace`, ensuring Prometheus metrics and internal
state are also scoped per tenant.

## Alternatives Considered

**Single shared Redis key space** — rejected. Key ID collisions across tenants are possible when
different tenants use the same key rotation schedule. Requires compound key construction at every
Redis call site and does not isolate observability signals.

**Per-tenant process isolation** — rejected. Deploying a separate process per tenant is
operationally disproportionate for the granularity needed. In-process namespace isolation
achieves the same data separation with a single binary and a shared connection pool.

## Consequences

**Positive:**
- Tenants can be onboarded and offboarded at runtime without a service restart.
- Each tenant's Redis keys, refresh tokens, JWT keys, and metrics are strictly isolated.
- `KeyManager` lifecycle is owned by the registry — no external bookkeeping required.
- The `TenantRegistry` interface is unchanged; callers (`TokenHandler`, health checkers, JWKS
  handler, `CursorReconciler`) are unaffected by the implementation switch.

**Negative:**
- Callers of `Remove` must coordinate with `Drain` and wait for in-flight requests to complete.
  There is no built-in fence — the operator provides the timing.
- `AllKeyManagers()` and `GetAll()` must iterate the live tenant map under a read lock. These
  methods are called by health checkers, the JWKS handler, and the reconciler on every tick.

## References

- [internal/registry/multi_tenant.go](../../internal/registry/multi_tenant.go)
- [internal/registry/tenant.go](../../internal/registry/tenant.go) — `TenantRegistry` interface and `TenantConfig`
- [doc/ARCHITECTURE.md](../ARCHITECTURE.md) — Interface Seams section
