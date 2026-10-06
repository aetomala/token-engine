# Token Engine Pre-Upgrade Runbook

## 1. Pre-Upgrade Checklist

Complete all items before applying a new Token Engine version to a production environment.

- [ ] **Backup Redis namespace** — snapshot the Redis database, and verify the backup is restorable before proceeding. If you export keys instead of snapshotting, export every family in the [Redis key inventory](#redis-key-inventory) below except the transient lock keys. **The backup is a credential store:** it contains raw refresh tokens, the RSA private keys that sign access tokens, and idempotency records holding complete token pairs. Restrict access to it, encrypt it at rest, and delete it once the upgrade is verified — see [MIGRATION.md](MIGRATION.md#v120--v121).
- [ ] **Verify cert-manager health** — if mTLS is enabled, confirm that cert-manager is running and that the serving certificate is valid and not approaching expiry. A certificate renewal failure during a rolling update will cause gRPC connection failures.
- [ ] **Confirm single-replica deployment if pre-v0.6** — before v0.6, Token Engine must run as a single replica. Verify `kubectl get deployment token-engine -o jsonpath='{.spec.replicas}'` returns `1`. Do not scale up until v0.6 distributed lock behaviour is validated (see operator guide §11).
- [ ] **Confirm Redis connectivity** — run `redis-cli -h $REDIS_HOST ping` from within the cluster and verify `PONG` is returned.
- [ ] **Check active reconciliation passes** — inspect logs for `reconciler:` prefix entries. If a reconciliation pass is in progress, wait for it to complete or for the lock TTL to expire before upgrading.
- [ ] **Review changelog** — read the release notes for the target version, paying particular attention to breaking changes in the library API surface (see §2) and metric renames (see §4).

### Redis key inventory

`{tenant_id}` is the tenant's issuer ID. jwtauth keys put it **directly in front of** the family prefix with no separator — tenant `local-dev` writes `local-devtokens:...`. token-engine's own keys put a fixed prefix first and the tenant ID after it. Patterns below were verified by running `redis-cli --scan` against a live v1.2.x service.

| Pattern | Type | Written by | Contents |
|---|---|---|---|
| `{tenant_id}tokens:{refresh_token}` | hash | jwtauth refresh store | One hash per refresh token. **The key name is the raw refresh token.** Fields: `expiresAt`, `createdAt`, `revoked`, `metadata`, `audience`, `userID`. |
| `{tenant_id}user_tokens:{user_id}` | set | jwtauth | Per-user index. **Members are raw refresh tokens.** |
| `{tenant_id}audience_tokens:{audience}` | set | jwtauth | Per-audience index. **Members are raw refresh tokens.** |
| `{tenant_id}audience_user_tokens:{audience}:{user_id}` | set | jwtauth | Per-user, per-audience index. **Members are raw refresh tokens.** |
| `{tenant_id}token_expiry_index` | sorted set | jwtauth (v1.1.0+) | Expiry index used by the reconciler's cleanup pass. **Members are raw refresh tokens.** No TTL. |
| `{tenant_id}ks:pem:{key_id}` | string | jwtauth key store | **RSA private signing key (PEM).** |
| `{tenant_id}ks:meta:{key_id}` | string | jwtauth key store | Signing-key metadata. |
| `idempotency:{tenant_id}:{method}:{client_key}` | string | token-engine | Idempotency record in the versioned `IDR1` envelope. A completed record **holds the full token pair** for `TOKEN_ENGINE_IDEMPOTENCY_TTL` (default 24h); a pending claim lives for `TOKEN_ENGINE_LOCK_TTL`. |
| `key_rotation:last_generated:{tenant_id}` | string | token-engine | RFC 3339 timestamp of the last key rotation, read by the rotation guard. No TTL. |

Transient keys that are **not** part of a backup: `locks:reconciliation:{tenant_id}` and `locks:key_rotation:{tenant_id}`. They exist only while a reconciliation pass or key rotation holds the lock and expire after `TOKEN_ENGINE_LOCK_TTL` — see §5, step 4.

## 2. Library API Surface Audit Procedure

Token Engine depends on `github.com/aetomala/jwtauth`. Breaking changes in this library's public API will cause compilation failures and require code changes before upgrading.

Known breaking surfaces — inspect these interfaces and types for signature changes when upgrading:

- `RefreshStore` — methods `Get`, `Set`, `Delete` used in idempotency and reconciliation paths.
- `KeyStore` — methods `GetActiveKey`, `GetAllKeyInfo`, `RotateKeys` used in key management and JWKS handler.
- `KeyManager` — full interface including `RotateKeys`, `Shutdown`, `GetAllKeyInfo`.
- `tokens.Manager` — the primary token operations interface; all 20 methods are mocked and tested. Additions to this interface require regenerating `internal/testutil/mock_tokens_manager.go`.

Audit procedure:
1. Run `go get github.com/aetomala/jwtauth@<target-version>` in a local branch.
2. Run `go build ./...` and examine compilation errors — these indicate interface mismatches.
3. Run `go vet ./...` to catch type assertion failures that the compiler may not surface.
4. Update mocks with `go generate ./...` or `mockgen` if any mocked interface changed.

## 3. Error Sentinel Audit Procedure

Token Engine consumes error sentinels from the jwtauth library. If sentinels are renamed or removed in a library upgrade, sentinel comparisons will silently fail (always false), causing incorrect error handling.

Run the following to identify all sentinel usages in the service:

```bash
grep -r 'ErrToken\|ErrKey\|ErrInvalid' ./internal
```

Cross-reference each sentinel against the library source for the target version:

```bash
grep -r 'ErrToken\|ErrKey\|ErrInvalid' $(go env GOPATH)/pkg/mod/github.com/aetomala/jwtauth@<target-version>/
```

Any sentinel present in `./internal` but absent in the library source must be updated before the upgrade is applied. Pay particular attention to `ErrTokenExpired`, `ErrTokenRevoked`, `ErrTokenNotFound`, and `ErrKeyNotFound`.

### Review new sentinels against `MapLibraryError`

`observability.MapLibraryError` (`internal/observability/errors.go`) converts jwtauth sentinels into gRPC status codes. Any sentinel it does not recognize becomes `codes.Internal`, so a sentinel added in the target version silently reports as a server error until it is mapped. On every upgrade, list the exported sentinels in `pkg/tokens`, `pkg/storage`, and `pkg/keys` that were added or removed between the current and target versions:

```bash
sentinels() {
  dir=$(go env GOPATH)/pkg/mod/github.com/aetomala/jwtauth@$1
  for p in tokens storage keys; do
    grep -hoE '^\s+Err[A-Za-z]+ +=' "$dir/pkg/$p"/*.go --exclude='*_test.go' | awk -v p="$p" '{print p"."$1}'
  done | sort -u
}
go mod download github.com/aetomala/jwtauth@<current-version> github.com/aetomala/jwtauth@<target-version>
diff <(sentinels <current-version>) <(sentinels <target-version>)
```

Then list the target version's sentinels that `MapLibraryError` does not reference (lines starting with `<`):

```bash
diff <(sentinels <target-version>) <(grep -oE '(tokens|storage|keys)\.Err[A-Za-z]+' internal/observability/errors.go | sort -u)
```

For every new sentinel that an RPC path can return, either add a deliberate mapping in `MapLibraryError` with a spec, or record why it stays `codes.Internal` — as `tokens.ErrInvalidRefreshToken` does, see aetomala/jwtauth#286. Sentinels that only arise at construction time (for example `keys.ErrInvalidKeyDirectory`, `storage.ErrNilClient`) never reach an RPC and need no mapping.

## 4. Prometheus Metric Rename/Removal Check

Metric renames break dashboards and alerts silently — the old metric stops appearing rather than causing an error. Reference the library changelog for metric changes.

Known metric renames by library version:
- **v0.5.0** — several `jwtauth_*` gauge names were renamed; cross-reference the v0.5.0 changelog.
- **v0.7.0** — additional metric label changes; cross-reference the v0.7.0 changelog.

Check procedure:
1. Before upgrading, snapshot current metric names with `curl -s http://token-engine:8080/metrics | grep -E '^# TYPE'`.
2. After upgrading in a staging environment, repeat the snapshot and diff against the pre-upgrade snapshot.
3. Update Prometheus alert rules, recording rules, and Grafana dashboard queries to use new metric names before promoting to production.

## 5. Rollback Procedure

If a post-upgrade issue requires rollback:

1. **Stop the new version** — `kubectl rollout undo deployment/token-engine` or equivalent. Wait for the old ReplicaSet to become ready.
2. **Restart the previous version** — verify the previous image tag is running with `kubectl get pods -l app=token-engine -o jsonpath='{.items[*].spec.containers[0].image}'`.
3. **Ignore leftover reconciliation cursor keys** — versions before v1.1.0 persisted a `reconciliation:cursor:{tenant_id}` key; current versions do not. If a scan of an older Redis shows any, they are inert and safe to delete. No rollback step depends on them.
4. **Verify lock key expiry** — distributed lock keys (`locks:reconciliation:*`, `locks:key_rotation:*`) will expire automatically per their TTL. Do not delete them manually unless the TTL has already passed and you are certain no process holds the lock.

## 6. Post-Upgrade Validation

After a successful upgrade, verify the following within the first 15 minutes:

- [ ] **JWKS key count gauge** — `curl -s http://token-engine:8080/metrics | grep token_engine_jwks_key_count`. The gauge must be non-zero for each active tenant. A zero value indicates key manager initialisation failed.
- [ ] **gRPC health** — `grpc-health-probe -addr=token-engine:9090` must return `SERVING`.
- [ ] **HTTP readiness** — `curl -s http://token-engine:8080/healthz/ready` must return HTTP 200 with a JSON body indicating all checkers passed.
- [ ] **Reconciliation log output** — inspect logs for `reconciler:` prefix entries within one `ReconciliationInterval` of startup. Absence of log lines may indicate the reconciliation goroutine panicked silently.
- [ ] **Key rotation log output** — inspect logs for `key rotation` prefix entries within one `RotationWindowGuard` of startup. Verify rotation succeeds and `key rotation error` log lines are absent.
- [ ] **Error rate** — confirm gRPC error rates in your observability system are not elevated compared to pre-upgrade baseline.
