# grpc-client

Minimal plaintext gRPC client demonstrating `IssueToken` using static API key authentication.

## Prerequisites

A running token-engine instance with `TLS_MODE=disabled`. Quickest start:

    docker compose up   # from repo root; or: podman compose up

## Usage

    TOKEN_ENGINE_STATIC_KEY=devkey go run .

Optionally override the server address:

    TOKEN_ENGINE_ADDR=localhost:9090 TOKEN_ENGINE_STATIC_KEY=devkey go run .

## Expected Output

    access_token:  eyJ...
    refresh_token: <token>
