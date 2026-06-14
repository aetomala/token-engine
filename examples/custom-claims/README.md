# custom-claims

Issues a token with a custom claims map (`role`, `org_id`, `tier`), then validates the
returned access token against the JWKS endpoint and prints both the registered JWT claims
and the custom claims.

## Prerequisites

A running token-engine instance with `TLS_MODE=disabled`. Quickest start:

    docker compose up   # from repo root; or: podman compose up

## Usage

    TOKEN_ENGINE_STATIC_KEY=devkey go run .

Optionally override addresses:

    TOKEN_ENGINE_ADDR=localhost:9090 TOKEN_ENGINE_HTTP_ADDR=localhost:8080 \
    TOKEN_ENGINE_STATIC_KEY=devkey go run .

## Expected Output

    access_token:  eyJ...

    === Registered claims ===
    sub: user-123
    iss: local-dev
    aud: [local-api]
    exp: 2026-06-13T16:00:00Z

    === Custom claims ===
    role:   admin
    org_id: acme-corp
    tier:   premium

## Notes

Custom claims in `IssueTokenRequest.Claims` are promoted to top-level JWT fields — they
are not nested under a `"claims"` key in the token payload.
