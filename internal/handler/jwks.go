package handler

import (
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/aetomala/jwtauth/pkg/keys"
	"github.com/aetomala/token-engine/internal/config"
)

// Unexported constants — NOT in internal/config.
const (
	jwksCacheControlHeader = "Cache-Control"
	// NOT: "cache-control" — net/http canonicalizes header names
	jwksContentTypeJSON = "application/json"
	// NOT: "application/json; charset=utf-8" — JWKS consumers expect bare media type
)

// JWKSHandler returns an http.HandlerFunc that serves the JWKS endpoint.
// Three response paths — in order:
//  1. km.GetJWKS(ctx) returns error      → 503, body {"error":"key manager unavailable"}
//  2. JWKS.Keys is empty                  → 503, body {"error":"no signing keys available"}
//  3. success                             → 200, Cache-Control, JSON-encoded JWKS body
//
// Cache-Control is written ONLY on the success path.
// Content-Type: application/json is written on ALL three paths.
func JWKSHandler(km keys.KeyManager, cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// ===== STEP 1: Fetch JWKS =====
		jwks, err := km.GetJWKS(r.Context())
		if err != nil {
			w.Header().Set("Content-Type", jwksContentTypeJSON)
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "key manager unavailable"})
			return
		}

		// ===== STEP 2: Guard empty key set =====
		if len(jwks.Keys) == 0 {
			w.Header().Set("Content-Type", jwksContentTypeJSON)
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "no signing keys available"})
			return
		}

		// ===== STEP 3: Success path =====
		maxAge := int(cfg.JWKSCacheMaxAge.Seconds())
		w.Header().Set("Content-Type", jwksContentTypeJSON)
		w.Header().Set(jwksCacheControlHeader, fmt.Sprintf("public, max-age=%d", maxAge))
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(jwks)
	}
}
