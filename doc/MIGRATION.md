# Migration Guide

This file documents the operator actions required when upgrading token-engine across versions.
Each section lists what changed, what operators must do, and what happens if the action is
skipped. For the full pre-upgrade checklist — Redis backup, cert-manager health, and jwtauth
API surface audits — see [pre-upgrade-runbook.md](pre-upgrade-runbook.md).

---

## v0.1.0 → v0.2.0

### What changed

- `go-redis` promoted to a direct dependency; `StaticTenantRegistry` is now Redis-backed (was
  a NoOp stub in v0.1).
- `LibraryPrometheusMetrics` adapter wired — Prometheus metrics now emitted for all gRPC calls.

### Required actions

- Ensure Redis is reachable at `TOKEN_ENGINE_REDIS_ADDR` before starting v0.2.

### Consequences if skipped

- Service fails to start or reports unhealthy — tenant registry initialization depends on Redis
  connectivity.

---

## v0.2.0 → v0.3.0

### What changed

- Revocation RPCs (`RevokeToken`, `RevokeAllForAudience`, `RevokeAllUserTokens`) promoted from
  NoOp stubs to full implementations backed by `SlogAuditStore`.
- `AuditChecker` wired into `/healthz/ready` — readiness now returns `503` if the audit store
  is unreachable.
- New optional config: `TOKEN_ENGINE_JWKS_CACHE_MAX_AGE` (default `5m`).

### Required actions

- No mandatory config changes.
- Update any health-check automation that asserts `/healthz/ready` always returns `200` — it
  will now return `503` under Redis unavailability.

### Consequences if skipped

- Revocation RPCs silently succeeded (NoOp) in v0.2; from v0.3 they actually revoke tokens.
  Callers that relied on the old no-op behavior will observe real revocation.

---

## v0.3.0 → v0.4.0

### What changed

- `IdempotencyStore` promoted from in-memory (ephemeral, per-process) to Redis-backed
  (`RedisIdempotencyStore`) — deduplication now survives restarts and spans replicas.
- `TOKEN_ENGINE_IDEMPOTENCY_TTL` **default changed from `5m` to `24h`**.
- `IdempotencyInterceptor` promoted from stub to full implementation — `IssueToken` and
  `RefreshToken` requests are now actually deduplicated.

### Required actions

- Set `TOKEN_ENGINE_IDEMPOTENCY_TTL` to at least `2 × max_caller_retry_duration` (see operator
  guide §3). If the prior `5m` default was intentional, set it explicitly.

### Consequences if skipped

- With the 24h default, duplicate `IssueToken`/`RefreshToken` calls within 24h of first
  issuance return the cached response — callers expecting a fresh token on retry will receive
  a stale one.

---

## v0.4.0 → v0.5.0

### What changed

- `StaticTenantRegistry` removed — `MultiTenantRegistry` is the sole tenant registry
  implementation (see [ADR-007](adr/ADR-007-multi-tenant-registry.md)).
- TLS mode selection made explicit via `TOKEN_ENGINE_TLS_MODE`:
  - `mtls` — `MTLSAuthenticator` enforced; callers must present a valid client certificate.
    Requires cert/key/CA files and a caller registry YAML.
  - `disabled` — `StaticKeyAuthenticator` via `TOKEN_ENGINE_STATIC_CALLER_KEYS`.
- **Four new config fields required when `TOKEN_ENGINE_TLS_MODE=mtls`:**

  | Variable | Description |
  |---|---|
  | `TOKEN_ENGINE_TLS_CERT_FILE` | Path to server TLS certificate. |
  | `TOKEN_ENGINE_TLS_KEY_FILE` | Path to server TLS private key. |
  | `TOKEN_ENGINE_TLS_CA_FILE` | Path to CA certificate for client verification. |
  | `TOKEN_ENGINE_CALLER_REGISTRY_PATH` | Path to caller registry YAML mapping cert CNs to caller identities. |

- New RPC: `RevokeAllForUserAndAudience`.
- **Single-replica constraint:** distributed locks are not yet present — run exactly one replica
  until v0.6.

### Required actions

1. **Set `TOKEN_ENGINE_TLS_MODE` explicitly.** The default is `mtls`; set `TOKEN_ENGINE_TLS_MODE=disabled`
   if running without TLS.
2. **For mTLS deployments:** provision server cert/key/CA and populate the three
   `TOKEN_ENGINE_TLS_*_FILE` variables. Create `caller-registry.yaml` and set
   `TOKEN_ENGINE_CALLER_REGISTRY_PATH`.
3. Confirm `spec.replicas == 1` before upgrading (see pre-upgrade runbook §1).

### Consequences if skipped

- Service fails to start if `TOKEN_ENGINE_TLS_MODE=mtls` and cert/key/CA files are missing.
- Missing `TOKEN_ENGINE_CALLER_REGISTRY_PATH` with `mtls` mode: all callers are permitted
  (no authorization enforcement).

---

## v0.5.0 → v0.6.0

### What changed

- **Four new config fields** (all optional with defaults):

  | Variable | Default | Description |
  |---|---|---|
  | `TOKEN_ENGINE_LOCK_TTL` | `30s` | TTL for distributed lock keys in Redis. |
  | `TOKEN_ENGINE_RECONCILIATION_INTERVAL` | `5m` | Time between reconciliation passes. |
  | `TOKEN_ENGINE_RECONCILIATION_PAGE_SIZE` | `100` | Tokens fetched per page during reconciliation. |
  | `TOKEN_ENGINE_ROTATION_WINDOW_GUARD` | `1m` | Minimum time since last key rotation before a new rotation is attempted. |

- `CursorReconciler` now active (was `NoOpReconciler` in v0.1–v0.5) — reconciliation passes run
  on every `TOKEN_ENGINE_RECONCILIATION_INTERVAL` tick.
- `RefreshToken` idempotency now active (was pass-through in v0.1–v0.5).
- Distributed locks guard key rotation and reconciliation — **single-replica constraint lifted**.
- New metric: `token_engine_jwks_key_count` gauge emitted on every JWKS request.

### Required actions

1. **Set `TOKEN_ENGINE_LOCK_TTL` conservatively.** The default `30s` is safe for most deployments.
   If Redis write p99 latency is elevated, increase the value — it must outlast a full key rotation
   write sequence under degraded conditions.
2. **Tune reconciliation.** With high token volume, reduce `TOKEN_ENGINE_RECONCILIATION_PAGE_SIZE`
   or increase `TOKEN_ENGINE_RECONCILIATION_INTERVAL` to avoid prolonged Redis scan passes.
3. **Verify reconciliation starts.** Within one interval of startup, inspect logs for entries
   with the `reconciler:` prefix.
4. **Scale up only after validation.** Increase replicas above 1 only after confirming lock
   acquisition under production load (see operator guide §11).

### Consequences if skipped

- `TOKEN_ENGINE_LOCK_TTL` too short: the lock expires mid-rotation, allowing a second replica to
  acquire the same lock and race on key state.
- Large `TOKEN_ENGINE_RECONCILIATION_PAGE_SIZE` on a large token store can cause Redis scan
  latency spikes during each reconciliation pass.

---

## v0.6.0 → v0.7.0

### What changed

- jwtauth dependency upgraded from v0.7.2 to v1.0.0.
- **Span attribute key changes:** jwtauth v1.0.0 renames internal OpenTelemetry span attribute
  keys — tracing dashboards querying jwtauth-origin span attributes require updating.
- `tokens.Manager` interface grows from 19 to 20 methods — regenerate mocks if maintaining
  custom mocks of this interface.
- No new environment variables.

### Required actions

1. Run the library API surface audit procedure (pre-upgrade runbook §2).
2. Verify error sentinel names — `ErrTokenExpired`, `ErrTokenRevoked`, `ErrTokenNotFound`,
   `ErrKeyNotFound` (pre-upgrade runbook §3).
3. Snapshot Prometheus metrics before and after upgrade in staging to catch any renames
   (pre-upgrade runbook §4).
4. Update tracing dashboards for any renamed jwtauth span attribute keys.

### Consequences if skipped

- Renamed span attributes produce silent gaps in tracing dashboards — old queries return no
  data for spans emitted after the upgrade.
- Stale mock interfaces cause compilation failures if regeneration is skipped.

---

## v0.7.0 → v0.8.0

### What changed

- `client/` Go SDK package added — callers can now import `github.com/aetomala/token-engine/client`
  instead of raw generated stubs.
- `examples/grpc-client` and `examples/mtls-client` added as reference clients.
- Documentation consolidated — `docs/` merged into `doc/`; `doc/MIGRATION.md` added;
  ADR-007 through ADR-010 filed.

### Required actions

None. No environment variables changed, no behavioral changes.

### Consequences if skipped

None.

---

## v0.8.0 → v0.9.0

### What changed

- `docker-compose.yaml` added — single-command local stack (`docker compose up`).
- `examples/custom-claims` and `examples/multi-tenant` added as runnable reference programs.
- All four examples restructured as independent Go modules with per-example READMEs.

### Required actions

None. No environment variables changed, no behavioral changes.

### Consequences if skipped

None.

---

## v0.9.0 → v1.0.0

### What changed

- **`RefreshToken` RPC now returns a replacement `refresh_token`.** Previously the field was
  always empty — the presented refresh token was revoked and no replacement was issued, locking
  the client out after one refresh call. From v1.0.0 the RPC performs true token rotation: the
  response `refresh_token` holds the next refresh token and the presented one is atomically
  revoked.
- **`IssueToken` and `RefreshToken` now populate `access_token_expires_in` and
  `refresh_token_expires_in`** with seconds-until-expiry. Both fields were always zero in
  earlier releases.
- **`TLS_MODE=disabled` no longer requires `TOKEN_ENGINE_CALLER_REGISTRY_PATH`.** When no
  registry file is configured in disabled-TLS mode, all callers are permitted and a startup
  warning is logged. In v0.9.0 and earlier an unconfigured registry caused `PermissionDenied`
  on every tenant-scoped RPC.
- **New `/healthz/ready` check: `reconciler`.** `NewReconcilerChecker` reports unhealthy if
  `CursorReconciler` has not completed a pass within 2× `TOKEN_ENGINE_RECONCILIATION_INTERVAL`
  (default threshold: 10 minutes). Health-check automation that parses check names in the
  `503` body should add `reconciler` to the expected set.

### Required actions

1. **Update clients to consume the `refresh_token` field from `RefreshToken` responses.**
   Clients that ignored the field (because it was always empty) must now read and store it —
   the returned token is the only valid refresh token for the next rotation.
2. **Verify health-check automation.** If any alerting rule or readiness probe asserts a fixed
   set of check names in the `/healthz/ready` body, add `reconciler` to the expected set.

### Consequences if skipped

- Clients that discard `RefreshToken`'s `refresh_token` response field will lose their refresh
  token after each call, requiring re-authentication via `IssueToken`.
- Health alerting that expects exactly the pre-v1.0 set of check names may fire false positives.
