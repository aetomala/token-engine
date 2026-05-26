# Changelog

All notable changes to token-engine are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

---

## [v0.1.0] — TBD

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
