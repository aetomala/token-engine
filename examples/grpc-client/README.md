# grpc-client

Minimal plaintext gRPC client demonstrating `IssueToken` using static API key authentication.

## Prerequisites

A running token-engine instance with `TLS_MODE=disabled`. Quickest start:

    docker compose up   # from repo root; or: podman compose up

## Usage

    TOKEN_ENGINE_STATIC_KEY=devkey go run .

`tenant_id` defaults to `local-dev` — the issuer configured in the root `docker-compose.yaml`.
To target a server with a different issuer, pass `TOKEN_ENGINE_ISSUER` to match:

    TOKEN_ENGINE_ISSUER=my-service TOKEN_ENGINE_STATIC_KEY=my-key go run .

Optionally override the server address:

    TOKEN_ENGINE_ADDR=localhost:9090 TOKEN_ENGINE_STATIC_KEY=devkey go run .

## Expected Output

    access_token:  eyJ...
    refresh_token: <token>
