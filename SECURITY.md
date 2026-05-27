# Security Policy

## Supported Versions

| Version | Support |
|---|---|
| v0.1.x | Active development — security fixes applied |
| < v0.1.0 | None |

## Reporting a Vulnerability

Report security vulnerabilities via [GitHub Security Advisories](https://github.com/aetomala/token-engine/security/advisories/new) — do not open a public issue.

**Acknowledgement:** Within 72 hours of receipt.  
**Patch timeline:** 7 days for critical issues; 30 days for others.

## Scope

**In scope:**
- gRPC authentication and caller authorization logic (`internal/interceptor/`)
- API key handling and identity mapping
- Token issuance, refresh, and revocation via jwtauth integration
- Error mapping and gRPC status code assignment
- Idempotency key handling

**Out of scope (operator responsibility):**
- TLS certificate management and rotation
- Redis ACL and network isolation
- API key storage and distribution
- Rate limiting (apply at API gateway or ingress layer — see [ADR-001](doc/adr/ADR-001-grpc-first-transport.md))
- Kubernetes network policy and pod security

## Security Architecture

Key decisions relevant to security:

- **ADR-003** — Static caller keys: API keys are validated in-memory on every request; there is no session state that can be hijacked.
- **ADR-006** — Interceptor chain order: authentication runs before caller authorization, idempotency, and validation — unauthorized requests never reach business logic.

Token security guarantees (signing, key rotation, replay prevention) are inherited from jwtauth — see the [jwtauth security policy](https://github.com/aetomala/jwtauth/blob/main/SECURITY.md).
