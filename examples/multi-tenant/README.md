# multi-tenant

Demonstrates per-tenant token isolation using two independent token-engine instances
(tenant-alpha on `:9090`, tenant-beta on `:9091`) sharing a single Redis container.

The example issues and refreshes tokens for each tenant, then demonstrates that a refresh
token issued for one tenant is rejected when presented with a different tenant ID. Comments
in the source explain how tenant ID maps to Redis key namespace isolation.

## Prerequisites

Docker or Podman (for the bundled compose stack).

## Usage

    # Start the two-server stack from this directory:
    docker compose up -d   # or: podman compose up -d

    # Wait for both servers to become healthy, then run:
    TOKEN_ENGINE_STATIC_KEY=devkey go run .

    # Tear down:
    docker compose down

## Expected Output

    [tenant-alpha] access_token:  eyJ...
    [tenant-alpha] refresh_token: <token>

    [tenant-beta]  access_token:  eyJ...
    [tenant-beta]  refresh_token: <token>

    [tenant-alpha] refreshed access_token: eyJ...

    [tenant-beta]  refreshed access_token: eyJ...

    [cross-tenant] RefreshToken correctly rejected: rpc error: code = NotFound ...

## mTLS Reference

See `caller-registry.yaml` in this directory for a two-tenant caller authorization
reference config for `TOKEN_ENGINE_CALLER_REGISTRY_PATH`.
