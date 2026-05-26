# ADR-005: In-Memory Idempotency Store for v0.1

**Status:** Accepted  
**Date:** 2026-05-26

## Context

`IssueToken` and `RefreshToken` operations must support idempotent execution: a caller that sends the same request twice (due to a network retry) should receive the same response without a second token issuance. This requires storing the response for a given idempotency key for some TTL.

Durable idempotency (surviving restarts, shared across replicas) requires Redis. Redis is out of scope for v0.1.

## Decision

In-memory idempotency store with configurable TTL (`TOKEN_ENGINE_IDEMPOTENCY_TTL`, default 5 minutes). The store is a `sync.Map` with a background goroutine that sweeps expired entries.

## Rationale

- **Request deduplication works within the TTL window.** The primary use case for idempotency is network retries — a caller retries within seconds of the original request. An in-memory store with a 5-minute TTL covers this case correctly.
- **Interface seam.** The `IdempotencyStore` interface is defined. v0.2 replaces the in-memory implementation with a Redis-backed one. No other code changes.
- **Acceptable limitation.** The in-memory store is per-process. In a multi-replica deployment, a retry routed to a different replica will not find the cached response and will issue a second token pair. This is documented as a known limitation.

## Limitations

- **Not durable.** Idempotency state is lost on process restart. A retry arriving after a restart will issue a new token pair.
- **Not shared across replicas.** In a multi-replica deployment, sticky sessions or a Redis-backed store (v0.2) are required for cross-replica idempotency.
- **Memory-bound.** High-volume deployments with large TTLs accumulate idempotency entries in memory until the sweep goroutine clears them.

## Target Version

v0.2 replaces the in-memory store with a Redis-backed implementation that provides durability and cross-replica consistency.

## References

- [internal/store/idempotency.go](../../internal/store/idempotency.go)
- [internal/interceptor/idempotency.go](../../internal/interceptor/idempotency.go)
