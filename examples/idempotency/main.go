// Example: idempotent request retries via the x-idempotency-key metadata header.
//
// Prerequisites:
//   - token-engine running with TOKEN_ENGINE_TLS_MODE=disabled
//   - TOKEN_ENGINE_STATIC_CALLER_KEYS contains the key set in TOKEN_ENGINE_STATIC_KEY below
//
// Quickest start — use the docker-compose.yaml at the repo root:
//
//	docker compose up   # or: podman compose up
//
// Usage:
//
//	TOKEN_ENGINE_STATIC_KEY=devkey go run ./examples/idempotency
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/aetomala/token-engine/client"
	tokenv1 "github.com/aetomala/token-engine/gen/v1"
)

func main() {
	addr := envOrDefault("TOKEN_ENGINE_ADDR", "localhost:9090")
	key := envOrDefault("TOKEN_ENGINE_STATIC_KEY", "example-key")
	tenantID := envOrDefault("TOKEN_ENGINE_ISSUER", "local-dev")

	c, err := client.NewClient(addr,
		client.WithPlaintext(),
		client.WithStaticKey(key),
	)
	if err != nil {
		log.Fatalf("client.NewClient: %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()

	demoRetry(c, tenantID)
	demoContentMismatch(c, tenantID)
	demoDeprecatedField(c, tenantID)
}

// demoRetry shows the normal case: a caller retries a request with the same
// x-idempotency-key header and the same content — the second call returns the
// exact cached response from the first, rather than issuing a second token pair.
func demoRetry(c client.Client, tenantID string) {
	fmt.Println("=== 1. Same key, same content — retry returns the cached response ===")

	ctx1, cancel1 := idempotentCtx("demo-retry-key")
	defer cancel1()
	first, err := c.IssueToken(ctx1, &tokenv1.IssueTokenRequest{
		Sub:      "user-retry-demo",
		TenantId: tenantID,
	})
	if err != nil {
		log.Fatalf("IssueToken (first): %v", err)
	}
	fmt.Printf("first call:  access_token=%s\n", first.GetAccessToken())

	ctx2, cancel2 := idempotentCtx("demo-retry-key")
	defer cancel2()
	second, err := c.IssueToken(ctx2, &tokenv1.IssueTokenRequest{
		Sub:      "user-retry-demo",
		TenantId: tenantID,
	})
	if err != nil {
		log.Fatalf("IssueToken (second): %v", err)
	}
	fmt.Printf("second call: access_token=%s (identical: %v)\n\n", second.GetAccessToken(), second.GetAccessToken() == first.GetAccessToken())
}

// demoContentMismatch shows what happens when the same idempotency key is reused
// for a request with genuinely different content (a different subject here). The
// key is bound to the content that first produced a response — a mismatch returns
// codes.FailedPrecondition instead of silently returning the first response for a
// different request. See ADR-013.
func demoContentMismatch(c client.Client, tenantID string) {
	fmt.Println("=== 2. Same key, different content — rejected, not silently mismatched ===")

	ctx1, cancel1 := idempotentCtx("demo-mismatch-key")
	defer cancel1()
	if _, err := c.IssueToken(ctx1, &tokenv1.IssueTokenRequest{
		Sub:      "user-a",
		TenantId: tenantID,
	}); err != nil {
		log.Fatalf("IssueToken (user-a): %v", err)
	}
	fmt.Println("first call:  sub=user-a — succeeds")

	ctx2, cancel2 := idempotentCtx("demo-mismatch-key")
	defer cancel2()
	_, err := c.IssueToken(ctx2, &tokenv1.IssueTokenRequest{
		Sub:      "user-b",
		TenantId: tenantID,
	})
	fmt.Printf("second call: sub=user-b, same key — %s\n\n", status.Code(err))
	if status.Code(err) != codes.FailedPrecondition {
		log.Fatalf("expected codes.FailedPrecondition, got: %v", err)
	}
}

// demoDeprecatedField shows the idempotency_key request field, deprecated in favor
// of the x-idempotency-key header (see ADR-015), still working as a fallback when
// the header is absent. New integrations should use the header — this exists only
// to show existing callers on the field that they are not broken.
func demoDeprecatedField(c client.Client, tenantID string) {
	fmt.Println("=== 3. Deprecated idempotency_key field — still works as a fallback ===")

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	first, err := c.IssueToken(ctx, &tokenv1.IssueTokenRequest{
		Sub:            "user-deprecated-field-demo",
		TenantId:       tenantID,
		IdempotencyKey: "demo-deprecated-field-key", //nolint:staticcheck // demonstrating the fallback, not recommending it
	})
	if err != nil {
		log.Fatalf("IssueToken (first): %v", err)
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel2()
	second, err := c.IssueToken(ctx2, &tokenv1.IssueTokenRequest{
		Sub:            "user-deprecated-field-demo",
		TenantId:       tenantID,
		IdempotencyKey: "demo-deprecated-field-key", //nolint:staticcheck // demonstrating the fallback, not recommending it
	})
	if err != nil {
		log.Fatalf("IssueToken (second): %v", err)
	}
	fmt.Printf("identical: %v (prefer the x-idempotency-key header for new integrations)\n", second.GetAccessToken() == first.GetAccessToken())
}

// idempotentCtx returns a context carrying the x-idempotency-key metadata header,
// the standard grpc-go pattern for attaching a per-call header — token-engine has
// no dedicated client.go helper for this, since the key varies per call the way a
// connection-level Option (like WithStaticKey) is not suited for.
func idempotentCtx(key string) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	ctx = metadata.AppendToOutgoingContext(ctx, "x-idempotency-key", key)
	return ctx, cancel
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
