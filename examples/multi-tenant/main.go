// Example: per-tenant token isolation with two independent token-engine instances.
//
// Prerequisites:
//   - Two token-engine servers running — see docker-compose.yaml in this directory:
//       docker compose up   # or: podman compose up
//
// Usage:
//
//	TOKEN_ENGINE_STATIC_KEY=devkey go run ./examples/multi-tenant
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/aetomala/token-engine/client"
	tokenv1 "github.com/aetomala/token-engine/gen/v1"
)

func main() {
	alphaAddr := envOrDefault("TOKEN_ENGINE_ALPHA_ADDR", "localhost:9090")
	betaAddr  := envOrDefault("TOKEN_ENGINE_BETA_ADDR", "localhost:9091")
	key       := envOrDefault("TOKEN_ENGINE_STATIC_KEY", "devkey")

	// ===== Connect to both tenant servers =====
	alphaClient, err := client.NewClient(alphaAddr,
		client.WithPlaintext(),
		client.WithStaticKey(key),
	)
	if err != nil {
		log.Fatalf("client.NewClient (alpha): %v", err)
	}
	defer func() {
		if err := alphaClient.Close(); err != nil {
			log.Printf("close alpha: %v", err)
		}
	}()

	betaClient, err := client.NewClient(betaAddr,
		client.WithPlaintext(),
		client.WithStaticKey(key),
	)
	if err != nil {
		log.Fatalf("client.NewClient (beta): %v", err)
	}
	defer func() {
		if err := betaClient.Close(); err != nil {
			log.Printf("close beta: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// ===== Issue tokens — tenant-alpha =====
	// tenant_id must match the issuer registered on the target server.
	// Each server registers exactly one tenant at startup via TOKEN_ENGINE_ISSUER.
	alphaPair, err := alphaClient.IssueToken(ctx, &tokenv1.IssueTokenRequest{
		Sub:      "user-123",
		TenantId: "tenant-alpha",
	})
	if err != nil {
		log.Fatalf("IssueToken (alpha): %v", err)
	}
	fmt.Printf("[tenant-alpha] access_token:  %s\n", alphaPair.GetAccessToken())
	fmt.Printf("[tenant-alpha] refresh_token: %s\n\n", alphaPair.GetRefreshToken())

	// Issue a second token for the cross-tenant rejection test below — kept separate
	// so it is not rotated before it is used.
	alpha2, err := alphaClient.IssueToken(ctx, &tokenv1.IssueTokenRequest{
		Sub:      "cross-test-user",
		TenantId: "tenant-alpha",
	})
	if err != nil {
		log.Fatalf("IssueToken (alpha/cross-test): %v", err)
	}

	// ===== Issue tokens — tenant-beta =====
	betaPair, err := betaClient.IssueToken(ctx, &tokenv1.IssueTokenRequest{
		Sub:      "user-456",
		TenantId: "tenant-beta",
	})
	if err != nil {
		log.Fatalf("IssueToken (beta): %v", err)
	}
	fmt.Printf("[tenant-beta]  access_token:  %s\n", betaPair.GetAccessToken())
	fmt.Printf("[tenant-beta]  refresh_token: %s\n\n", betaPair.GetRefreshToken())

	// ===== RefreshToken — tenant-alpha =====
	// Rotates the access+refresh pair. The old refresh token is revoked atomically.
	alphaRefreshed, err := alphaClient.RefreshToken(ctx, &tokenv1.RefreshTokenRequest{
		RefreshToken: alphaPair.GetRefreshToken(),
		TenantId:     "tenant-alpha",
	})
	if err != nil {
		log.Fatalf("RefreshToken (alpha): %v", err)
	}
	fmt.Printf("[tenant-alpha] refreshed access_token:  %s\n", alphaRefreshed.GetAccessToken())
	fmt.Printf("[tenant-alpha] refreshed refresh_token: %s\n\n", alphaRefreshed.GetRefreshToken())

	// ===== RefreshToken — tenant-beta =====
	betaRefreshed, err := betaClient.RefreshToken(ctx, &tokenv1.RefreshTokenRequest{
		RefreshToken: betaPair.GetRefreshToken(),
		TenantId:     "tenant-beta",
	})
	if err != nil {
		log.Fatalf("RefreshToken (beta): %v", err)
	}
	fmt.Printf("[tenant-beta]  refreshed access_token:  %s\n", betaRefreshed.GetAccessToken())
	fmt.Printf("[tenant-beta]  refreshed refresh_token: %s\n\n", betaRefreshed.GetRefreshToken())

	// ===== Cross-tenant isolation =====
	// Present tenant-alpha's refresh token to the alpha server but claim it belongs
	// to "tenant-beta". The alpha server has only "tenant-alpha" registered; it has
	// no record of "tenant-beta" and returns NOT_FOUND.
	//
	// In Redis, each tenant's tokens are stored under an isolated key prefix:
	//   tenant-alpha: "tenant-alpha:refresh:<token-id>"
	//   tenant-beta:  "tenant-beta:refresh:<token-id>"
	// A token issued under one prefix cannot be found — or validated — under another.
	_, err = alphaClient.RefreshToken(ctx, &tokenv1.RefreshTokenRequest{
		RefreshToken: alpha2.GetRefreshToken(),
		TenantId:     "tenant-beta",
	})
	if err != nil {
		fmt.Printf("[cross-tenant] RefreshToken correctly rejected: %v\n", err)
	} else {
		log.Fatal("[cross-tenant] RefreshToken succeeded — tenant isolation broken")
	}
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
