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

// splitLeadingContext separates the request context jwtauth passes as the first key-value element
// from the fields that follow it. The library's logging.Logger has no context parameter, so it
// passes the context positionally — see aetomala/jwtauth#288. Returns the context and the remaining
// elements when the first element implements context.Context, or context.Background() and
// keysAndValues unchanged otherwise. The remaining elements are a reslice — the caller's slice is
// never mutated.
func splitLeadingContext(keysAndValues []interface{}) (context.Context, []interface{}) {
	if len(keysAndValues) > 0 {
		if ctx, ok := keysAndValues[0].(context.Context); ok {
			return ctx, keysAndValues[1:]
		}
	}
	return context.Background(), keysAndValues
}

// redactKeysAndValues returns keysAndValues with the value following every deny-listed key
// replaced by redactedValue. Pairing starts at index 0 — callers strip a leading context with
// splitLeadingContext first. No element is removed or reordered, and a trailing key without a
// value is left as-is. The caller's slice is never mutated — a copy is made on the first
// redaction, and the original slice is returned unchanged when nothing matches.
func redactKeysAndValues(keysAndValues []interface{}) []interface{} {
	out := keysAndValues
	copied := false
	for i := 0; i+1 < len(keysAndValues); i += 2 {
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
