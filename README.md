# token-engine

[![CI](https://github.com/aetomala/token-engine/actions/workflows/ci.yml/badge.svg)](https://github.com/aetomala/token-engine/actions/workflows/ci.yml)
[![Go 1.22+](https://img.shields.io/badge/go-1.22+-blue.svg)](https://go.dev/dl/)
[![License: Apache 2.0](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

A production-grade gRPC service that wraps [jwtauth](https://github.com/aetomala/jwtauth) v0.7.0 and exposes stateful JWT token management as a network API — multi-tenant, observable, and horizontally scalable.

---

## What It Provides

| RPC | Description |
|---|---|
| `IssueToken` | Issue a new access + refresh token pair for a subject |
| `RefreshToken` | Rotate tokens using a valid refresh token |
| `RevokeToken` | Revoke a single refresh token immediately |
| `RevokeAllForAudience` | Revoke all tokens scoped to an audience |
| `RevokeAllUserTokens` | Revoke all tokens for a user across all audiences |

**Interceptor chain (applied to every RPC):**
OpenTelemetry tracing → Correlation ID → API key authentication → Caller authorization → Idempotency → Request validation

**Observability:** Prometheus metrics at `/metrics`, OpenTelemetry traces via OTLP, structured slog logging with correlation IDs, health probes at `/healthz/live` and `/healthz/ready`.

---

## Quick Start

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
    TenantId: "tenant-abc",
})
```

---

## Configuration

All configuration is via environment variables. The service exits fatally at startup if required fields are missing.

| Variable | Type | Default | Behavior on Invalid |
|---|---|---|---|
| `TOKEN_ENGINE_ISSUER` | string | **required** | fatal exit |
| `TOKEN_ENGINE_AUDIENCE` | string | **required** | fatal exit |
| `TOKEN_ENGINE_TLS_MODE` | `mtls` \| `disabled` | `mtls` | fatal exit |
| `TOKEN_ENGINE_STATIC_CALLER_KEYS` | `key=id,key=id` | required when TLS disabled | fatal exit |
| `TOKEN_ENGINE_GRPC_ADDR` | string | `:9090` | warning + default |
| `TOKEN_ENGINE_HTTP_ADDR` | string | `:8080` | warning + default |
| `TOKEN_ENGINE_IDEMPOTENCY_TTL` | duration | `5m` | warning + default |
| `TOKEN_ENGINE_MAX_CONNECTION_AGE` | duration | `30m` | warning + default |
| `TOKEN_ENGINE_MAX_CONNECTION_AGE_GRACE` | duration | `5m` | warning + default |
| `TOKEN_ENGINE_REDIS_ADDR` | string | `localhost:6379` | warning + default |
| `TOKEN_ENGINE_REDIS_PASSWORD` | string | `` | — |
| `TOKEN_ENGINE_REDIS_DB` | int | `0` | warning + default |
| `OTEL_EXPORTER_OTLP_ENDPOINT` | string | `` | no-op tracer (no traces) |

**`TOKEN_ENGINE_STATIC_CALLER_KEYS` format:** `apikey1=caller-identity-1,apikey2=caller-identity-2`

**Duration format:** Go duration strings — `5m`, `30m`, `1h30m`, `300s`.

---

## API Reference

### IssueToken

Issues a new access + refresh token pair.

| Field | Type | Description |
|---|---|---|
| `sub` | string | Subject identifier (required) |
| `tenant_id` | string | Tenant scoping for multi-tenancy (required) |
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

### Error Codes

| gRPC Code | Condition |
|---|---|
| `UNAUTHENTICATED` | Missing or invalid API key; expired access token |
| `PERMISSION_DENIED` | Caller not authorized for this tenant; revoked token; invalid audience |
| `NOT_FOUND` | Refresh token not found |
| `INTERNAL` | Invalid key ID; missing kid claim; unexpected library error |

---

## Observability

### Health Endpoints (HTTP)

| Path | Purpose |
|---|---|
| `GET /healthz/live` | Liveness probe — returns 200 if process is alive |
| `GET /healthz/ready` | Readiness probe — returns 200 if service is ready |

### Metrics (HTTP)

Available at `GET /metrics` (Prometheus text format).

| Metric | Type | Description |
|---|---|---|
| `token_engine_grpc_requests_total` | Counter | Total gRPC requests processed |
| `token_engine_grpc_request_duration_seconds` | Histogram | gRPC request duration |
| `token_engine_idempotency_total` | Counter | Idempotency operations |
| `token_engine_active_tenants` | Gauge | Active tenant count |
| `token_engine_tenant_registry_operations_total` | Counter | Tenant registry operations |

See [doc/METRICS.md](doc/METRICS.md) for full label reference and PromQL examples.

### Distributed Tracing

Set `OTEL_EXPORTER_OTLP_ENDPOINT` to enable trace export to an OTLP collector. All gRPC requests produce spans with the interceptor chain visible as child spans.

---

## Development

### Prerequisites

- Go 1.22+
- [buf](https://buf.build/docs/installation) (for proto regeneration)
- [golangci-lint](https://golangci-lint.run/usage/install/) v2+
- [govulncheck](https://pkg.go.dev/golang.org/x/vuln/cmd/govulncheck) (`go install golang.org/x/vuln/cmd/govulncheck@latest`)
- [ginkgo](https://onsi.github.io/ginkgo/#installing-ginkgo) v2 (`go install github.com/onsi/ginkgo/v2/ginkgo@v2.27.3`)

### Make Targets

```bash
make build       # Compile the binary
make test        # Run all tests with race detector
make coverage    # Run tests with coverage report
make lint        # go vet + golangci-lint
make proto-gen   # Regenerate from proto/token_engine.proto (requires buf)
make ci          # Full CI pipeline: lint + build + test
make clean       # Remove binary and coverage files
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
./run-ci-locally.sh
```

---

## Architecture

See [doc/ARCHITECTURE.md](doc/ARCHITECTURE.md) for component model, interceptor chain rationale, and roadmap.

Architecture decisions are recorded in [doc/adr/](doc/adr/).

---

## Roadmap

| Version | Status | Key Additions |
|---|---|---|
| v0.1 | ✅ Complete | gRPC service, interceptor chain, static auth, in-memory idempotency, NoOp audit + reconciliation |
| v0.2 | Planned | Single hardcoded tenant, Redis key + refresh stores, `IssueToken` + `RefreshToken` live |
| v0.3 | Planned | `RevokeToken`, `RevokeAllForAudience`, `RevokeAllUserTokens`, JWKS endpoint, Redis audit store |
| v0.4 | Planned | Redis idempotency store, idempotency interceptor wired, `RefreshToken` retry safety end-to-end |
| v0.5 | Planned | mTLS authenticator, static YAML caller registry, full multi-tenant `TenantRegistry` |
| v1.0 | Planned | Distributed locks, cursor-based reconciler, Kubernetes manifests, operator runbook |

---

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md).

## License

Apache 2.0 — see [LICENSE](LICENSE).
