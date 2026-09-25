package tokenref

import (
	"crypto/sha256"
	"encoding/hex"
)

// refLen is the number of hex characters kept from the SHA-256 digest.
const refLen = 16

// Ref returns a non-reversible reference to a refresh token for use in logs and audit
// records. It returns the first 16 lowercase hex characters of the SHA-256 digest of token,
// or "" if token is "". The algorithm matches jwtauth's internal tokenref.Ref so references
// correlate across token-engine and jwtauth output.
func Ref(token string) string {
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])[:refLen]
}
