package observability

import (
	"context"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// LoggerFromContext returns a *slog.Logger derived from l that
// has trace_id and span_id attributes pre-populated from the
// OpenTelemetry SpanContext on ctx. It is the escape hatch for
// call sites that cannot easily use the slog *Context variants —
// for example, a non-slog logger passed through a third-party
// library — but still want correlated logs.
//
// Rules:
//
//   - If l is nil the process-wide slog.Default() is used so
//     callers can write observability.LoggerFromContext(ctx, nil)
//     without hand-threading a logger.
//   - If ctx has no valid SpanContext the input logger is
//     returned unchanged. No empty-valued trace fields are
//     attached, to keep non-traced log lines clean.
//   - The returned logger carries trace_id and span_id as top-
//     level string attributes; additional attributes added via
//     subsequent logger.With calls compose normally.
//
// The handler wired by logger.Init already performs automatic
// correlation for slog.*Context calls. Prefer those when
// possible; LoggerFromContext is intentionally a secondary tool.
func LoggerFromContext(ctx context.Context, l *slog.Logger) *slog.Logger {
	if l == nil {
		l = slog.Default()
	}
	if ctx == nil {
		return l
	}
	sc := trace.SpanContextFromContext(ctx)
	if !sc.IsValid() {
		return l
	}
	return l.With(
		slog.String("trace_id", sc.TraceID().String()),
		slog.String("span_id", sc.SpanID().String()),
	)
}
