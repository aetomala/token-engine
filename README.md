# token-engine

[![CI](https://github.com/aetomala/token-engine/actions/workflows/ci.yml/badge.svg)](https://github.com/aetomala/token-engine/actions/workflows/ci.yml)
[![CD](https://github.com/aetomala/token-engine/actions/workflows/cd.yml/badge.svg)](https://github.com/aetomala/token-engine/actions/workflows/cd.yml)
[![Go 1.26+](https://img.shields.io/badge/go-1.26+-blue.svg)](https://go.dev/dl/)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

A production-grade gRPC service that wraps [jwtauth](https://github.com/aetomala/jwtauth) v1.0.0 and exposes stateful JWT token management as a network API — multi-tenant, observable, and horizontally scalable.

---

## What It Provides

| RPC | Description |
|---|---|
| `IssueToken` | Issue a new access + refresh token pair for a subject |
| `RefreshToken` | Rotate tokens using a valid refresh token |
| `RevokeToken` | Revoke a single refresh token immediately |
| `RevokeAllForAudience` | Revoke all tokens scoped to an audience |
| `RevokeAllUserTokens` | Revoke all tokens for a user across all audiences |
| `RevokeAllForUserAndAudience` | Revoke all tokens for a user within a specific audience |

**Interceptor chain (applied to every RPC):**
OpenTelemetry tracing → Correlation ID → Authentication (API key or mTLS CN) → Caller authorization → Idempotency → Request validation

**Observability:** Prometheus metrics at `/metrics`, OpenTelemetry traces via OTLP, structured slog logging with correlation IDs, health probes at `/healthz/live` and `/healthz/ready`.

---

## Quick Start

### Using Docker Compose (recommended)

```bash
docker compose up          # Docker
# or: podman compose up    # Podman
```

The stack starts Redis and token-engine. Once healthy:

```bash
curl http://localhost:8080/healthz/ready   # → 200 OK
```

The gRPC server is on `:9090` and the HTTP server (health, metrics, JWKS) is on `:8080`.
See [`docker-compose.yaml`](docker-compose.yaml) for all pre-configured environment variables.

### Running directly

```bash
# Build
make build

# Run with minimum required config
TOKEN_ENGINE_ISSUER=my-service \
TOKEN_ENGINE_AUDIENCE=my-api \
TOKEN_ENGINE_TLS_MODE=disabled \
TOKEN_ENGINE_STATIC_CALLER_KEYS=supersecret=service-a \
./token-engine
```

The gRPC server starts on `:9090` and the HTTP server (health + metrics) on `:8080`.

**Connect a client:**
```go
conn, err := grpc.Dial(":9090", grpc.WithTransportCredentials(insecure.NewCredentials()))
client := tokenv1.NewTokenEngineClient(conn)

resp, err := client.IssueToken(ctx, &tokenv1.IssueTokenRequest{
    Sub:      "user-123",
    TenantId: "my-service",
})
```

`tenant_id` must equal the server's `TOKEN_ENGINE_ISSUER` — with the config above, that is `"my-service"`.

---

## Configuration

All configuration is via environment variables. The service exits fatally at startup if required fields are missing.

| Variable | Type | Default | Behavior on Invalid |
|---|---|---|---|
| `TOKEN_ENGINE_ISSUER` | string | **required** | fatal exit |
| `TOKEN_ENGINE_AUDIENCE` | string | **required** | fatal exit |
| `TOKEN_ENGINE_TLS_MODE` | `mtls` \| `disabled` | `mtls` | fatal exit |
| `TOKEN_ENGINE_STATIC_CALLER_KEYS` | `key=id,key=id` | required when TLS disabled | fatal exit |
| `TOKEN_ENGINE_TLS_CERT_FILE` | string | `` | required when `TLS_MODE=mtls` |
| `TOKEN_ENGINE_TLS_KEY_FILE` | string | `` | required when `TLS_MODE=mtls` |
| `TOKEN_ENGINE_TLS_CA_FILE` | string | `` | required when `TLS_MODE=mtls` |
| `TOKEN_ENGINE_CALLER_REGISTRY_PATH` | string | `` | optional; path to `caller-registry.yaml` |
| `TOKEN_ENGINE_GRPC_ADDR` | string | `:9090` | warning + default |
| `TOKEN_ENGINE_HTTP_ADDR` | string | `:8080` | warning + default |
| `TOKEN_ENGINE_IDEMPOTENCY_TTL` | duration | `24h` | warning + default |
| `TOKEN_ENGINE_JWKS_CACHE_MAX_AGE` | duration | `5m` | warning + default |
| `TOKEN_ENGINE_MAX_CONNECTION_AGE` | duration | `30m` | warning + default |
| `TOKEN_ENGINE_MAX_CONNECTION_AGE_GRACE` | duration | `5m` | warning + default |
| `TOKEN_ENGINE_REDIS_ADDR` | string | `localhost:6379` | warning + default |
| `TOKEN_ENGINE_REDIS_PASSWORD` | string | `` | — |
| `TOKEN_ENGINE_REDIS_DB` | int | `0` | warning + default |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | string | `` | no-op tracer (no traces) |
| `TOKEN_ENGINE_LOCK_TTL` | duration | `30s` | warning + default |
| `TOKEN_ENGINE_RECONCILIATION_INTERVAL` | duration | `5m` | warning + default |
| `TOKEN_ENGINE_RECONCILIATION_PAGE_SIZE` | int | `100` | warning + default |
| `TOKEN_ENGINE_ROTATION_WINDOW_GUARD` | duration | `1m` | warning + default |

**`TOKEN_ENGINE_STATIC_CALLER_KEYS` format:** `apikey1=caller-identity-1,apikey2=caller-identity-2`

**Duration format:** Go duration strings — `5m`, `30m`, `1h30m`, `300s`.

For per-version upgrade instructions see [MIGRATION.md](doc/MIGRATION.md).

---

## API Reference

### IssueToken

Issues a new access + refresh token pair.

| Field | Type | Description |
|---|---|---|
| `sub` | string | Subject identifier (required) |
| `tenant_id` | string | Must equal the server's `TOKEN_ENGINE_ISSUER` (required) |
| `idempotency_key` | string | Deduplication key — same key returns same tokens within TTL |
| `claims` | map<string,string> | Custom claims stamped on the access token |
| `audiences` | repeated string | Audience override; defaults to `TOKEN_ENGINE_AUDIENCE` |

Returns `TokenPair` containing `access_token`, `refresh_token`, `access_token_expires_in` (seconds), `refresh_token_expires_in` (seconds).

### RefreshToken

Rotates tokens using a valid refresh token. The old refresh token is revoked atomically.

| Field | Type | Description |
|---|---|---|
| `refresh_token` | string | Current valid refresh token (required) |
| `tenant_id` | string | Must match the tenant that issued the token (required) |
| `idempotency_key` | string | Deduplication key |
| `claims` | map<string,string> | Custom claims on the new access token |

Returns `TokenPair`.

### RevokeToken

Revokes a single refresh token immediately. Subsequent refresh attempts with this token return `NOT_FOUND`.

| Field | Type | Description |
|---|---|---|
| `refresh_token` | string | Refresh token to revoke (required) |
| `tenant_id` | string | Must match the issuing tenant (required) |

### RevokeAllForAudience

Revokes all refresh tokens scoped to a specific audience within a tenant.

| Field | Type | Description |
|---|---|---|
| `audience` | string | Audience to revoke (required) |
| `tenant_id` | string | Tenant scope (required) |

### RevokeAllUserTokens

Revokes all refresh tokens for a user across all audiences within a tenant.

| Field | Type | Description |
|---|---|---|
| `user_id` | string | User whose tokens are revoked (required) |
| `tenant_id` | string | Tenant scope (required) |

### RevokeAllForUserAndAudience

Revokes all refresh tokens for a user within a specific audience.

| Field | Type | Description |
|---|---|---|
| `user_id` | string | User whose tokens are revoked (required) |
| `audience` | string | Audience scope (required) |
| `tenant_id` | string | Tenant scope (required) |

### Error Codes

| gRPC Code | Condition |
|---|---|
| `UNAUTHENTICATED` | Missing or invalid API key; expired access token |
| `PERMISSION_DENIED` | Caller not authorized for this tenant; revoked token; invalid audience |
| `NOT_FOUND` | Refresh token not found |
| `UNAVAILABLE` | Audit store unreachable — revocation RPCs only; issuance is never gated |
| `INTERNAL` | Invalid key ID; missing kid claim; audit record failure; unexpected library error |

---

## Observability

### HTTP Endpoints

| Path | Purpose |
|---|---|
| `GET /healthz/live` | Liveness probe — returns 200 if process is alive |
| `GET /healthz/ready` | Readiness probe — returns 200 if Redis, key availability, and audit store are healthy |
| `GET /.well-known/jwks.json` | JWKS endpoint — public keys for token verification; `Cache-Control` header set via `TOKEN_ENGINE_JWKS_CACHE_MAX_AGE` |
| `GET /metrics` | Prometheus metrics (text format) |

### Metrics

Available at `GET /metrics` (Prometheus text format).

| Metric | Type | Description |
|---|---|---|
| `token_engine_grpc_requests_total` | Counter | Total gRPC requests processed |
| `token_engine_grpc_request_duration_seconds` | Histogram | gRPC request duration |
| `token_engine_idempotency_total` | Counter | Idempotency operations |
| `token_engine_active_tenants` | Gauge | Active tenant count |
| `token_engine_tenant_registry_operations_total` | Counter | Tenant registry operations |
| `token_engine_jwks_key_count` | Gauge | Non-expired signing keys at the JWKS endpoint, per tenant |

See [doc/METRICS.md](doc/METRICS.md) for full label reference and PromQL examples.

### Distributed Tracing

Set `OTEL_EXPORTER_OTLP_ENDPOINT` to enable trace export to an OTLP collector. All gRPC requests produce spans with the interceptor chain visible as child spans.

---

## Development

### Prerequisites

- Go 1.26+
- [buf](https://buf.build/docs/installation) (for proto regeneration)
- [golangci-lint](https://golangci-lint.run/usage/install/) v2+
- [ginkgo](https://onsi.github.io/ginkgo/#installing-ginkgo) v2 (`go install github.com/onsi/ginkgo/v2/ginkgo@latest`)

### Make Targets

```bash
make build          # Compile the binary
make test           # Run all tests with race detector
make coverage       # Run tests with coverage report
make lint           # go vet + golangci-lint
make proto-gen      # Regenerate from proto/token_engine.proto (requires buf)
make ci             # Full CI pipeline: lint + build + test
make benchmark      # Run RPC latency benchmarks (count=5, ~2 min; see doc/PERFORMANCE.md)
make docker-build   # Build Docker image locally (uses Podman by default)
make cd             # Build and push multi-platform image to Docker Hub (requires tag)
make clean          # Remove binary and coverage files
make examples-build # Build all four examples (go build ./... in each directory)
make examples-tidy  # Run go mod tidy in all four example directories
```

### Running Tests

```bash
make test                               # All packages
ginkgo -r --race ./internal/...        # Equivalent
ginkgo --race ./internal/observability/...  # Single package
```

Tests use [Ginkgo](https://onsi.github.io/ginkgo/) v2 with Gomega matchers and [go.uber.org/mock](https://github.com/uber-go/mock) for generated mocks.

### Local CI

Reproduce the full CI pipeline before pushing:

```bash
make ci
```

---

## Docker

Pre-built multi-platform images (`linux/amd64`, `linux/arm64`) are published automatically on every release tag:

```bash
docker pull docker.io/angeltomala/token-engine:v0.9.0
```

See [doc/DEPLOYMENT.md](doc/DEPLOYMENT.md) for full deployment configuration.

---

## Architecture

See [doc/ARCHITECTURE.md](doc/ARCHITECTURE.md) for component model, interceptor chain rationale, and roadmap.

Architecture decisions are recorded in [doc/adr/](doc/adr/).

See [doc/PERFORMANCE.md](doc/PERFORMANCE.md) for measured RPC latency baselines, mTLS overhead analysis, and regression detection guidance.

---

## Roadmap

| Version | Status | Key Additions |
|---|---|---|
| v0.1 | ✅ Complete | gRPC service, interceptor chain, static auth, in-memory idempotency, NoOp audit + reconciliation |
| v0.2 | ✅ Complete | Single hardcoded tenant, Redis key + refresh stores, `IssueToken` + `RefreshToken` live |
| v0.3 | ✅ Complete | `RevokeToken`, `RevokeAllForAudience`, `RevokeAllUserTokens`, JWKS endpoint, `SlogAuditStore`, CD pipeline |
| v0.4 | ✅ Complete | `RedisIdempotencyStore` + full idempotency interceptor, 24h TTL default, shutdown hardening, end-to-end integration test suite |
| v0.5 | ✅ Complete | `RevokeAllForUserAndAudience` RPC; `MTLSAuthenticator`; static YAML caller registry; `MultiTenantRegistry` with `Add`/`Drain`/`Remove`; mTLS gRPC server credentials (TLS 1.3 min) |
| v0.6 | ✅ Complete | Distributed lock package (`RedisLock`), `CursorReconciler` (cursor-based token reconciliation), `RefreshToken` idempotency, JWKS key count metric, Kubernetes manifests, operator + pre-upgrade runbooks, `govulncheck` + `revive`/`godot` enforced in CI |
| v0.7 | ✅ Complete | jwtauth v1.0.0 upgrade (per-tenant Redis key namespace isolation), `NoOpLocker` + `NoOpLock` test utilities, ADR-003 through ADR-006 corrections |
| v0.8 | ✅ Complete | `doc/MIGRATION.md` per-version upgrade guide, `client/` Go SDK package, `examples/grpc-client` + `examples/mtls-client`, ADR-007 through ADR-010 filed, `docs/` consolidated into `doc/` |
| v0.9 | ✅ Complete | `docker-compose.yaml` single-command local stack, `examples/custom-claims` + `examples/multi-tenant`, all four examples as independent Go modules with per-example READMEs |

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache 2.0 — see [LICENSE](LICENSE).
