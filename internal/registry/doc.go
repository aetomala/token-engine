// Package registry provides tenant and caller registry interfaces and static implementations.
// It does not own token lifecycle operations or observability wiring.
// Primary dependencies: github.com/aetomala/jwtauth/pkg/tokens for Manager type,
// and internal/observability for Logger.
package registry
