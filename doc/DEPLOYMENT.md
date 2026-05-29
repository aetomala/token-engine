# token-engine — Deployment Reference

## Pre-flight Checklist

Before starting the service:

- [ ] `TOKEN_ENGINE_ISSUER` set — non-empty string
- [ ] `TOKEN_ENGINE_AUDIENCE` set — non-empty string
- [ ] `TOKEN_ENGINE_TLS_MODE` set — `mtls` or `disabled`
- [ ] If `TLS_MODE=disabled`: `TOKEN_ENGINE_STATIC_CALLER_KEYS` set with at least one entry
- [ ] If `TLS_MODE=mtls`: TLS certificates provisioned and accessible to the process
- [ ] jwtauth `KeyManager` key store (disk or Redis) accessible and writable
- [ ] Redis accessible if using any Redis-backed components (v0.2+)

---

## TLS Configuration

### Mutual TLS (`TLS_MODE=mtls`)

The gRPC server requires client certificates. Configure TLS at the process level via your deployment platform (Kubernetes service mesh, Envoy sidecar, or direct certificate injection).

The service does not load certificates directly in v0.1 — mTLS is enforced at the transport layer by the infrastructure.

### No TLS (`TLS_MODE=disabled`)

For development or internal networks with transport security handled at the infrastructure layer (VPC, service mesh). Requires `TOKEN_ENGINE_STATIC_CALLER_KEYS` for caller authentication.

**Do not use `disabled` mode with public-facing traffic.**

---

## Health Checks

The HTTP server (default `:8080`) exposes:

| Endpoint | Method | Success | Failure |
|---|---|---|---|
| `/healthz/live` | GET | 200 OK | — (always 200 if process is running) |
| `/healthz/ready` | GET | 200 OK | 503 Service Unavailable |

### Kubernetes Probe Configuration

```yaml
livenessProbe:
  httpGet:
    path: /healthz/live
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
  failureThreshold: 3

readinessProbe:
  httpGet:
    path: /healthz/ready
    port: 8080
  initialDelaySeconds: 5
  periodSeconds: 10
  failureThreshold: 3
```

---

## Prometheus Metrics

The Prometheus metrics endpoint is available at `GET http://<host>:8080/metrics`.

### Prometheus Scrape Config

```yaml
scrape_configs:
  - job_name: token-engine
    static_configs:
      - targets: ["<host>:8080"]
    metrics_path: /metrics
```

For alerting rules and PromQL examples, see [METRICS.md](METRICS.md).

---

## OpenTelemetry Tracing

Set `OTEL_EXPORTER_OTLP_ENDPOINT` to export traces to an OTLP-compatible collector (Jaeger, Tempo, OTLP collector, etc.).

```bash
OTEL_EXPORTER_OTLP_ENDPOINT=http://jaeger:4317 ./token-engine
```

When the endpoint is empty (default), a no-op `TracerProvider` is used — no traces are emitted and no connection is attempted.

All gRPC requests produce a server span. The interceptor chain produces child spans for:
- OTel interceptor (root span)
- Correlation ID attachment
- Authentication check
- Caller authorization check
- Idempotency lookup

---

## Graceful Shutdown

The service listens for `SIGINT` and `SIGTERM`. On receipt, shutdown proceeds in this exact order:

1. **gRPC drain** — `GracefulStop()` with a 10-second sub-deadline; hard stop if exceeded
2. **OTel flush** — buffered spans are flushed to the OTLP collector before the process exits
3. **Key manager stop** — background key-rotation goroutine is stopped
4. **HTTP shutdown** — stops accepting new connections; health and metrics stay available through the gRPC drain

Total budget: 30 seconds. Allow sufficient time in your pod termination grace period. Recommended minimum: `MaxConnectionAge + MaxConnectionAgeGrace` (default: 35 minutes total).

```yaml
terminationGracePeriodSeconds: 60
```

---

## gRPC Connection Lifetime

Long-lived gRPC connections are cycled to prevent resource accumulation:

| Variable | Default | Purpose |
|---|---|---|
| `TOKEN_ENGINE_MAX_CONNECTION_AGE` | `30m` | Maximum age before graceful close signal |
| `TOKEN_ENGINE_MAX_CONNECTION_AGE_GRACE` | `5m` | Grace period for in-flight calls after close signal |

Clients should implement retry with exponential backoff and respect `GOAWAY` frames. gRPC client libraries handle this automatically.

---

## Redis

Redis is required from v0.2+. It backs the tenant key and refresh-token stores (v0.2), the Redis idempotency store (v0.3), and is checked by the readiness probe. The service blocks at startup until Redis is reachable (retry window: 30 seconds).

Redis hardening guidelines:
- Use Redis ACL to restrict commands to: `GET`, `SET`, `DEL`, `EXPIRE`, `KEYS`, `SCAN`
- Isolate the Redis instance on a private network — no public exposure
- Enable TLS on the Redis connection when using Redis 6+

---

## Environment Reference

See the [Configuration section of README.md](../README.md#configuration) for the full variable table with types, defaults, and validation behavior.

---

## Rate Limiting

Rate limiting is not implemented in token-engine. Apply rate limiting at the API gateway or ingress layer before requests reach the gRPC port. Recommended tools:

- Kong with Rate Limiting plugin
- AWS API Gateway with usage plans
- Kubernetes Ingress with NGINX `limit_req`
- Envoy with local rate limiting filter

See [ADR-001](adr/ADR-001-grpc-first-transport.md) for the rationale.
