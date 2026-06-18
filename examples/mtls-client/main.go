// Example: mTLS gRPC client.
//
// Prerequisites:
//   - token-engine running with TOKEN_ENGINE_TLS_MODE=mtls
//   - Client certificate, key, and CA certificate available on the filesystem
//
// Usage:
//
//	CLIENT_CERT=client.crt CLIENT_KEY=client.key CA_CERT=ca.crt go run ./examples/mtls-client
//
// If CLIENT_CERT/CLIENT_KEY/CA_CERT are not set, the example falls back to plaintext.
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

	certFile := os.Getenv("CLIENT_CERT")
	keyFile := os.Getenv("CLIENT_KEY")
	caFile := os.Getenv("CA_CERT")

	var opts []client.Option
	if certFile != "" && keyFile != "" && caFile != "" {
		opts = append(opts, client.WithMTLS(certFile, keyFile, caFile))
	} else {
		log.Println("CLIENT_CERT/CLIENT_KEY/CA_CERT not set — falling back to plaintext")
		opts = append(opts, client.WithPlaintext())
	}

	c, err := client.NewClient(addr, opts...)
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
