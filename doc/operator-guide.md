# Token Engine Operator Guide

## 1. Architecture Overview

Token Engine exposes two network interfaces:

- **gRPC** on `:9090` — handles `IssueToken`, `RefreshToken`, `RevokeToken`, `RevokeAllForAudience`, `RevokeAllUserTokens`, and `RevokeAllForUserAndAudience` RPCs.
- **HTTP** on `:8080` — serves JWKS at `/.well-known/jwks.json`, health endpoints at `/healthz/live` and `/healthz/ready`, and Prometheus metrics at `/metrics`.

Both listeners start simultaneously. The gRPC port is the primary RPC surface; the HTTP port is read-only and supports health probes and metric scraping. Startup probes should target the HTTP port to avoid gating readiness on gRPC listener availability.

## 2. Deployment Modes

Token Engine supports two TLS modes controlled by `TOKEN_ENGINE_TLS_MODE`:

- **`mtls`** — mutual TLS is enforced on the gRPC listener. Callers must present a valid client certificate signed by the configured CA. Requires `TOKEN_ENGINE_TLS_CERT_FILE`, `TOKEN_ENGINE_TLS_KEY_FILE`, and `TOKEN_ENGINE_TLS_CA_FILE` to be set. gRPC reflection is disabled.
- **`disabled`** — no TLS is applied. gRPC reflection is enabled for development and debugging. Do not use in production without a TLS-terminating proxy.

## 3. Configuration Reference

All configuration is read from environment variables at startup. Missing required values cause the process to exit with a non-zero status.

| Environment Variable | Default | Description |
|---|---|---|
| `TOKEN_ENGINE_ISSUER` | _(required)_ | JWT `iss` claim value; also used as tenant identifier. |
| `TOKEN_ENGINE_AUDIENCE` | _(required)_ | JWT `aud` claim value. |
| `TOKEN_ENGINE_REDIS_ADDR` | `localhost:6379` | Redis server address (`host:port`). |
| `TOKEN_ENGINE_REDIS_PASSWORD` | _(empty)_ | Redis password. |
| `TOKEN_ENGINE_REDIS_DB` | `0` | Redis database index. |
| `TOKEN_ENGINE_TLS_MODE` | `mtls` | TLS mode: `mtls` or `disabled`. |
| `TOKEN_ENGINE_TLS_CERT_FILE` | _(empty)_ | Path to server TLS certificate (mtls only). |
| `TOKEN_ENGINE_TLS_KEY_FILE` | _(empty)_ | Path to server TLS private key (mtls only). |
| `TOKEN_ENGINE_TLS_CA_FILE` | _(empty)_ | Path to CA certificate for client verification (mtls only). |
| `TOKEN_ENGINE_GRPC_ADDR` | `:9090` | gRPC listener bind address. |
| `TOKEN_ENGINE_HTTP_ADDR` | `:8080` | HTTP listener bind address. |
| `TOKEN_ENGINE_JWKS_CACHE_MAX_AGE` | `5m` | Controls the `Cache-Control: max-age` value returned by the JWKS endpoint. |
| `TOKEN_ENGINE_IDEMPOTENCY_TTL` | `24h` | Idempotency key TTL in Redis. Must be at least 2x the caller's maximum retry duration. |
| `TOKEN_ENGINE_LOCK_TTL` | `30s` | TTL for all distributed lock keys. Must be long enough for a full key rotation write under degraded Redis. |
| `TOKEN_ENGINE_RECONCILIATION_INTERVAL` | `5m` | Time between reconciliation passes. |
| `TOKEN_ENGINE_ROTATION_WINDOW_GUARD` | `1m` | Minimum elapsed time since last key generation before a new rotation is attempted. |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | _(empty)_ | OTLP gRPC endpoint for trace export. Tracing is disabled when empty. |
| `TOKEN_ENGINE_CALLER_REGISTRY_PATH` | _(empty)_ | Path to caller registry YAML. All callers are permitted when empty. |
| `TOKEN_ENGINE_STATIC_CALLER_KEYS` | _(empty)_ | Comma-separated static API keys for non-mtls authentication. |
| `TOKEN_ENGINE_MAX_CONNECTION_AGE` | `30m` | gRPC keepalive `MaxConnectionAge`. |
| `TOKEN_ENGINE_MAX_CONNECTION_AGE_GRACE` | `5m` | gRPC keepalive `MaxConnectionAgeGrace`. |
| `TOKEN_ENGINE_BACKFILL_EXPIRY_INDEX` | `false` | Runs the one-time jwtauth v1.1.0 expiry-index migration at startup when `true`. See [§12](#12-one-time-expiry-index-backfill-after-jwtauth-v110-upgrade). |

## 4. Multi-Audience Token Design Guidance

A single token carries exactly one `aud` claim at issuance time. When a token is revoked, only that token is invalidated; other tokens issued to the same subject but for different audiences remain valid.

This is the intended behaviour for most use cases. If you require independent revocability per audience, issue separate tokens per audience at the call site — do not attempt to reuse a single token across multiple audiences. Treating a token as multi-audience is semantically incorrect and will cause partial revocation failures because `RevokeAllForAudience` operates on a per-audience scope (see ADR-009).

Atomic revocation semantics: calling `RevokeToken` invalidates the specific JTI atomically in Redis. Calling `RevokeAllForAudience` marks all refresh tokens for the given subject+audience as revoked in a single pass. Access tokens are short-lived and rely on expiry for invalidation.

## 5. RevokeAllForAudience Redis Concurrency Caveat

`RevokeAllForAudience` iterates and marks refresh tokens for the given subject+audience. If new `IssueToken` calls race against the revocation scan, tokens issued after the scan has passed their key may not be revoked.

For strict guarantee that no valid tokens survive after `RevokeAllForAudience`, quiesce new token issuance for the subject+audience before calling this RPC. In practice, this means coordinating at the application layer (for example, setting a short-lived "no-issue" flag in Redis before calling revocation).

## 6. Lock TTL Co-Design Constraint

`TOKEN_ENGINE_LOCK_TTL` must be set long enough to accommodate the worst-case duration of a full key rotation write sequence under degraded Redis conditions (high latency, slow followers). If `LockTTL` expires before rotation completes, a competing replica may acquire the lock and attempt a concurrent rotation, resulting in unnecessary key material.

Rule of thumb: set `LockTTL` to at least 3× the observed p99 Redis write latency multiplied by the number of sequential writes in a rotation. The default of `30s` is conservative for local or low-latency Redis deployments.

## 7. IdempotencyTTL Co-Design Constraint

`TOKEN_ENGINE_IDEMPOTENCY_TTL` governs how long an idempotency key is retained in Redis after the first request. A second request with the same key within this window returns the cached response without calling the underlying handler.

Constraint: `IdempotencyTTL` must be at least 2× the caller's maximum retry duration. If a caller retries after the TTL has expired, the request is treated as new and may issue a duplicate token. Callers with retry windows longer than `IdempotencyTTL / 2` will not receive idempotent protection.

## 8. JTI Replay Prevention Guidance (R2)

Token Engine does not maintain a JTI revocation cache for access tokens. Access tokens are short-lived by design; enforcement of the "no-replay after expiry" invariant is delegated to the operator's middleware.

Operator responsibility (R2): implement a JTI cache in your API gateway or middleware that records used JTIs for at least the access token lifetime. Reject any request presenting a JTI already recorded in the cache, even if the token signature is valid and the expiry has not passed.

## 9. TOCTOU Concurrent Refresh Race and Single-Flight Mitigation (R1)

A TOCTOU race exists when two concurrent `RefreshToken` RPCs arrive with the same refresh JTI before either has completed. Both may read the token as valid, then both attempt to issue a new access token and rotate the refresh token. The second write will fail or succeed depending on Redis atomicity, but the first caller may observe a revoked refresh token on their next call.

Mitigation pattern (R1): implement single-flight deduplication at the API gateway keyed on the refresh JTI. Only one in-flight refresh per JTI should reach Token Engine at a time. The idempotency interceptor provides a second layer of protection for callers that include an `X-Idempotency-Key` header, but single-flight is the preferred mitigation because it operates without requiring client cooperation.

## 10. RS256 Algorithm Invariant Guidance (R3)

Token Engine always issues RS256-signed JWTs. The signing algorithm is not configurable. Middleware that validates tokens must pin the expected algorithm to `RS256` and reject tokens with any other `alg` header value, including `none`.

Operator guidance (R3): configure your JWT validation library to require `RS256` explicitly. Monitor your observability pipeline for unexpected algorithm values in token validation logs — these indicate either misconfiguration or a downgrade attempt. Alert on any non-RS256 algorithm appearing in validated tokens.

## 11. Pre-v0.6 Single-Replica Constraint

Before v0.6, Token Engine required single-replica deployment to avoid concurrent key rotation and reconciliation races. Running multiple replicas without coordination would result in duplicate key material and inconsistent refresh token state.

v0.6 lifts this constraint by introducing distributed locks (`internal/lock.RedisLock`) for all key rotation and reconciliation operations. Each replica acquires a per-tenant lock before rotating keys or running a reconciliation pass. The single-replica note in `deploy/k8s/deployment.yaml` reflects a conservative validation posture for the initial v0.6 rollout — once lock acquisition behaviour is confirmed under production load, replicas can be increased.

## 12. One-Time Expiry-Index Backfill After jwtauth v1.1.0 Upgrade

jwtauth v1.1.0 replaced `RefreshStore.Cleanup`'s full-keyspace scan with an expiry index populated at `Store` time. Tokens stored before the upgrade are never added to that index, so on an existing Redis-backed deployment they must be migrated once per tenant via `RedisRefreshStore.BackfillExpiryIndex`, or their entries in the `user_tokens:`, `audience_tokens:`, and `audience_user_tokens:` membership sets persist indefinitely — silently, with no error and no functional breakage.

Set `TOKEN_ENGINE_BACKFILL_EXPIRY_INDEX=true` and restart the service during an upgrade window. The migration runs synchronously at startup, before the gRPC and HTTP servers begin serving traffic, and logs a line per tenant:

```
expiry index backfill complete tenant_id=<tenant> removed=<n> indexed=<n>
```

A per-tenant failure logs a warning and does not block startup or the remaining tenants — retry by restarting with the flag still set. **Unset `TOKEN_ENGINE_BACKFILL_EXPIRY_INDEX` after a successful run.** The migration is idempotent — `BackfillExpiryIndex` is safe to run more than once, including concurrently with live traffic — but leaving the flag set means every subsequent restart re-runs the same full-keyspace scan the v1.1.0 upgrade exists to eliminate.

This step is not required for tenants added after upgrading to jwtauth v1.1.0 — their tokens are indexed at `Store` time from the start.
