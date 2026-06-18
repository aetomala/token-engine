// Example: plaintext gRPC client using static API key authentication.
//
// Prerequisites:
//   - token-engine running with TOKEN_ENGINE_TLS_MODE=disabled
//   - TOKEN_ENGINE_STATIC_CALLER_KEYS contains the key set in TOKEN_ENGINE_STATIC_KEY below
//
// Usage:
//
//	TOKEN_ENGINE_ADDR=localhost:9090 TOKEN_ENGINE_STATIC_KEY=my-key go run ./examples/grpc-client
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
	addr := envOrDefault("TOKEN_ENGINE_ADDR", "localhost:9090")
	staticKey := envOrDefault("TOKEN_ENGINE_STATIC_KEY", "example-key")

	c, err := client.NewClient(addr,
		client.WithPlaintext(),
		client.WithStaticKey(staticKey),
	)
	if err != nil {
		log.Fatalf("client.NewClient: %v", err)
	}
	defer func() {
		if err := c.Close(); err != nil {
			log.Printf("close: %v", err)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	tenantID := envOrDefault("TOKEN_ENGINE_ISSUER", "local-dev")
	pair, err := c.IssueToken(ctx, &tokenv1.IssueTokenRequest{
		Sub:      "user-123",
		TenantId: tenantID,
	})
	if err != nil {
		log.Fatalf("IssueToken: %v", err)
	}

	fmt.Printf("access_token:  %s\n", pair.GetAccessToken())
	fmt.Printf("refresh_token: %s\n", pair.GetRefreshToken())
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
