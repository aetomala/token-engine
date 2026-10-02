package observability

import (
	"context"
	"io"
	"log/slog"

	"github.com/aetomala/jwtauth/pkg/logging"
)

// ===== Service Logger Interface =====

// Logger is the service's logging interface.
type Logger interface {
	Debug(ctx context.Context, msg string, keysAndValues ...interface{})
	Info(ctx context.Context, msg string, keysAndValues ...interface{})
	Warn(ctx context.Context, msg string, keysAndValues ...interface{})
	Error(ctx context.Context, msg string, keysAndValues ...interface{})
	With(keysAndValues ...interface{}) Logger
}

// ===== SlogLogger =====

// SlogLogger wraps *slog.Logger and implements the service Logger interface.
type SlogLogger struct {
	logger *slog.Logger
}

// NewSlogLogger creates a new SlogLogger with a JSON handler writing to w.
func NewSlogLogger(w io.Writer) *SlogLogger {
	handler := slog.NewJSONHandler(w, nil)
	return &SlogLogger{logger: slog.New(handler)}
}

// Debug logs a debug-level message with correlation ID.
func (s *SlogLogger) Debug(ctx context.Context, msg string, keysAndValues ...interface{}) {
	corrID := CorrelationIDFromContext(ctx)
	attrs := append([]interface{}{"correlation_id", corrID}, keysAndValues...)
	s.logger.DebugContext(ctx, msg, attrs...)
}

// Info logs an info-level message with correlation ID.
func (s *SlogLogger) Info(ctx context.Context, msg string, keysAndValues ...interface{}) {
	corrID := CorrelationIDFromContext(ctx)
	attrs := append([]interface{}{"correlation_id", corrID}, keysAndValues...)
	s.logger.InfoContext(ctx, msg, attrs...)
}

// Warn logs a warn-level message with correlation ID.
func (s *SlogLogger) Warn(ctx context.Context, msg string, keysAndValues ...interface{}) {
	corrID := CorrelationIDFromContext(ctx)
	attrs := append([]interface{}{"correlation_id", corrID}, keysAndValues...)
	s.logger.WarnContext(ctx, msg, attrs...)
}

// Error logs an error-level message with correlation ID.
func (s *SlogLogger) Error(ctx context.Context, msg string, keysAndValues ...interface{}) {
	corrID := CorrelationIDFromContext(ctx)
	attrs := append([]interface{}{"correlation_id", corrID}, keysAndValues...)
	s.logger.ErrorContext(ctx, msg, attrs...)
}

// With returns a new SlogLogger with additional fields bound.
func (s *SlogLogger) With(keysAndValues ...interface{}) Logger {
	newLogger := s.logger.With(keysAndValues...)
	return &SlogLogger{logger: newLogger}
}

// ===== LibraryLoggerAdapter =====

// LibraryLoggerAdapter wraps the service Logger and implements the library logging.Logger interface,
// suitable for injecting into every jwtauth component. The library passes the request context as the first
// key-value element because its interface has no context parameter; the adapter uses that context as
// the logging context — so library lines carry the request's correlation_id — and drops it from the
// forwarded fields. Values of library keys that carried credentials in jwtauth <= v1.1.0 are redacted
// on every call. All methods are safe for concurrent use if the wrapped Logger is.
type LibraryLoggerAdapter struct {
	logger Logger // Service logger every call is forwarded to.
}

// NewLibraryLoggerAdapter returns a LibraryLoggerAdapter wrapping the given service Logger.
func NewLibraryLoggerAdapter(logger Logger) *LibraryLoggerAdapter {
	return &LibraryLoggerAdapter{logger: logger}
}

// Debug logs a debug-level message. A leading context.Context in keysAndValues is used as the
// logging context and removed from the fields; without one, context.Background() is used. Values of
// deny-listed credential keys are replaced with "[REDACTED]" — the caller's slice is not modified.
func (a *LibraryLoggerAdapter) Debug(msg string, keysAndValues ...interface{}) {
	ctx, fields := splitLeadingContext(keysAndValues)
	a.logger.Debug(ctx, msg, redactKeysAndValues(fields)...)
}

// Info logs an info-level message. A leading context.Context in keysAndValues is used as the
// logging context and removed from the fields; without one, context.Background() is used. Values of
// deny-listed credential keys are replaced with "[REDACTED]" — the caller's slice is not modified.
func (a *LibraryLoggerAdapter) Info(msg string, keysAndValues ...interface{}) {
	ctx, fields := splitLeadingContext(keysAndValues)
	a.logger.Info(ctx, msg, redactKeysAndValues(fields)...)
}

// Warn logs a warn-level message. A leading context.Context in keysAndValues is used as the
// logging context and removed from the fields; without one, context.Background() is used. Values of
// deny-listed credential keys are replaced with "[REDACTED]" — the caller's slice is not modified.
func (a *LibraryLoggerAdapter) Warn(msg string, keysAndValues ...interface{}) {
	ctx, fields := splitLeadingContext(keysAndValues)
	a.logger.Warn(ctx, msg, redactKeysAndValues(fields)...)
}

// Error logs an error-level message. A leading context.Context in keysAndValues is used as the
// logging context and removed from the fields; without one, context.Background() is used. Values of
// deny-listed credential keys are replaced with "[REDACTED]" — the caller's slice is not modified.
func (a *LibraryLoggerAdapter) Error(msg string, keysAndValues ...interface{}) {
	ctx, fields := splitLeadingContext(keysAndValues)
	a.logger.Error(ctx, msg, redactKeysAndValues(fields)...)
}

// With returns a new LibraryLoggerAdapter wrapping the logger with additional fields bound. A
// leading context.Context is discarded — bound fields cannot carry a context. Values of deny-listed
// credential keys are redacted before binding; the caller's slice is not modified.
func (a *LibraryLoggerAdapter) With(keysAndValues ...interface{}) logging.Logger {
	_, fields := splitLeadingContext(keysAndValues)
	newLogger := a.logger.With(redactKeysAndValues(fields)...)
	return &LibraryLoggerAdapter{logger: newLogger}
}

// ===== Compile-time Assertion =====

// Verify that LibraryLoggerAdapter implements the library logging.Logger interface.
var _ logging.Logger = (*LibraryLoggerAdapter)(nil)
