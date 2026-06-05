# ADR-005: In-Memory Idempotency Store for v0.1

**Status:** Superseded — RedisIdempotencyStore delivered in v0.4.0  
**Date:** 2026-05-26

## Context

`IssueToken` and `RefreshToken` operations must support idempotent execution: a caller that sends the same request twice (due to a network retry) should receive the same response without a second token issuance. This requires storing the response for a given idempotency key for some TTL.

Durable idempotency (surviving restarts, shared across replicas) requires Redis. Redis is out of scope for v0.1.

## Decision

In-memory idempotency store with configurable TTL (`TOKEN_ENGINE_IDEMPOTENCY_TTL`, default 5 minutes). The store is a `sync.Map` with a background goroutine that sweeps expired entries.

This decision was superseded in v0.4.0 — see Outcome below.

## Rationale

- **Request deduplication works within the TTL window.** The primary use case for idempotency is network retries — a caller retries within seconds of the original request. An in-memory store with a 5-minute TTL covers this case correctly.
- **Interface seam.** The `IdempotencyStore` interface is defined. v0.4 replaces the in-memory implementation with a Redis-backed one. No other code changes.
- **Acceptable limitation.** The in-memory store is per-process. In a multi-replica deployment, a retry routed to a different replica will not find the cached response and will issue a second token pair. This is documented as a known limitation for v0.1.

## Outcome

`RedisIdempotencyStore` replaced the in-memory store in v0.4.0. It is wired in `main.go` and
provides durability and cross-replica consistency. The `TOKEN_ENGINE_IDEMPOTENCY_TTL` default is
**24 hours** — not 5 minutes as stated in the original decision.

Idempotency coverage was extended to **`RefreshToken`** in v0.6.0 alongside the `IssueToken`
coverage established in v0.1.

**Pre-handler ordering invariant (security-relevant):** The idempotency cache check for
`RefreshToken` must occur before the jwtauth library is called — not after. jwtauth immediately
revokes the old refresh token on invocation. A post-handler cache check would lose the race: the
old token is already rotated before the duplicate request can be identified. This invariant is
enforced by the position of `IdempotencyInterceptor` in the chain — it runs before the handler,
not after.

## References

- [internal/store/idempotency.go](../../internal/store/idempotency.go)
- [internal/store/idempotency_redis.go](../../internal/store/idempotency_redis.go)
- [internal/interceptor/idempotency.go](../../internal/interceptor/idempotency.go)
