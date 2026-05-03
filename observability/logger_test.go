package observability_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/tink3rlabs/magic/observability"
)

// jsonLogger returns a JSON slog.Logger writing into buf so tests
// can inspect structured attrs attached to a record.
func jsonLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

// spanContext starts a real SDK span on ctx (so SpanContext is
// valid) and returns both the derived context and a cleanup.
func spanContext(t *testing.T) context.Context {
	t.Helper()
	tp := sdktrace.NewTracerProvider()
	t.Cleanup(func() { _ = tp.Shutdown(context.Background()) })
	ctx, span := tp.Tracer("test").Start(context.Background(), "op")
	t.Cleanup(func() { span.End() })
	return ctx
}

func TestLoggerFromContextAttachesTraceAndSpanIDs(t *testing.T) {
	ctx := spanContext(t)

	var buf bytes.Buffer
	base := jsonLogger(&buf)

	l := observability.LoggerFromContext(ctx, base)
	l.Info("hello")

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("log not valid JSON: %v\n%s", err, buf.String())
	}
	if _, ok := rec["trace_id"]; !ok {
		t.Fatalf("missing trace_id in log record: %v", rec)
	}
	if _, ok := rec["span_id"]; !ok {
		t.Fatalf("missing span_id in log record: %v", rec)
	}
	if s, _ := rec["trace_id"].(string); len(s) != 32 {
		t.Errorf("trace_id %q is not a 32-hex-char W3C trace ID", s)
	}
	if s, _ := rec["span_id"].(string); len(s) != 16 {
		t.Errorf("span_id %q is not a 16-hex-char W3C span ID", s)
	}
}

func TestLoggerFromContextReturnsInputUnchangedWithoutSpan(t *testing.T) {
	var buf bytes.Buffer
	base := jsonLogger(&buf)

	got := observability.LoggerFromContext(context.Background(), base)
	if got != base {
		t.Fatalf("expected same logger when ctx has no span")
	}

	got.Info("hello")
	if strings.Contains(buf.String(), "trace_id") {
		t.Errorf("log unexpectedly contains trace_id: %s", buf.String())
	}
}

func TestLoggerFromContextFallsBackToSlogDefault(t *testing.T) {
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(jsonLogger(&buf))
	t.Cleanup(func() { slog.SetDefault(prev) })

	ctx := spanContext(t)
	l := observability.LoggerFromContext(ctx, nil)
	if l == nil {
		t.Fatalf("LoggerFromContext returned nil logger")
	}

	l.Info("via default")
	if !strings.Contains(buf.String(), "trace_id") {
		t.Errorf("expected trace_id in output from default-derived logger: %s", buf.String())
	}
}

func TestLoggerFromContextNilContextReturnsInputLogger(t *testing.T) {
	var buf bytes.Buffer
	base := jsonLogger(&buf)

	//nolint:staticcheck // explicitly passing nil to exercise guard
	if got := observability.LoggerFromContext(nil, base); got != base {
		t.Fatalf("expected input logger when ctx is nil")
	}
}
