// Example: issuing a token with custom claims and validating them downstream.
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
//	TOKEN_ENGINE_STATIC_KEY=devkey go run ./examples/custom-claims
package main

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math/big"
	"net/http"
	"os"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/aetomala/token-engine/client"
	tokenv1 "github.com/aetomala/token-engine/gen/v1"
)

// tokenClaims holds the registered JWT claims plus the custom fields
// stamped by token-engine. Custom claims from the IssueTokenRequest.Claims
// map are promoted to top-level JWT fields — they are not nested under a
// "claims" key.
type tokenClaims struct {
	jwt.RegisteredClaims
	Role  string `json:"role"`
	OrgID string `json:"org_id"`
	Tier  string `json:"tier"`
}

func main() {
	addr     := envOrDefault("TOKEN_ENGINE_ADDR",      "localhost:9090")
	httpAddr := envOrDefault("TOKEN_ENGINE_HTTP_ADDR", "localhost:8080")
	key      := envOrDefault("TOKEN_ENGINE_STATIC_KEY", "example-key")

	// ===== Connect =====
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

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// ===== Issue token with custom claims =====
	pair, err := c.IssueToken(ctx, &tokenv1.IssueTokenRequest{
		Sub:      "user-123",
		TenantId: "local-dev",
		Claims: map[string]string{
			"role":   "admin",
			"org_id": "acme-corp",
			"tier":   "premium",
		},
	})
	if err != nil {
		log.Fatalf("IssueToken: %v", err)
	}

	fmt.Printf("access_token:  %s\n\n", pair.GetAccessToken())

	// ===== Validate and decode the access token =====
	// A downstream service (API gateway, resource server) validates the token
	// by fetching the service's public keys from the JWKS endpoint and verifying
	// the RS256 signature — no shared secret needed.
	jwksURL := fmt.Sprintf("http://%s/.well-known/jwks.json", httpAddr)
	publicKey, err := fetchFirstRSAKey(jwksURL)
	if err != nil {
		log.Fatalf("fetchFirstRSAKey: %v", err)
	}

	var claims tokenClaims
	_, err = jwt.ParseWithClaims(pair.GetAccessToken(), &claims, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodRSA); !ok {
			return nil, fmt.Errorf("unexpected signing method: %v", t.Header["alg"])
		}
		return publicKey, nil
	})
	if err != nil {
		log.Fatalf("jwt.ParseWithClaims: %v", err)
	}

	// ===== Print decoded claims =====
	fmt.Println("=== Registered claims ===")
	fmt.Printf("sub: %s\n", claims.Subject)
	fmt.Printf("iss: %s\n", claims.Issuer)
	fmt.Printf("aud: %v\n", claims.Audience)
	fmt.Printf("exp: %s\n", claims.ExpiresAt.Time.Format(time.RFC3339))

	fmt.Println("\n=== Custom claims ===")
	fmt.Printf("role:   %s\n", claims.Role)
	fmt.Printf("org_id: %s\n", claims.OrgID)
	fmt.Printf("tier:   %s\n", claims.Tier)
}

// fetchFirstRSAKey fetches the JWKS from jwksURL and returns the first RSA public key.
// It reconstructs the key from the base64url-encoded n and e components in the JWKS JSON.
func fetchFirstRSAKey(jwksURL string) (*rsa.PublicKey, error) {
	resp, err := http.Get(jwksURL) //nolint:noctx
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", jwksURL, err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var jwks struct {
		Keys []struct {
			N string `json:"n"`
			E string `json:"e"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
		return nil, fmt.Errorf("unmarshal JWKS: %w", err)
	}
	if len(jwks.Keys) == 0 {
		return nil, fmt.Errorf("no keys in JWKS response from %s", jwksURL)
	}

	nBytes, err := base64.RawURLEncoding.DecodeString(jwks.Keys[0].N)
	if err != nil {
		return nil, fmt.Errorf("decode n: %w", err)
	}
	eBytes, err := base64.RawURLEncoding.DecodeString(jwks.Keys[0].E)
	if err != nil {
		return nil, fmt.Errorf("decode e: %w", err)
	}

	return &rsa.PublicKey{
		N: new(big.Int).SetBytes(nBytes),
		E: int(new(big.Int).SetBytes(eBytes).Int64()),
	}, nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
