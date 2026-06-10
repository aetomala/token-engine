# Changelog

All notable changes to token-engine are documented here.

Format follows [Keep a Changelog](https://keepachangelog.com/en/1.0.0/).
Versions follow [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

---

## [Unreleased]

### Added

- Added `client/` package: Go client SDK wrapping the generated gRPC stubs; `NewClient` with functional options for mTLS (`WithMTLS`), plaintext (`WithPlaintext`), static API key (`WithStaticKey`), connect timeout (`WithConnectTimeout`), and interceptor injection (`WithUnaryInterceptor`); `NoOpClient` for tests; `NewClientFromStub` for unit-test stub injection; `MockClient` generated in `internal/testutil`
- Added `examples/grpc-client` and `examples/mtls-client` minimal operator example programs using the new client package

### Changed

- Consolidated `docs/` directory into `doc/` — moved `operator-guide.md` and `pre-upgrade-runbook.md` into `doc/`; `docs/` directory removed
- Filed ADR-007 documenting `MultiTenantRegistry` design — `Add`/`Drain`/`Remove` lifecycle rationale, two-phase shutdown semantic, and per-tenant Redis key prefix and namespace isolation
- Filed ADR-008 documenting the mTLS authentication model — CN-based caller identity, `RequireAndVerifyClientCert` rationale, TLS 1.3 minimum, and coexistence with static key auth
- Filed ADR-009 documenting the distributed lock design — SET NX PX acquisition, Lua CAS-delete release rationale, UUID v4 lock token, per-call TTL semantics, and acceptable failure modes for best-effort operations; added missing `internal/lock` row to ARCHITECTURE.md package layout

---

## [v0.7.0] — 2026-06-05

### Added

- Added `NoOpLocker` and `NoOpLock` to `internal/lock` — no-op implementations of the `Locker` and `Lock` interfaces for use in single-node deployments and unit tests that do not require distributed mutual exclusion

### Changed

- Upgraded jwtauth dependency from v0.7.2 to v1.0.0; wired `Namespace` field on `RedisKeyStoreConfig` and `RedisRefreshStoreConfig` for per-tenant observability label decoupling
- Updated ADR-003 status to Superseded; replaced stale Target Version note with an Outcome section documenting mTLS delivery in v0.5.0, coexistence with static key auth, and a forward reference to ADR-008
- Corrected ADR-002 jwtauth pinned version (v0.7.0 → v0.7.2); updated ADR-004 with production implementation outcomes (SlogAuditStore, CursorReconciler, MultiTenantRegistry); rewrote ADR-005 to Superseded with RedisIdempotencyStore delivery, 24h TTL default, RefreshToken coverage, and pre-handler ordering invariant; corrected ADR-006 interceptor naming and idempotency scope; updated ADR README to reflect ADR-003 and ADR-005 Superseded status

### Fixed

- Fixed `docs/operator-guide.md` Architecture Overview to remove the non-existent `IntrospectToken` RPC and add the missing `RevokeAllUserTokens` and `RevokeAllForUserAndAudience` RPCs; the list now matches `proto/token_engine.proto` exactly

---

## [v0.6.0] — 2026-06-03

### Added

- Distributed lock package (`internal/lock`) — `Locker`/`Lock` interfaces with `RedisLock`
  implementation; SET NX PX acquisition with Lua CAS-delete release; guards key rotation and
  reconciliation critical sections against concurrent multi-replica execution
- `CursorReconciler` — cursor-based `Reconciler` replacing `NoOpReconciler`; polls
  Redis-backed refresh token store in pages using a scan cursor; wired in `main.go` with
  configurable interval and page size; resolves v0.5.0 deferral (see ADR-011)
- `RefreshToken` idempotency — idempotency interceptor extended to deduplicate `RefreshToken`
  requests using `IdempotencyStore`; pre-handler `Get()` ordering preserves atomicity; resolves
  v0.5.0 deferral
- JWKS key count metric — `token_engine_jwks_active_key_count` gauge emitted on every JWKS
  request; `JWKSHandler` updated to accept tenant ID and metrics for per-tenant key tracking
- Kubernetes deployment manifest — `deploy/k8s/deployment.yaml` with startup, liveness, and
  readiness probes co-designed with key rotation interval
- Operator runbook (`docs/operator-guide.md`) — covers startup, key rotation, reconciliation,
  and observability procedures
- Pre-upgrade runbook (`docs/pre-upgrade-runbook.md`) — covers pre-upgrade verification steps
- 4 new config fields: `TOKEN_ENGINE_LOCK_TTL` (default 30s), `TOKEN_ENGINE_RECONCILIATION_INTERVAL`
  (default 5m), `TOKEN_ENGINE_RECONCILIATION_PAGE_SIZE` (default 100),
  `TOKEN_ENGINE_ROTATION_WINDOW_GUARD` (default 1m)

### Changed

- jwtauth upgraded v0.7.1 → v0.7.2 — `tokens.TokenManager` interface updated to 20 methods;
  `MockTokenManager` regenerated
- `govulncheck` added to CI — runs on every PR; blocks on known vulnerabilities in dependencies
- `golangci-lint` configuration updated — `revive` and `godot` linters now enforced; exported
  symbol comments and declaration-level punctuation validated across all packages
- Key rotation Redis key prefixes extracted as named constants in `main.go`

### Security

- Go toolchain bumped 1.26.3 → 1.26.4 — addresses GO-2026-5039 and GO-2026-5037, surfaced
  by the newly added `govulncheck` CI step

---

## [v0.5.0] — 2026-05-30

### Added

- `RevokeAllForUserAndAudience` RPC — revokes all refresh tokens for a user within a specific
  audience; audit-gated (returns `codes.Unavailable` if audit store unreachable)
- `MTLSAuthenticator` — extracts caller identity from TLS peer certificate Common Name;
  selected when `TOKEN_ENGINE_TLS_MODE=mtls`
- mTLS gRPC server credentials — loads cert/key/CA from filesystem; enforces
  `RequireAndVerifyClientCert` with TLS 1.3 minimum; wired in `main.go`
- Static YAML caller registry — `CallerRegistryConfig` / `LoadCallerRegistryConfig` decode
  `caller-registry.yaml`; `StaticCallerRegistry.IsPermitted` promoted from stub to real
  implementation; `TOKEN_ENGINE_CALLER_REGISTRY_PATH` config field
- `MultiTenantRegistry` — replaces `StaticTenantRegistry`; supports dynamic `Add`/`Drain`/`Remove`
  with per-tenant `KeyManager` + `RefreshStore` namespace isolation
- `deploy/caller-registry.yaml` — example caller authorization config with format documentation
- Integration test suite extended to 12 specs — added `RevokeAllForUserAndAudience` end-to-end
  test covering revoke → subsequent refresh fails

### Changed

- `StaticTenantRegistry` removed — `MultiTenantRegistry` is now the sole `TenantRegistry`
  implementation; `main.go` wires `Add` on startup, iterates `AllKeyManagers()` for health
  checkers, JWKS handler, and graceful shutdown
- Authenticator selection is now conditional on `TOKEN_ENGINE_TLS_MODE` — `MTLSAuthenticator`
  for `mtls`, `StaticKeyAuthenticator` for `disabled`

### Version Deferrals (v0.6)

- Token reconciliation — `NoOpReconciler` remains; cursor-based implementation deferred to v0.6
- `RefreshToken` idempotency — deferred to v0.6 (requires refresh token rotation + idempotency
  store interaction to be safe)
- Distributed locks — deferred to v0.6 (single-replica deployment required until then)
- Kubernetes manifests — deferred to v0.6

---

## [v0.4.0] — 2026-05-29

### Added

- `RedisIdempotencyStore` — Redis-backed idempotency store replacing the v0.1 in-memory implementation; persists deduplication records across replicas with configurable TTL (default 24h via `TOKEN_ENGINE_IDEMPOTENCY_TTL`); resolves v0.3.0 deferral
- `IdempotencyInterceptor` promoted to full implementation — previously a NoOp stub; now performs live lookups against `RedisIdempotencyStore` and caches responses for the duration of the TTL
- End-to-end gRPC integration test suite — validates the full interceptor chain against a real Redis instance; covers `IssueToken`, `RefreshToken`, and all revocation RPCs including idempotency deduplication behavior

### Fixed

- Shutdown hardening — OTel flush step added so buffered spans reach the collector before exit; gRPC `GracefulStop` bounded by a 10-second sub-deadline with hard stop if exceeded; HTTP server shutdown timeout added; listener bound synchronously before goroutine spawn to eliminate a bind/serve race
- Three production bugs identified and fixed during integration test development

### Version Deferrals (v0.5+)

- Token reconciliation — `NoOpReconciler` remains; cursor-based implementation deferred to v1.0
- mTLS authenticator — static API key authentication remains; mTLS deferred to v0.5
- Dynamic tenant registry — `StaticTenantRegistry` remains; full Redis-backed multi-tenant registry deferred to v0.5
- Caller registry — `StaticCallerRegistry` remains; dynamic YAML-backed registry deferred to v1.0

---

## [v0.3.0] — 2026-05-28

### Added

- `SlogAuditStore` — live audit-store implementation; writes structured log lines via the observability logger for all revocation events (token ID, target, scope, caller identity, timestamp)
- `AuditChecker` — extends `/healthz/ready` readiness probe with audit store connectivity verification; revocation RPCs return `codes.Unavailable` if the audit store is unreachable
- Revocation RPCs fully implemented — previously NoOp stubs in v0.2:
  - `RevokeToken` — revokes individual refresh token; introspects token to resolve token ID; records audit event with `scope="token"`
  - `RevokeAllForAudience` — revokes all tokens for a given audience; records audit event with `scope="audience"`
  - `RevokeAllUserTokens` — revokes all tokens for a given user; records audit event with `scope="user"`
- `JWKSHandler` — fully functional HTTP handler at `/.well-known/jwks.json`; returns 503 on key manager unavailability or empty key set; sets `Cache-Control: public, max-age=N` on success
- `JWKSCacheMaxAge` config field — controls JWKS `Cache-Control` max-age; env var `TOKEN_ENGINE_JWKS_CACHE_MAX_AGE`; defaults to 5 minutes
- CD pipeline — Docker image published to `docker.io/angeltomala/token-engine:<tag>` automatically on `v*` tag push; active starting with this release
- Comprehensive v0.3 test suite: `SlogAuditStore` (100%), `AuditChecker` (100%), config including `JWKSCacheMaxAge` (90.3%), revocation handlers + `JWKSHandler` (92.6%)

### Fixed

- Go runtime bumped 1.26.2 → 1.26.3 — resolves three stdlib security advisories (`GO-2026-4982`, `GO-2026-4980`, `GO-2026-4971`)

### Version Deferrals (v0.4+)

- Redis-backed idempotency store — in-memory TTL-based store remains; cross-replica consistency deferred to v0.4
- Token reconciliation — `NoOpReconciler` remains; cursor-based implementation deferred to v1.0
- mTLS authenticator — static API key authentication remains; mTLS deferred to v0.5
- Dynamic tenant registry — `StaticTenantRegistry` remains; full Redis-backed multi-tenant registry deferred to v0.5
- Caller registry — `StaticCallerRegistry` remains; dynamic YAML-backed registry deferred to v1.0

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
