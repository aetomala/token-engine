// Package interceptor implements the gRPC interceptor chain for the token-engine service.
// It does not implement the correlation interceptor — that lives in internal/observability.
// Chain order (maintained in main.go): otelgrpc → correlation → auth → caller auth → validation → idempotency (ADR-016).
// Primary dependencies: internal/observability for Logger and context key functions,
// internal/registry for CallerRegistry, internal/store for IdempotencyStore.
package interceptor
