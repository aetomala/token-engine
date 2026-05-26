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

// LibraryLoggerAdapter wraps the service Logger and implements the library logging.Logger interface.
type LibraryLoggerAdapter struct {
	logger Logger
}

// NewLibraryLoggerAdapter creates a new LibraryLoggerAdapter wrapping the given service Logger.
func NewLibraryLoggerAdapter(logger Logger) *LibraryLoggerAdapter {
	return &LibraryLoggerAdapter{logger: logger}
}

// Debug logs a debug-level message using context.Background().
func (a *LibraryLoggerAdapter) Debug(msg string, keysAndValues ...interface{}) {
	// context discarded — library Logger interface has no ctx parameter
	a.logger.Debug(context.Background(), msg, keysAndValues...)
}

// Info logs an info-level message using context.Background().
func (a *LibraryLoggerAdapter) Info(msg string, keysAndValues ...interface{}) {
	// context discarded — library Logger interface has no ctx parameter
	a.logger.Info(context.Background(), msg, keysAndValues...)
}

// Warn logs a warn-level message using context.Background().
func (a *LibraryLoggerAdapter) Warn(msg string, keysAndValues ...interface{}) {
	// context discarded — library Logger interface has no ctx parameter
	a.logger.Warn(context.Background(), msg, keysAndValues...)
}

// Error logs an error-level message using context.Background().
func (a *LibraryLoggerAdapter) Error(msg string, keysAndValues ...interface{}) {
	// context discarded — library Logger interface has no ctx parameter
	a.logger.Error(context.Background(), msg, keysAndValues...)
}

// With returns a new LibraryLoggerAdapter wrapping the logger with additional fields bound.
func (a *LibraryLoggerAdapter) With(keysAndValues ...interface{}) logging.Logger {
	// context discarded — library Logger interface has no ctx parameter
	newLogger := a.logger.With(keysAndValues...)
	return &LibraryLoggerAdapter{logger: newLogger}
}

// ===== Compile-time Assertion =====

// Verify that LibraryLoggerAdapter implements the library logging.Logger interface.
var _ logging.Logger = (*LibraryLoggerAdapter)(nil)
