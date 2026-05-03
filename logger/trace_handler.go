package logger

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// traceHandler is a slog.Handler wrapper that automatically
// injects the current OpenTelemetry trace_id and span_id as
// structured attributes on every log record that carries a valid
// SpanContext on its context.
//
// It is installed by Init when observability tracing is enabled.
// Records produced from contexts without a SpanContext (for
// example startup code or standalone background goroutines) are
// passed through unchanged, so the handler is safe to apply
// unconditionally.
type traceHandler struct {
	inner slog.Handler
}

// newTraceHandler wraps inner with automatic trace correlation.
// If inner is nil the function returns nil; callers should guard
// against this upstream.
func newTraceHandler(inner slog.Handler) slog.Handler {
	if inner == nil {
		return nil
	}
	return &traceHandler{inner: inner}
}

func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, r)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{inner: h.inner.WithGroup(name)}
}
