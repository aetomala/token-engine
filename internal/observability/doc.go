// Package observability provides service observability interfaces, real implementations,
// library adapter types, centralized error mapping, and correlation ID management.
// It does not own component construction or dependency injection — those belong to main.go.
// Primary dependencies: log/slog, go.opentelemetry.io/otel, github.com/prometheus/client_golang,
// and github.com/aetomala/jwtauth/pkg/tracing and pkg/logging for compile-time adapter assertions.
package observability
