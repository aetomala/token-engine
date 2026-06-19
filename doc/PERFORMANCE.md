# Performance

## Overview

This document covers RPC-level performance baselines for token-engine. All measurements use
a loopback gRPC connection backed by an in-process miniredis instance — real Redis network
RTT is excluded. See [What These Numbers Don't Include](#what-these-numbers-dont-include).

The measurements answer two questions:

1. **What is the steady-state latency of each RPC?** (Planning capacity, setting SLOs)
2. **How much overhead does the gRPC service layer add on top of raw jwtauth?** (Understanding where time goes)

---

## Reference Machine

| Field | Value |
|---|---|
| **Date** | 2026-06-18 (v0.9.0+) |
| Hardware | Apple M4 Max |
| OS | macOS 25.5.0 (darwin/arm64) |
| Go | 1.26.4 |
| `GOMAXPROCS` | 16 |
| Redis (storage layer) | in-process miniredis (no network) |

---

## Reproduction

```bash
make benchmark
```

Which expands to:
```bash
go test -bench=. -benchmem -run=^$ -count=5 -timeout=30m ./integration/bench/
```

Use `benchstat` to compare runs:
```bash
make benchmark | tee bench-new.txt
benchstat bench-baseline.txt bench-new.txt
```

> **Note:** Refresh token benchmarks (`BenchmarkRefreshToken`, `BenchmarkRefreshTokenMTLS`) pre-issue a pool of tokens outside the timed section. jwtauth rotates the refresh token on each `RefreshToken` RPC, so each iteration requires a fresh token. The timed section measures only the `RefreshToken` RPC itself.

> **Note on bulk revocation benchmarks:** `BenchmarkRevokeAllFor*` measures the baseline RPC cost with a near-empty store. These RPCs fan out to Redis SCAN operations — cost scales linearly with total token count in the store. See [vs. Raw jwtauth Baseline](#vs-raw-jwtauth-tokenmanager-baseline) for storage-layer scaling data.

---

## Results: Single-Client RPC Latency (no-TLS)

Averages over 5 runs (`-count=5`). Each run executes ~1,100–1,500 iterations.

| RPC | ns/op | µs/op | B/op | allocs/op |
|---|---|---|---|---|
| `IssueToken` | 832,298 | **832** | 33,407 | 484 |
| `RefreshToken` | 912,693 | **913** | 32,762 | 466 |
| `RevokeToken` (issue + revoke) | 1,063,001 | **1,063** | 57,594 | 861 |
| `RevokeAllForAudience` (empty store) | 82,664 | **83** | 22,940 | 317 |
| `RevokeAllUserTokens` (empty store) | 78,120 | **78** | 19,789 | 260 |
| `RevokeAllForUserAndAudience` (empty store) | 78,069 | **78** | 20,088 | 267 |

**Notes:**
- `RevokeToken` measures the combined issue-then-revoke lifecycle. A fresh token is issued
  each iteration so the revocation operates on a live token. Neither sub-call is isolated.
- Bulk revocation (`RevokeAllFor*`) measures against a near-empty store. Cost grows linearly
  with matched token count — see the jwtauth storage benchmarks for per-token scaling data.

---

## Results: mTLS Single-Client

| RPC | no-TLS ns/op | mTLS ns/op | Δ |
|---|---|---|---|
| `IssueToken` | 832,298 | 825,259 | **−0.8%** (noise) |
| `RefreshToken` | 912,693 | 902,752 | **−1.1%** (noise) |

Per-RPC latency under mTLS is statistically identical to no-TLS. The TLS 1.3 symmetric
cipher (AES-256-GCM) operates at the transport byte-stream layer, not per-RPC. The
measurable cost of mTLS is in connection establishment (TLS handshake, ~1–5 ms once at
startup), not in steady-state request processing.

---

## Results: 10-Concurrent-Client (no-TLS)

A fixed pool of 10 goroutines dispatches work from a shared channel. `ns/op` is the
wall-clock elapsed time divided by the total number of RPCs dispatched.

| RPC | 1-client ns/op | 10-client ns/op | Throughput gain |
|---|---|---|---|
| `IssueToken` | 832,298 | 92,540 | **~9×** |

At 10 concurrent clients the effective throughput reaches approximately
**10,800 IssueToken req/s** (1 s / 92,540 ns × 10 workers = ~108,000 req/s total;
divided by 10 clients = 10,800 req/s per client). Single-client throughput is ~1,200 req/s.

The 9× gain (not 10×) reflects lock contention in the idempotency interceptor's Redis
pipeline and the gRPC server's connection multiplexing overhead.

---

## vs. Raw jwtauth TokenManager Baseline

These numbers isolate where token-engine's service overhead comes from relative to the
underlying jwtauth library (measured in [jwtauth doc/PERFORMANCE.md](https://github.com/aetomala/jwtauth/blob/main/doc/PERFORMANCE.md)).

| Operation | jwtauth raw (in-memory store) | token-engine gRPC (miniredis) | gRPC overhead |
|---|---|---|---|
| Token issuance | 58,485 ns (~58 µs) | 832,298 ns (~832 µs) | **~774 µs** |
| Token refresh | — | 912,693 ns (~913 µs) | — |

The ~774 µs gRPC overhead for issuance breaks down as (approximate):
- Loopback TCP round-trip + gRPC HTTP/2 framing: ~350–450 µs
- Protobuf serialization/deserialization: ~20–40 µs
- Five interceptor chain passes (correlation → auth → caller authz → idempotency → validation): ~250–350 µs
  - Idempotency interceptor includes a Redis round-trip to miniredis
- jwtauth Redis storage layer (miniredis `Store`): ~35–42 µs

The comparison uses jwtauth's in-memory store baseline (~58 µs) rather than the Redis
baseline because the token manager benchmark in jwtauth was measured with `MemoryRefreshStore`.
Adding the Redis storage cost (~35 µs for `Store`, see jwtauth benchmarks) gives an
estimated raw Redis-backed issuance cost of ~93 µs, leaving ~740 µs for the gRPC service layer.

---

## Regression Detection

Use `benchstat` to compare two benchmark runs and flag regressions:

```bash
# Capture baseline (e.g., before a change)
make benchmark > bench-baseline.txt

# After the change
make benchmark > bench-new.txt

# Compare
benchstat bench-baseline.txt bench-new.txt
```

**Policy:** A PR that degrades any RPC by more than **15%** on the reference machine should
include an explanation of why the regression is acceptable. Regressions ≤ 15% are noise.
Use at least `-count=5` for stable comparisons; `-count=10` for borderline cases.

---

## What These Numbers Don't Include

- **Real Redis network RTT.** All benchmarks use in-process miniredis. Production Redis on
  a datacenter LAN adds ~100–500 µs per round-trip; each RPC makes 1–3 Redis calls.
- **TLS connection establishment.** mTLS numbers reflect a long-lived connection — the
  TLS handshake (~1–5 ms) is paid once at startup.
- **Observability enabled.** Benchmarks use `NoOpLogger`, `NoOpMetrics`, and `NoOpTracer`.
  Production Prometheus metrics and OTEL tracing add < 2% overhead (see
  [jwtauth Observability Tax](https://github.com/aetomala/jwtauth/blob/main/doc/PERFORMANCE.md)).
- **Key rotation under load.** `CursorReconciler` and key rotation run in background
  goroutines. Active rotation (RSA 2048-bit key generation, ~46 ms) does not run on the
  request path and has no effect on these measurements.
- **Concurrent Redis key growth.** Bulk revocation cost scales with total token count.
  The numbers above reflect a near-empty store. Operators with millions of active tokens
  should consult the jwtauth storage-layer benchmarks for per-token scaling factors.
