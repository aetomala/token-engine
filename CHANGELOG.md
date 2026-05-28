# Changelog

All notable changes to token-engine are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

---

## [v0.2.0] — 2026-05-27

### Added

- `LibraryPrometheusMetrics` adapter — bridges jwtauth's `metrics.Recorder` interface to the shared Prometheus registry; promotes `go-redis` to a direct dependency
- `CorrelationInterceptor` now emits gRPC request count and duration metrics on every call
- `ValidationInterceptor` — rejects requests with an empty `sub` claim or any reserved claim key before the request reaches the handler
- `StaticTenantRegistry` — real Redis-backed implementation replacing the v0.1 NoOp stub; manages the full jwtauth key/token stack per tenant
- `TokenHandler` — real `IssueToken` and `RefreshToken` implementations replacing the v0.1 NoOp stub; maps jwtauth sentinel errors to canonical gRPC status codes
- `RedisChecker` and `KeyAvailabilityChecker` — wired into `/healthz/ready` so the readiness probe reflects Redis connectivity and key availability
- `main.go` rewired for v0.2 — Redis client lifecycle, updated constructor signatures, health checkers, and graceful shutdown order
- `MockKeyManager` — generated mock for `keys.KeyManager` to support handler and registry tests
- Comprehensive test suite for all v0.2 components (phases 9A–9D): LibraryPrometheusMetrics, CorrelationInterceptor metrics, ValidationInterceptor, RedisChecker, KeyAvailabilityChecker, StaticTenantRegistry, TokenHandler — handler 97.7%, interceptor 97.1%, health 100%, registry 82.6%, observability 100%

### Version Deferrals (v0.3+)

- Audit logging — NoOp stub remains; Redis-backed implementation deferred
- Token reconciliation — NoOp stub remains; deferred
- Redis-backed idempotency store — in-memory store remains; cross-replica consistency deferred

---

## [v0.1.0] — 2026-05-27

### Added

- gRPC token service wrapping jwtauth v0.7.0
- IssueToken, RefreshToken, RevokeToken, RevokeAllForAudience, RevokeAllUserTokens RPCs
- Multi-tenant support via static caller and tenant registry
- Request idempotency (in-memory store, per-session; TTL configurable via TOKEN_ENGINE_IDEMPOTENCY_TTL)
- Auth interceptor with static API key authentication (TOKEN_ENGINE_STATIC_CALLER_KEYS)
- Interceptor chain: OTel → Correlation → Auth → CallerAuthz → Idempotency → Validation
- Prometheus metrics (5 instruments: request count, duration, active requests, auth failures, idempotency hits)
- Structured slog logging with namespace and correlation ID propagation
- OpenTelemetry tracing with OTLP/gRPC export
- Health endpoints: /healthz/live (liveness), /healthz/ready (readiness with jwtauth ping)
- gRPC and HTTP servers with optional TLS and graceful shutdown on SIGINT/SIGTERM

### Version Deferrals (v0.2)

- Audit logging: NoOp stub present; Redis-backed implementation deferred
- Token reconciliation: NoOp stub present; deferred
- Dynamic tenant registry: static configuration in v0.1; Redis-backed deferred
- Idempotency store: in-memory in v0.1; Redis-backed store for cross-replica consistency deferred
