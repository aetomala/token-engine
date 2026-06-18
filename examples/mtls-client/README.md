# mtls-client

mTLS gRPC client demonstrating certificate-based authentication via `client.WithMTLS`.
Falls back to plaintext if certificate paths are not set.

## Prerequisites

- token-engine running with `TOKEN_ENGINE_TLS_MODE=mtls`
- Client certificate, key, and CA certificate available on the filesystem

## Usage — mTLS

    CLIENT_CERT=client.crt CLIENT_KEY=client.key CA_CERT=ca.crt go run .

`tenant_id` defaults to `local-dev`. Set `TOKEN_ENGINE_ISSUER` to match your server's issuer:

    TOKEN_ENGINE_ISSUER=my-service CLIENT_CERT=client.crt CLIENT_KEY=client.key CA_CERT=ca.crt go run .

## Usage — plaintext fallback

    go run .

If `CLIENT_CERT`, `CLIENT_KEY`, and `CA_CERT` are not set, the example falls back to
plaintext and logs a warning.

## Expected Output

    access_token:  eyJ...
    refresh_token: <token>
