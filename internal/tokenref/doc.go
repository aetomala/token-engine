// Package tokenref derives non-reversible references to refresh tokens for logs and audit records.
// Refresh tokens are opaque bearer credentials — only their tokenref.Ref digest may be written to
// any log, span, or audit sink. The algorithm matches jwtauth's internal tokenref package so
// references correlate across token-engine and jwtauth output.
// Primary dependency: crypto/sha256.
package tokenref
