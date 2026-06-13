# token-engine examples

Runnable Go programs demonstrating token-engine client usage.

All examples assume a running token-engine instance. The easiest way to start one:

```bash
docker compose up   # or: podman compose up
```

See [`docker-compose.yaml`](../docker-compose.yaml) for the default credentials
(`devkey=local-client`, issuer `local-dev`, audience `local-api`).

| Example | Auth | Demonstrates |
|---|---|---|
| `grpc-client` | Static API key | Minimal gRPC client — `IssueToken` and token pair output |
| `mtls-client` | mTLS certificate | Certificate-based auth with `WithMTLS` |
| `custom-claims` | Static API key | Custom claims issuance and JWKS-based JWT validation |
| `multi-tenant` | Static API key | Per-tenant isolation and cross-tenant token rejection |

---

## grpc-client

Plaintext gRPC client using static API key authentication. Calls `IssueToken` with a
minimal request and prints the returned token pair.

```bash
TOKEN_ENGINE_STATIC_KEY=devkey go run ./examples/grpc-client
```

## mtls-client

mTLS gRPC client using mutual TLS certificate authentication. Falls back to plaintext if
certificate paths are not set.

```bash
CLIENT_CERT=path/to/client.crt \
CLIENT_KEY=path/to/client.key \
CA_CERT=path/to/ca.crt \
go run ./examples/mtls-client
```

## custom-claims

Issues a token with a custom claims map (`role`, `org_id`, `tier`), then validates the
returned access token against the service's JWKS endpoint and prints both the registered
JWT claims and the custom claims.

```bash
TOKEN_ENGINE_STATIC_KEY=devkey go run ./examples/custom-claims
```

Custom claims in `IssueTokenRequest.Claims` are promoted to top-level JWT fields — they
are not nested under a `"claims"` key in the token payload.

## multi-tenant

Runs two token-engine instances (tenant-alpha on `:9090`, tenant-beta on `:9091`) sharing
a single Redis. Issues and refreshes tokens for each tenant, then demonstrates that a
refresh token issued for one tenant is rejected when presented with a different tenant ID.

```bash
docker compose -f examples/multi-tenant/docker-compose.yaml up -d
sleep 10
TOKEN_ENGINE_STATIC_KEY=devkey go run ./examples/multi-tenant
docker compose -f examples/multi-tenant/docker-compose.yaml down
```

See `examples/multi-tenant/caller-registry.yaml` for a two-tenant mTLS caller authorization
reference.
