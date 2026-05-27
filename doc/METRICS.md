# token-engine — Metrics Reference

All metrics are exported in Prometheus text format at `GET http://<host>:8080/metrics`.

---

## Metric Inventory

### token_engine_grpc_requests_total

| Field | Value |
|---|---|
| Type | Counter |
| Description | Total number of gRPC requests processed by the service |
| Labels | _(none in v0.1)_ |

**PromQL examples:**

```promql
# Request rate over 5 minutes
rate(token_engine_grpc_requests_total[5m])
```

---

### token_engine_grpc_request_duration_seconds

| Field | Value |
|---|---|
| Type | Histogram |
| Description | Duration of gRPC requests in seconds |
| Buckets | Default Prometheus histogram buckets |
| Labels | _(none in v0.1)_ |

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
| Description | Total number of idempotency store operations (hits and misses) |
| Labels | _(none in v0.1)_ |

**PromQL examples:**

```promql
# Idempotency operation rate
rate(token_engine_idempotency_total[5m])
```

---

### token_engine_active_tenants

| Field | Value |
|---|---|
| Type | Gauge |
| Description | Number of active tenants registered in the tenant registry |
| Labels | _(none in v0.1)_ |

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
| Labels | _(none in v0.1)_ |

**PromQL examples:**

```promql
# Registry operation rate
rate(token_engine_tenant_registry_operations_total[5m])
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
```

---

## v0.2 Planned Additions

The following metrics will be added in v0.2 when Redis-backed components are enabled:

| Metric | Type | Description |
|---|---|---|
| `token_engine_audit_operations_total` | Counter | Audit log write operations |
| `token_engine_reconciliation_runs_total` | Counter | Token reconciliation run count |
| `token_engine_redis_operations_total` | Counter | Redis client operations (by command) |
| `token_engine_redis_operation_duration_seconds` | Histogram | Redis operation latency |
