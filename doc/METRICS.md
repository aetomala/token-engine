# token-engine — Metrics Reference

All metrics are exported in Prometheus text format at `GET http://<host>:8080/metrics`.

---

## Metric Inventory

### token_engine_grpc_requests_total

| Field | Value |
|---|---|
| Type | Counter |
| Description | Total number of gRPC requests processed by the service |
| Labels | `rpc_method` (full gRPC method path), `status` (gRPC status code string) |

**PromQL examples:**

```promql
# Request rate over 5 minutes
rate(token_engine_grpc_requests_total[5m])

# Error rate by method
rate(token_engine_grpc_requests_total{status!="OK"}[5m])
```

---

### token_engine_grpc_request_duration_seconds

| Field | Value |
|---|---|
| Type | Histogram |
| Description | Duration of gRPC requests in seconds |
| Buckets | Default Prometheus histogram buckets |
| Labels | `rpc_method` (full gRPC method path) |

**PromQL examples:**

```promql
# 99th percentile latency over 5 minutes
histogram_quantile(0.99, rate(token_engine_grpc_request_duration_seconds_bucket[5m]))

# 50th percentile latency
histogram_quantile(0.50, rate(token_engine_grpc_request_duration_seconds_bucket[5m]))
```

---

### token_engine_idempotency_total

| Field | Value |
|---|---|
| Type | Counter |
| Description | Total number of idempotency store lookups, split by outcome |
| Labels | `result` (`hit` or `miss`), `rpc_method` (full gRPC method path) |

**PromQL examples:**

```promql
# Idempotency hit rate
rate(token_engine_idempotency_total{result="hit"}[5m])

# Hit ratio
rate(token_engine_idempotency_total{result="hit"}[5m])
  /
rate(token_engine_idempotency_total[5m])
```

---

### token_engine_active_tenants

| Field | Value |
|---|---|
| Type | Gauge |
| Description | Number of active tenants registered in the tenant registry |
| Labels | _(none)_ |

**PromQL examples:**

```promql
# Current active tenant count
token_engine_active_tenants
```

---

### token_engine_tenant_registry_operations_total

| Field | Value |
|---|---|
| Type | Counter |
| Description | Total number of tenant registry operations |
| Labels | `operation` (`add`, `drain`, `remove`) |

**PromQL examples:**

```promql
# Registry operation rate
rate(token_engine_tenant_registry_operations_total[5m])
```

---

### token_engine_jwks_key_count

| Field | Value |
|---|---|
| Type | Gauge |
| Description | Number of non-expired public keys currently available in the JWKS endpoint |
| Labels | `tenant_id` |
| Emitted | On every JWKS request, before fetching the JWKS — only when `GetAllKeyInfo` succeeds; silently skipped on error |

**PromQL examples:**

```promql
# Current key count for a specific tenant
token_engine_jwks_key_count{tenant_id="tenant-a"}

# Alert: no signing keys available for any tenant
token_engine_jwks_key_count == 0
```

---

## Alerting Examples

```yaml
groups:
  - name: token-engine
    rules:
      - alert: TokenEngineHighLatency
        expr: histogram_quantile(0.99, rate(token_engine_grpc_request_duration_seconds_bucket[5m])) > 0.5
        for: 2m
        labels:
          severity: warning
        annotations:
          summary: "token-engine p99 latency above 500ms"

      - alert: TokenEngineDown
        expr: absent(token_engine_grpc_requests_total)
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "token-engine is not emitting metrics"

      - alert: TokenEngineNoSigningKeys
        expr: token_engine_jwks_key_count == 0
        for: 1m
        labels:
          severity: critical
        annotations:
          summary: "token-engine has no signing keys for tenant {{ $labels.tenant_id }}"
```

---

## Related Configuration

The following environment variables influence metric behavior and should be considered when
interpreting the metrics above.

| Variable | Default | Affected Metric | Notes |
|---|---|---|---|
| `TOKEN_ENGINE_LOCK_TTL` | `30s` | _(no dedicated metric)_ | TTL for distributed lock keys; controls how long key rotation and reconciliation are mutually exclusive across replicas |
| `TOKEN_ENGINE_RECONCILIATION_INTERVAL` | `5m` | _(no dedicated metric)_ | Time between reconciliation passes; affects how quickly stale refresh tokens are purged |
| `TOKEN_ENGINE_RECONCILIATION_PAGE_SIZE` | `100` | _(no dedicated metric)_ | Tokens fetched per page during reconciliation; large values increase Redis scan duration per pass |
| `TOKEN_ENGINE_ROTATION_WINDOW_GUARD` | `1m` | `token_engine_jwks_key_count` | Minimum time between key rotations; a count of 1 near a rotation boundary is expected behavior, not a fault |

