package observability

import (
	"context"
	"time"
)

// ===== NoOpLogger =====

// NoOpLogger is a no-op implementation of the Logger interface.
type NoOpLogger struct{}

// NewNoOpLogger creates a new NoOpLogger.
func NewNoOpLogger() *NoOpLogger {
	return &NoOpLogger{}
}

// Debug does nothing.
func (n *NoOpLogger) Debug(ctx context.Context, msg string, keysAndValues ...interface{}) {
}

// Info does nothing.
func (n *NoOpLogger) Info(ctx context.Context, msg string, keysAndValues ...interface{}) {
}

// Warn does nothing.
func (n *NoOpLogger) Warn(ctx context.Context, msg string, keysAndValues ...interface{}) {
}

// Error does nothing.
func (n *NoOpLogger) Error(ctx context.Context, msg string, keysAndValues ...interface{}) {
}

// With returns the receiver.
func (n *NoOpLogger) With(keysAndValues ...interface{}) Logger {
	return n
}

// ===== NoOpTracer =====

// NoOpTracer is a no-op implementation of the Tracer interface.
type NoOpTracer struct{}

// NewNoOpTracer creates a new NoOpTracer.
func NewNoOpTracer() *NoOpTracer {
	return &NoOpTracer{}
}

// Start returns the context unchanged and a new NoOpSpan.
func (n *NoOpTracer) Start(ctx context.Context, name string) (context.Context, Span) {
	return ctx, &NoOpSpan{}
}

// ===== NoOpSpan =====

// NoOpSpan is a no-op implementation of the Span interface.
type NoOpSpan struct{}

// SetStatus does nothing.
func (n *NoOpSpan) SetStatus(code StatusCode, description string) {
}

// SetAttributes does nothing.
func (n *NoOpSpan) SetAttributes(attrs map[string]string) {
}

// RecordError does nothing.
func (n *NoOpSpan) RecordError(err error) {
}

// End does nothing.
func (n *NoOpSpan) End() {
}

// ===== NoOpMetrics =====

// NoOpMetrics is a no-op implementation of the Metrics interface.
type NoOpMetrics struct{}

// NewNoOpMetrics creates a new NoOpMetrics.
func NewNoOpMetrics() *NoOpMetrics {
	return &NoOpMetrics{}
}

// IncrementCounter does nothing.
func (n *NoOpMetrics) IncrementCounter(name string, labels map[string]string) {
}

// AddCounter does nothing.
func (n *NoOpMetrics) AddCounter(name string, value float64, labels map[string]string) {
}

// SetGauge does nothing.
func (n *NoOpMetrics) SetGauge(name string, value float64, labels map[string]string) {
}

// RecordHistogram does nothing.
func (n *NoOpMetrics) RecordHistogram(name string, value float64, labels map[string]string) {
}

// RecordDuration does nothing.
func (n *NoOpMetrics) RecordDuration(name string, d time.Duration, labels map[string]string) {
}
