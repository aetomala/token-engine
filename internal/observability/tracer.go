package observability

import (
	"context"
	"fmt"

	"github.com/aetomala/jwtauth/pkg/tracing"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

// ===== Service Tracer and Span Interfaces =====

// Tracer is the service's span creation interface.
type Tracer interface {
	Start(ctx context.Context, name string) (context.Context, Span)
}

// Span is the service's span interface.
type Span interface {
	SetStatus(code StatusCode, description string)
	SetAttributes(attrs map[string]string)
	RecordError(err error)
	End()
}

// StatusCode represents the status of a span.
type StatusCode int

const (
	// StatusOK indicates successful span completion.
	StatusOK StatusCode = 0
	// StatusError indicates span failure.
	StatusError StatusCode = 1
)

// ===== OtelTracer and OtelSpan =====

// OtelTracer wraps the OpenTelemetry trace.Tracer and implements the service Tracer interface.
type OtelTracer struct {
	tracer oteltrace.Tracer
}

// NewOtelTracer creates a new OtelTracer wrapping the given OpenTelemetry tracer.
func NewOtelTracer(tracer oteltrace.Tracer) *OtelTracer {
	return &OtelTracer{tracer: tracer}
}

// Start creates a new span with SpanKindInternal and returns it wrapped in OtelSpan.
func (t *OtelTracer) Start(ctx context.Context, name string) (context.Context, Span) {
	ctx, span := t.tracer.Start(ctx, name, oteltrace.WithSpanKind(oteltrace.SpanKindInternal))
	return ctx, &OtelSpan{span: span}
}

// OtelSpan wraps an OpenTelemetry span and implements the service Span interface.
type OtelSpan struct {
	span oteltrace.Span
}

// SetStatus sets the status of the span, converting service StatusCode to OTel codes.
func (s *OtelSpan) SetStatus(code StatusCode, description string) {
	switch code {
	case StatusOK:
		s.span.SetStatus(codes.Ok, "")
	case StatusError:
		s.span.SetStatus(codes.Error, description)
	}
}

// SetAttributes sets string attributes on the span, converting to OTel attributes.
func (s *OtelSpan) SetAttributes(attrs map[string]string) {
	kvs := make([]attribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		kvs = append(kvs, attribute.String(k, v))
	}
	s.span.SetAttributes(kvs...)
}

// RecordError records an error on the span.
func (s *OtelSpan) RecordError(err error) {
	s.span.RecordError(err)
}

// End ends the span.
func (s *OtelSpan) End() {
	s.span.End()
}

// ===== LibraryOtelTracer and LibraryOtelSpan =====

// LibraryOtelTracer wraps the OpenTelemetry trace.Tracer and implements the library tracing.Tracer interface.
type LibraryOtelTracer struct {
	tracer oteltrace.Tracer
}

// NewLibraryOtelTracer creates a new LibraryOtelTracer wrapping the given OpenTelemetry tracer.
func NewLibraryOtelTracer(tracer oteltrace.Tracer) *LibraryOtelTracer {
	return &LibraryOtelTracer{tracer: tracer}
}

// Start creates a new span with SpanKindInternal and returns it wrapped in LibraryOtelSpan.
func (t *LibraryOtelTracer) Start(ctx context.Context, name string) (context.Context, tracing.Span) {
	ctx, span := t.tracer.Start(ctx, name, oteltrace.WithSpanKind(oteltrace.SpanKindInternal))
	return ctx, &LibraryOtelSpan{span: span}
}

// LibraryOtelSpan wraps an OpenTelemetry span and implements the library tracing.Span interface.
type LibraryOtelSpan struct {
	span oteltrace.Span
}

// End ends the span.
func (s *LibraryOtelSpan) End() {
	s.span.End()
}

// SetAttribute sets a single attribute on the span, converting non-string values using Sprintf.
func (s *LibraryOtelSpan) SetAttribute(key string, value interface{}) {
	var attr attribute.KeyValue
	switch v := value.(type) {
	case string:
		attr = attribute.String(key, v)
	default:
		attr = attribute.String(key, fmt.Sprintf("%v", v))
	}
	s.span.SetAttributes(attr)
}

// SetAttributes sets multiple attributes on the span, converting non-string values using Sprintf.
func (s *LibraryOtelSpan) SetAttributes(attrs map[string]interface{}) {
	kvs := make([]attribute.KeyValue, 0, len(attrs))
	for k, v := range attrs {
		switch val := v.(type) {
		case string:
			kvs = append(kvs, attribute.String(k, val))
		default:
			kvs = append(kvs, attribute.String(k, fmt.Sprintf("%v", val)))
		}
	}
	s.span.SetAttributes(kvs...)
}

// RecordError records an error on the span.
func (s *LibraryOtelSpan) RecordError(err error) {
	s.span.RecordError(err)
}

// SetStatus sets the status of the span, mapping library StatusCode to OTel codes.
func (s *LibraryOtelSpan) SetStatus(code tracing.StatusCode, description string) {
	switch code {
	case tracing.StatusOK:
		s.span.SetStatus(codes.Ok, "")
	case tracing.StatusError:
		s.span.SetStatus(codes.Error, description)
	}
}

// ===== Compile-time Assertion =====

// Verify that LibraryOtelTracer implements the library tracing.Tracer interface.
var _ tracing.Tracer = (*LibraryOtelTracer)(nil)
