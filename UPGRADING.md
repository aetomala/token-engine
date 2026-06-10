# Upgrading token-engine

## v0.7.x → v0.8.0

No operator action required. v0.8.0 adds documentation, ADRs, and the `client/` Go SDK package.
There are no changes to environment variables, Redis key layouts, gRPC protocol, or runtime
behavior. Drop-in upgrade from v0.7.x.

---

## v0.6.0 → v0.7.0

### jwtauth v1.0.0 upgrade — Redis key namespace isolation

The bundled jwtauth dependency was upgraded from v0.7.2 to v1.0.0. token-engine now wires the
`Namespace` field on `RedisKeyStoreConfig` and `RedisRefreshStoreConfig` using the tenant issuer
(`TOKEN_ENGINE_ISSUER`). Redis keys for JWKS and refresh tokens are now namespaced by tenant ID.

**Existing deployments:** If upgrading from v0.6.0, existing Redis refresh token and JWKS keys
were written without a namespace prefix. After upgrading, token-engine will look for them under
the new namespaced prefix and will not find the old keys — callers will receive token-not-found
errors and will need to re-issue tokens. Plan a brief re-authentication window when rolling out
v0.7.0 to a live deployment.

No environment variable changes are required.

### No-op lock implementations (test utility)

`NoOpLocker` and `NoOpLock` are new exported types in `internal/lock`. These are test utilities
only and have no effect on production deployments.
