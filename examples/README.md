# token-engine examples

Runnable Go programs demonstrating token-engine client usage.

All examples assume a running token-engine instance. The easiest way to start one:

```bash
docker compose up   # or: podman compose up
```

See [`docker-compose.yaml`](../docker-compose.yaml) for the default credentials
(`devkey=local-client`, issuer `local-dev`, audience `local-api`).

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
