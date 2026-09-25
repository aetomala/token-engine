package observability

import "context"

// ===== Library Credential Redaction =====

// redactedValue replaces the value of any deny-listed library key.
const redactedValue = "[REDACTED]"

// libraryRedactedKeys lists jwtauth log and span attribute keys whose values carried raw
// refresh tokens in jwtauth <= v1.1.0. "tokenID" and "token_id" are deliberately absent —
// from jwtauth v1.1.1 they carry only an access-token jti, which is not a credential.
var libraryRedactedKeys = map[string]struct{}{
	"token":       {},
	"key":         {},
	"cursor":      {},
	"next_cursor": {},
}

// isRedactedKey reports whether key is on the library credential deny-list.
func isRedactedKey(key string) bool {
	_, ok := libraryRedactedKeys[key]
	return ok
}

// redactKeysAndValues returns keysAndValues with the value following every deny-listed key
// replaced by redactedValue. The jwtauth library passes the request context as the first element, so
// pairing starts at index 1 when that element implements context.Context and at index 0
// otherwise. No element is removed or reordered, and a trailing key without a value is left
// as-is. The caller's slice is never mutated — a copy is made on the first redaction, and the
// original slice is returned unchanged when nothing matches.
func redactKeysAndValues(keysAndValues []interface{}) []interface{} {
	// ===== STEP 1: Determine Pairing Offset =====
	offset := 0
	if len(keysAndValues) > 0 {
		if _, ok := keysAndValues[0].(context.Context); ok {
			offset = 1
		}
	}

	// ===== STEP 2: Redact Deny-Listed Values =====
	out := keysAndValues
	copied := false
	for i := offset; i+1 < len(keysAndValues); i += 2 {
		key, ok := keysAndValues[i].(string)
		if !ok || !isRedactedKey(key) {
			continue
		}
		if !copied {
			out = make([]interface{}, len(keysAndValues))
			copy(out, keysAndValues)
			copied = true
		}
		out[i+1] = redactedValue
	}
	return out
}
