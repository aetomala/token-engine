# ADR-006: Interceptor Chain Order

**Status:** Accepted  
**Date:** 2026-05-26

## Context

Six cross-cutting concerns must be applied to every gRPC request: OTel trace span creation (via `otelgrpc.UnaryServerInterceptor`), correlation ID injection and request metrics, API key authentication, caller authorization, request idempotency, and request validation. gRPC's `ChainUnaryInterceptor` applies them in order. The ordering has correctness, performance, and observability implications.

## Decision

The chain runs in this order:

```
1. otelgrpc.UnaryServerInterceptor — trace span creation
2. Correlation ID + request count and duration metrics
3. Authentication
4. Caller authorization
5. Idempotency (IssueToken and RefreshToken)
6. Validation
```

## Rationale

**OTel first:** The trace span must be started before any other interceptor so that all downstream work — including authentication failures, log lines, and the handler — is attributed to the same trace. Span creation inside `otelgrpc.UnaryServerInterceptor` attaches the trace context to the `context.Context` that all subsequent interceptors receive.

**Correlation ID second:** The correlation interceptor injects a UUID correlation ID into the context and records request count and duration metrics via Prometheus. The correlation ID should be in the context before authentication so that auth failure log lines include the correlation ID. This makes it possible to correlate a rejected request across log aggregators without a trace ID.

**Authentication before authorization:** Identity must be established before permissions can be checked. An unauthenticated request cannot be authorized.

**Authorization before idempotency:** Checking the idempotency store for an unauthorized caller is wasted work and could leak information about whether a previous request with that key was successful. Authorization gates access to the idempotency store.

**Idempotency before validation:** A duplicate `IssueToken` or `RefreshToken` request (same idempotency key within TTL) should return the cached response immediately, before field validation runs. Validation is not needed for a request that will return an already-computed result.

**Validation last:** Only requests that have passed identity checks and idempotency deduplication reach the validation step. This minimizes unnecessary parsing work and keeps the validation interceptor focused purely on business logic preconditions.

## Consequences

**Positive:**
- Unauthorized requests are rejected before any store access or business logic.
- All log lines for a request share a correlation ID, including auth failure lines.
- All request work appears under a single trace span.
- Duplicate requests return immediately without hitting the handler.

**Negative:**
- OTel interceptor runs even for requests that fail authentication — a small overhead. This is intentional: failed auth requests are worth tracing for security monitoring.

## References

- [internal/interceptor/](../../internal/interceptor/)
- [cmd/token-engine/main.go](../../cmd/token-engine/main.go) — chain assembly
- [ARCHITECTURE.md — Interceptor Chain](../ARCHITECTURE.md#interceptor-chain)
