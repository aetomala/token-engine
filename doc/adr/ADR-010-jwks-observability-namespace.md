# ADR-010: JWKS Per-Tenant Observability Namespace Strategy

**Status:** Complete — v0.5.0  
**Date:** 2026-05-30

## Context

v0.5.0 introduced `MultiTenantRegistry`, which constructs an independent stack of
`RedisKeyStore → KeyManager → RedisRefreshStore → TokenManager` for each tenant
(`internal/registry/multi_tenant.go`, `Add()`, lines 93–134). Each component receives
`Namespace: tenantID` and `KeyPrefix: tenantID`.

The jwtauth library (`github.com/aetomala/jwtauth`) registers its internal metrics at
construction time via `librarymetrics.NewPrometheusMetrics(PrometheusConfig{Namespace: tenantID})`.
The library has no per-call label injection — the namespace string is baked into the metric
name at registration time.

The JWKS HTTP endpoint serves public keys for a given tenant's `KeyManager`. `JWKSHandler`
(`internal/handler/jwks.go`) emits `token_engine_jwks_key_count` after calling `GetAllKeyInfo`
on the tenant's `KeyManager`. This metric is owned by the service, not the library, and was
registered once at startup as a plain `prometheus.Gauge`.

With multiple tenants, two questions arise: how do library-internal metrics stay isolated across
tenants in a shared Prometheus registry, and how does the service attribute the JWKS key count
to the correct tenant?

## Decision

**Two-tier observability namespace strategy:**

**Tier 1 — Library-level metrics (jwtauth internal): namespace prefix.**

`MultiTenantRegistry.Add()` passes `tenantID` as the Prometheus namespace to each tenant's
`TokenManager`. The jwtauth library prepends this to every metric name it registers at
construction:

```
<tenantID>_jwtauth_token_manager_issued_total
<tenantID>_jwtauth_key_manager_rotations_total
...
```

All library metrics for all tenants land on the single shared `*prometheus.Registry` injected
into `MultiTenantRegistry` at construction. Two tenants with IDs `acme` and `globex` produce
entirely distinct metric names and never collide.

The same `tenantID` string also serves as the `Namespace` field for jwtauth's internal spans
(OpenTelemetry) and log enrichment, making it the universal per-tenant identifier across all
three observability signals — metrics, traces, and logs.

**Tier 2 — Service-level metrics (token-engine owned): `tenant_id` label.**

`JWKSHandler` emits `token_engine_jwks_key_count` with labels `{"tenant_id": tenantID}`.
Service-level metrics are registered once at startup; per-tenant attribution is expressed
through labels rather than name prefixes.

**Current implementation gap.** The `PrometheusMetrics.SetGauge` implementation ignores the
labels map — it calls `gauge.Set(value)` on a plain `prometheus.Gauge` (see
`internal/observability/metrics.go`). In a multi-tenant deployment, the gauge reflects the last
tenant to update it (last write wins). To realize the label strategy,
`token_engine_jwks_key_count` must be migrated from `prometheus.Gauge` to `prometheus.GaugeVec`
with a `tenant_id` dimension, and `PrometheusMetrics.SetGauge` must be updated to apply labels.

**One shared Prometheus registry.**

A single `*prometheus.Registry` is passed to `NewMultiTenantRegistry`. There is no per-tenant
registry or per-tenant `/metrics` scrape endpoint. The namespace-prefix strategy (Tier 1) and
the label strategy (Tier 2) both depend on this shared-registry constraint.

## Rationale

**Namespace prefix for library metrics, not labels.**
The jwtauth library pre-registers its metrics at construction via `MustRegister`. It has no
mechanism for per-call label injection. Retrofitting it to use labels would require changes to
the library's public metrics interface. The namespace-prefix approach requires no library
changes: each `Add()` call passes a different `tenantID` and the library registers a distinct
set of metric names. The shared registry remains the authoritative scrape target.

**Labels for service-level metrics, not per-tenant registries.**
Service-level metrics (`token_engine_grpc_requests_total`, `token_engine_jwks_key_count`, etc.)
are registered once, regardless of tenant count. A per-tenant registry would require
tenant-scoped scrape endpoints or a registry-merging shim — both complicate operator monitoring
setup significantly. Label-based attribution is idiomatic Prometheus for service-owned metrics.

**Single tenantID string across all signals.**
Using the same `tenantID` for Redis key prefixes, jwtauth `Namespace`, and Prometheus namespace
gives operators a single filter key: `{tenant_id="acme"}` in Prometheus, `namespace=acme` in
trace attributes, and `tenant=acme` in log fields all resolve to the same tenant without any
cross-signal mapping.

## Alternatives Considered

**Per-tenant Prometheus registry.** Each tenant gets its own `*prometheus.Registry`, scraped at
`/metrics/{tenantID}`. Rejected: requires dynamic scrape target configuration in Prometheus
(not static), and the library's internal metrics would be split across multiple endpoints,
defeating the purpose of a unified `/metrics` scrape.

**Labels for library metrics.** Instrument every jwtauth metric call with a `tenant_id` label
at the library level. Rejected: requires changes to the jwtauth public metrics interface, which
is a versioned external dependency. Namespace prefix achieves equivalent isolation with no
library API changes.

**Separate namespace strings per tier.** Use a different identifier for the library namespace
vs. the Redis key prefix. Rejected: adds an operator mapping burden without benefit. The tenantID
already serves as a stable, unique identifier for all tenant-scoped resources.

## Consequences

**Positive:**
- All tenants' metrics are available at a single `/metrics` scrape endpoint — no dynamic
  scrape configuration required.
- Library and service observability signals share a common `tenantID` key for cross-signal
  correlation (metrics prefix, span attribute, log field).
- No library changes required — the namespace-prefix approach is compatible with jwtauth's
  existing `PrometheusConfig.Namespace` API.
- Adding a new tenant at runtime does not require changes to monitoring configuration.

**Negative:**
- Library metric names grow as O(tenants × library_metric_count). With 18 jwtauth metrics and
  100 tenants, the registry holds ~1,800 metric series — acceptable at current scale but worth
  monitoring as tenant count grows.
- Operators must know the tenantID prefix to query library metrics (`<tenantID>_jwtauth_*`).
  Service-level metrics use the idiomatic `tenant_id` label instead, creating two different
  query patterns for the same tenant in the same scrape endpoint.
- `token_engine_jwks_key_count` is currently a plain `prometheus.Gauge` — the `tenant_id`
  label passed by `JWKSHandler` is silently dropped by `PrometheusMetrics.SetGauge`.
  Multi-tenant deployments observe last-write-wins on this gauge until it is migrated to
  `prometheus.GaugeVec`.

## References

- [internal/registry/multi_tenant.go](../../internal/registry/multi_tenant.go) — `Add()` namespace threading (lines 93–134)
- [internal/handler/jwks.go](../../internal/handler/jwks.go) — `tenant_id` label in JWKS gauge emission (line 38)
- [internal/observability/metrics.go](../../internal/observability/metrics.go) — `MetricJWKSKeyCount` constant; `PrometheusMetrics.SetGauge` label-drop gap
- [doc/adr/ADR-007-multi-tenant-registry.md](ADR-007-multi-tenant-registry.md) — per-tenant stack construction and lifecycle
