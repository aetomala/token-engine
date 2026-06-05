# ADR-008: mTLS Authentication Model and CN-Based Caller Identity

**Status:** Complete — v0.5.0  
**Date:** 2026-05-30

## Context

ADR-003 chose static API key authentication for v0.1 and explicitly deferred mTLS. v0.5.0
delivered mTLS as the production authentication mode alongside the `StaticCallerRegistry` and
`MTLSAuthenticator`. The design decisions behind the mTLS model needed recording.

The relevant design space was:
- How to establish caller identity from a TLS client certificate
- What level of client authentication to enforce at the TLS layer
- What minimum TLS version to require
- How to express the trust boundary (per-caller pinning vs CA-based)
- How mTLS coexists with the static key fallback for non-TLS deployments

## Decision

`MTLSAuthenticator` establishes caller identity by extracting the Common Name (CN) from the
first client certificate in the TLS connection:

```
tlsInfo.State.PeerCertificates[0].Subject.CommonName
```

Certificate cryptographic verification occurs at the gRPC transport layer before
`Authenticate()` is called. The method only reads an already-verified identity — it does not
perform any cryptographic operations. If peer information is absent, TLS credentials are missing,
no client certificate is present, or the CN is empty, `Authenticate()` returns
`codes.Unauthenticated`.

The gRPC server is configured with:

- **`tls.RequireAndVerifyClientCert`** — the TLS handshake rejects any connection that does not
  present a valid client certificate signed by the configured CA.
- **`tls.VersionTLS13`** — TLS 1.3 is the minimum accepted protocol version.
- **CA file as trust anchor** — a single CA certificate loaded from disk at startup. Any client
  certificate signed by that CA is accepted; the CA is the trust boundary.

The extracted CN is stored in the request context via `observability.WithCallerIdentity`. The
`CallerAuthorizationInterceptor` reads it via `observability.CallerIdentityFromContext` and
passes it to `CallerRegistry.IsPermitted(ctx, callerIdentity, tenantID)`. The CN value in the
certificate must match a `callerIdentity` entry in `caller-registry.yaml`.

`TLS_MODE=disabled` wires `StaticKeyAuthenticator` instead and does not configure TLS server
credentials. The two modes are mutually exclusive at startup — `TLS_MODE` is evaluated once in
`main.go` and determines which `Authenticator` implementation is injected into the interceptor
chain.

## Rationale

**CN over SubjectAltName for caller identity.** CN is a single, unambiguous string. The
SubjectAltName (SAN) extension can carry multiple values across different types — DNS names, IP
addresses, email addresses, URIs. Selecting a canonical SAN type would require additional
configuration or heuristics. CN is the natural "name of this service" field that operators
already set when issuing internal service certificates. Mapping CN to `callerIdentity` requires
no type selection logic and preserves a 1-to-1 relationship between a certificate and the
identity string the `CallerRegistry` evaluates.

**`RequireAndVerifyClientCert` over `RequestClientCert`.** `RequestClientCert` permits
connections without a client certificate, which would silently fall through to the auth
interceptor with no TLS peer credentials and produce `codes.Unauthenticated` at runtime. In
`mtls` mode there is no legitimate use case for an unauthenticated connection — every caller
must present a certificate. Rejecting at the TLS handshake is earlier, cleaner, and produces a
clear protocol-level error rather than a runtime gRPC status code.

**TLS 1.3 minimum.** TLS 1.2 retains support for weak cipher suites and has documented
vulnerabilities (BEAST, POODLE, ROBOT) even with careful configuration. TLS 1.3 removes
negotiation of weak primitives entirely, mandates forward secrecy, and simplifies the handshake.
Since token-engine controls both endpoints in internal service-to-service deployments, requiring
TLS 1.3 costs nothing and eliminates a class of downgrade attacks.

**CA file as trust anchor over per-caller cert pinning.** Pinning individual caller certificates
would require updating the server's trusted-cert set on every new caller onboarding or cert
rotation — an operational burden that scales with the number of callers. A CA-based model
delegates trust decisions to the PKI that issues service certificates. The `caller-registry.yaml`
then controls which CN values are authorized to operate on which tenants — the two layers are
independent and composable.

**Mutually exclusive modes.** mTLS and static key auth serve the same role: establishing caller
identity before the `CallerAuthorizationInterceptor` runs. Running both simultaneously would
create ambiguity about which identity takes precedence. `TLS_MODE` is a deployment-time
decision; mixed-mode authentication is not supported.

## Alternatives Considered

**SAN-based identity** — rejected. SAN can contain DNS names, IP addresses, and email addresses
in a single certificate. Selecting a canonical type requires additional configuration or
precedence rules. CN is simpler and unambiguous for service-to-service identity in an internal
PKI.

**JWT bearer token in addition to mTLS** — rejected. Adding a second credential layer on top of
an already-authenticated TLS connection provides no meaningful security benefit in a CA-controlled
environment. It increases operational complexity (token issuance, refresh, revocation) without
reducing the trust surface — the CA already controls which callers can connect.

## Consequences

**Positive:**
- Caller identity is cryptographically verified at the TLS handshake before any application code
  runs — no shared secrets are required in the application layer.
- Certificate rotation is independent of the service: operators issue new certs and the old ones
  expire; no service restart is required for the caller's side.
- TLS 1.3 prevents protocol downgrade attacks at the transport layer.
- The `Authenticator` interface abstracts the identity extraction — swapping to a different model
  requires no changes to the interceptor chain.

**Negative:**
- Operators must maintain a PKI: a CA, cert issuance per caller service, and cert rotation
  procedures.
- The CN in each caller's certificate must be coordinated with the `callerIdentity` entries in
  `caller-registry.yaml`. A mismatch produces `codes.PermissionDenied` with no obvious
  diagnostic.
- `TLS_MODE=disabled` deployments cannot switch to mTLS without a service restart and
  corresponding infrastructure changes (CA setup, cert distribution).

## References

- [internal/interceptor/auth.go](../../internal/interceptor/auth.go) — `MTLSAuthenticator` and `Authenticate()` implementation
- [cmd/token-engine/main.go](../../cmd/token-engine/main.go) — TLS credential setup and `TLS_MODE`-based authenticator selection
- [internal/interceptor/caller_auth.go](../../internal/interceptor/caller_auth.go) — `CallerAuthorizationInterceptor` consuming the CN identity
- [deploy/caller-registry.yaml](../../deploy/caller-registry.yaml) — example caller authorization config (CN → tenant mapping)
- [doc/adr/ADR-003-static-caller-keys-v01.md](ADR-003-static-caller-keys-v01.md) — superseded decision; forward-references this ADR
- [doc/adr/ADR-006-interceptor-chain-order.md](ADR-006-interceptor-chain-order.md) — interceptor position of Auth and CallerAuthz steps
