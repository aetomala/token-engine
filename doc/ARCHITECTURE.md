# token-engine — Architecture

## Overview

token-engine is a gRPC service that exposes [jwtauth](https://github.com/aetomala/jwtauth) — a stateful JWT authorization library — as a multi-tenant network API. Its design goals are:

- **Thin service layer.** All token business logic lives in jwtauth. token-engine is responsible only for transport, multi-tenancy, authentication, observability, and lifecycle management.
- **Observability-first.** Every request produces a trace span, increments counters, and emits structured log entries. All three signals are injectable — components never construct their own observability primitives.
- **Progressive implementation.** v0.1 establishes the full service skeleton with NoOp stubs for components that require Redis. Each deferred feature has an interface seam and a NoOp implementation — the service runs correctly without the feature present, and the feature can be wired in later without API changes.

---

## Component Model

```
cmd/token-engine/main.go
├── Config (internal/config)
├── Observability
│   ├── Logger        (internal/observability — SlogLogger)
│   ├── Metrics       (internal/observability — PrometheusMetrics)
│   └── Tracer        (internal/observability — OtelTracer)
├── Authenticator     (internal/interceptor — StaticKeyAuthenticator)
├── Registries
│   ├── CallerRegistry  (internal/registry — StaticCallerRegistry)
│   └── TenantRegistry  (internal/registry — StaticTenantRegistry, NoOp in v0.1)
├── Stores
│   └── IdempotencyStore (internal/store — in-memory, v0.1)
├── Audit             (internal/audit — NoOpAuditStore, v0.1)
├── Reconciliation    (internal/reconciliation — NoOpReconciler, v0.1)
├── Handler           (internal/handler — TokenHandler, delegates to jwtauth)
├── Health            (internal/health — LiveHandler, ReadyHandler)
├── gRPC Server       (interceptor chain + TokenEngine service)
└── HTTP Server       (/healthz/live, /healthz/ready, /metrics)
```

---

## Interceptor Chain

Every gRPC request passes through six interceptors in this order:

```
Client Request
    │
    ▼
┌─────────────────────────┐
│ 1. OTel Tracing         │  starts trace span; propagates W3C trace context
├─────────────────────────┤
│ 2. Correlation ID       │  attaches a UUID to context; logged on every entry
├─────────────────────────┤
│ 3. Authentication       │  validates API key from gRPC metadata; sets caller identity
├─────────────────────────┤
│ 4. Caller Authorization │  checks caller identity against allowed callers per tenant
├─────────────────────────┤
│ 5. Idempotency          │  deduplicates requests by idempotency key within TTL
├─────────────────────────┤
│ 6. Validation           │  validates request fields (stub in v0.1)
└─────────────────────────┘
    │
    ▼
Handler → jwtauth → Response
```

See [ADR-006](adr/ADR-006-interceptor-chain-order.md) for the rationale behind this ordering.

---

## Request Lifecycle

```
Client
  │  gRPC call + Authorization metadata header
  ▼
OTel interceptor        — extracts/creates trace context; starts server span
  │
  ▼
Correlation interceptor — generates UUID; stores in context; logs to all downstream log lines
  │
  ▼
Auth interceptor        — reads Authorization header; looks up API key in StaticKeyAuthenticator
  │  ← UNAUTHENTICATED if key missing or unrecognized
  ▼
CallerAuthz interceptor — checks caller identity against tenant's allowed callers list
  │  ← PERMISSION_DENIED if caller not authorized
  ▼
Idempotency interceptor — checks in-memory store for duplicate idempotency key
  │  ← returns cached response if duplicate within TTL
  ▼
Validation interceptor  — validates required fields (stub; pass-through in v0.1)
  │
  ▼
TokenHandler            — delegates to jwtauth (KeyManager, TokenManager, RefreshStore)
  │  ← maps jwtauth errors to gRPC status codes via MapLibraryError
  ▼
Client ← TokenPair / RevokeTokenResponse
```

---

## Observability Architecture

Every component receives three observability fields injected at construction time:

| Signal | Interface | Default |
|---|---|---|
| Logging | `observability.Logger` | `SlogLogger` (structured JSON) |
| Metrics | `observability.Metrics` | `PrometheusMetrics` (Prometheus registry) |
| Tracing | `observability.Tracer` | `OtelTracer` (OTLP export) or no-op |

**Rule:** No component nil-checks observability fields at call sites. The constructor injects NoOp implementations when config fields are absent. This ensures zero-cost observability when not configured, with no if-nil guards scattered through business logic.

**Correlation ID:** The correlation interceptor stores a UUID in the request context using `CorrelationIDKey{}`. The `SlogLogger` reads this key on every log call and includes it as `correlation_id`. All log entries for a single request share the same correlation ID regardless of which component emits them.

---

## jwtauth Integration

token-engine delegates all token business logic to `github.com/aetomala/jwtauth` v0.7.0.

The `TokenHandler` uses three jwtauth components:

| Component | Purpose |
|---|---|
| `keys.KeyManager` | Key lifecycle — rotation, loading, JWKS |
| `tokens.TokenManager` | Token issuance, refresh, revocation, validation |
| `storage.RefreshStore` | Persistent refresh token storage |

token-engine does not implement any JWT signing, key management, or token storage logic. It provides the transport, multi-tenancy, and observability layers that jwtauth does not include by design.

**Error mapping:** jwtauth errors are converted to gRPC status codes in `observability.MapLibraryError`. Package ownership of each sentinel is verified from jwtauth v0.7.0 source:

| Sentinel | Package | gRPC Code |
|---|---|---|
| `ErrTokenRevoked` | `pkg/tokens` | `PERMISSION_DENIED` |
| `ErrTokenNotFound` | `pkg/storage` | `NOT_FOUND` |
| `ErrInvalidAudience` | `pkg/tokens` | `PERMISSION_DENIED` |
| `ErrKeyStoreInvalidKeyID` | `pkg/keys` | `INTERNAL` |
| `ErrTokenMissingKid` | `pkg/tokens` | `INTERNAL` |
| `ErrTokenExpired` | `pkg/tokens` | `UNAUTHENTICATED` |

---

## v0.1 Deferrals

The following components are present as interface seams with NoOp implementations in v0.1. They produce correct behavior (no panics, no errors) with zero side effects.

| Component | Interface | v0.1 Implementation | Target Version |
|---|---|---|---|
| Audit logging | `audit.AuditStore` | `NoOpAuditStore` | v0.2 (Redis) |
| Token reconciliation | `reconciliation.TokenReconciler` | `NoOpReconciler` | v0.2 |
| Dynamic tenant registry | `registry.TenantRegistry` | `StaticTenantRegistry` (static config) | v0.2 (Redis) |
| Idempotency store | `store.IdempotencyStore` | In-memory map with TTL | v0.2 (Redis) |
| Caller registry | `registry.CallerRegistry` | `StaticCallerRegistry` | v1.0 |

---

## Package Layout

| Package | Import Path | Role |
|---|---|---|
| `cmd/token-engine` | `github.com/aetomala/token-engine/cmd/token-engine` | Binary entry point, wiring |
| `internal/config` | `.../internal/config` | Environment-based configuration loading |
| `internal/observability` | `.../internal/observability` | Logger, Metrics, Tracer interfaces and implementations |
| `internal/interceptor` | `.../internal/interceptor` | gRPC interceptor chain (auth, correlation, OTel, idempotency, validation) |
| `internal/registry` | `.../internal/registry` | Caller and tenant identity registries |
| `internal/handler` | `.../internal/handler` | gRPC service implementation delegating to jwtauth |
| `internal/health` | `.../internal/health` | HTTP liveness and readiness handlers |
| `internal/store` | `.../internal/store` | Idempotency store |
| `internal/audit` | `.../internal/audit` | Audit logging interface and NoOp |
| `internal/reconciliation` | `.../internal/reconciliation` | Token reconciliation interface and NoOp |
| `internal/testutil` | `.../internal/testutil` | Generated mocks for all interfaces |
| `gen/v1` | `github.com/aetomala/token-engine/gen/v1` | Generated protobuf and gRPC stubs |

---

## Testing Strategy

Tests use Ginkgo v2 (BDD) with Gomega matchers. All suites run with the race detector.

Each package has a suite bootstrap file (`*_suite_test.go`) and a test file organized into numbered phases:

1. Constructor and initialization
2. Default configuration
3. Core operations
4. Error cases
5. Concurrency (where applicable)

Mocks are generated with `go.uber.org/mock/mockgen` in source mode. All mocks live in `internal/testutil/`. Tests use black-box testing (`package xxx_test`) to validate the public API surface.

---

## Roadmap

| Version | Target | Key Work |
|---|---|---|
| v0.1 | ✅ Complete | Full service skeleton, static auth, in-memory idempotency, NoOp stubs |
| v0.2 | Planned | Redis client lifecycle, Redis-backed idempotency + audit + reconciliation, dynamic tenant registry |
| v1.0 | Planned | Stable API commitment, dynamic caller registry, multi-region deployment guide |

---

## Architecture Decision Records

| ADR | Decision |
|---|---|
| [ADR-001](adr/ADR-001-grpc-first-transport.md) | gRPC as the primary transport |
| [ADR-002](adr/ADR-002-jwtauth-library-delegation.md) | Token business logic delegated entirely to jwtauth |
| [ADR-003](adr/ADR-003-static-caller-keys-v01.md) | Static API key authentication for v0.1 |
| [ADR-004](adr/ADR-004-noop-stubs-v01.md) | NoOp stubs for audit, reconciliation, and tenant registry in v0.1 |
| [ADR-005](adr/ADR-005-in-memory-idempotency-v01.md) | In-memory idempotency store for v0.1 |
| [ADR-006](adr/ADR-006-interceptor-chain-order.md) | Interceptor chain ordering rationale |
