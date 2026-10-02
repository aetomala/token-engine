# idempotency

Demonstrates retry-safe `IssueToken` calls using the `x-idempotency-key` metadata header: a retry
with the same key and the same content returns the cached response; the same key reused with
different content is rejected with `codes.FailedPrecondition`; and the deprecated
`idempotency_key` request field still works as a fallback when the header is absent.

## Prerequisites

A running token-engine instance with `TLS_MODE=disabled`. Quickest start:

    docker compose up   # from repo root; or: podman compose up

## Usage

    TOKEN_ENGINE_STATIC_KEY=devkey go run .

Optionally override the address and tenant:

    TOKEN_ENGINE_ADDR=localhost:9090 TOKEN_ENGINE_ISSUER=local-dev \
    TOKEN_ENGINE_STATIC_KEY=devkey go run .

`TOKEN_ENGINE_ISSUER` is the `tenant_id` sent on each request and must match the server's
configured issuer (`local-dev` in the compose stack).

## Expected Output

    === 1. Same key, same content — retry returns the cached response ===
    first call:  access_token=eyJhbG...
    second call: access_token=eyJhbG... (identical: true)

    === 2. Same key, different content — rejected, not silently mismatched ===
    first call:  sub=user-a — succeeds
    second call: sub=user-b, same key — FailedPrecondition

    === 3. Deprecated idempotency_key field — still works as a fallback ===
    identical: true (prefer the x-idempotency-key header for new integrations)

## Notes

- Use the `x-idempotency-key` header for new integrations. The `idempotency_key` request field
  is deprecated and kept only as a fallback — see
  [ADR-015](../../doc/adr/ADR-015-idempotency-key-field-deprecation.md).
- A key is bound to the content of the request that first used it, so reusing a key with
  different content fails rather than returning a mismatched response — see
  [ADR-013](../../doc/adr/ADR-013-idempotency-request-fingerprint.md).
- A concurrent duplicate that arrives while the first request with the same key is still in
  flight receives `codes.Aborted`; retry it after a short delay.
