# ADR-001: gRPC-First Transport

**Status:** Accepted  
**Date:** 2026-05-26

## Context

token-engine exposes jwtauth's token operations over a network API. The choice of transport protocol affects client ergonomics, schema enforcement, observability integration, and operational complexity.

Candidates evaluated: REST/HTTP+JSON, gRPC+Protobuf, GraphQL.

## Decision

gRPC with Protocol Buffers.

## Rationale

- **Strict schema.** Protobuf defines the API contract with field types, required/optional semantics, and generated client stubs. No hand-rolled serialization, no schema drift between caller and service.
- **Native OTel integration.** `otelgrpc` provides first-class trace context propagation (W3C Trace Context) and server span creation for every RPC with zero application code.
- **Deadline propagation.** gRPC deadlines propagate automatically across service boundaries. Callers set a deadline; the server respects it via context cancellation.
- **Interceptor chain.** gRPC's `ChainUnaryInterceptor` provides a clean composition model for cross-cutting concerns (auth, logging, idempotency) without middleware framework coupling.
- **Streaming capability.** Not used in v0.1, but available without protocol change if future operations require server-side streaming (e.g., event subscriptions).

## Consequences

**Positive:**
- Generated client stubs in any gRPC-supported language.
- Trace context propagated without custom headers.
- Contract defined in `proto/token_engine.proto`; changes are versioned and explicit.

**Negative:**
- Requires `buf` toolchain for proto management and code generation.
- Browser clients require gRPC-Web proxy or Envoy transcoding.
- Proto toolchain adds a build step (`buf generate`) when the service definition changes.

## Alternatives Considered

**REST/HTTP+JSON:** Simpler client setup (curl, any HTTP client), but no generated stubs, no built-in deadline propagation, manual trace context injection required.

**GraphQL:** Flexible queries, but adds significant operational complexity (schema registry, resolver design) for a service with a small, fixed RPC surface.

## References

- [proto/token_engine.proto](../../proto/token_engine.proto)
- [buf.yaml](../../buf.yaml)
- [DEPLOYMENT.md — Rate Limiting](../DEPLOYMENT.md#rate-limiting)
