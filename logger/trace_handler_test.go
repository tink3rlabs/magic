package logger

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestTraceHandlerInjectsTraceAndSpanID(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := newTraceHandler(inner)
	log := slog.New(h)

	tp := sdktrace.NewTracerProvider()
	tr := tp.Tracer("test")
	ctx, span := tr.Start(context.Background(), "op")
	defer span.End()

	log.InfoContext(ctx, "hello")
	out := buf.String()

	if !strings.Contains(out, "\"trace_id\"") {
		t.Errorf("expected trace_id in output, got %q", out)
	}
	if !strings.Contains(out, "\"span_id\"") {
		t.Errorf("expected span_id in output, got %q", out)
	}
}

func TestTraceHandlerNoSpanOmitsAttrs(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := newTraceHandler(inner)
	log := slog.New(h)

	log.InfoContext(context.Background(), "hello")
	out := buf.String()
	if strings.Contains(out, "trace_id") {
		t.Errorf("trace_id should not appear when ctx has no span, got %q", out)
	}
}

func TestTraceHandlerNonContextCallsPassThrough(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := newTraceHandler(inner)
	log := slog.New(h)

	log.Info("hello", "key", "value")
	out := buf.String()
	if !strings.Contains(out, "hello") {
		t.Errorf("expected message, got %q", out)
	}
	if strings.Contains(out, "trace_id") {
		t.Errorf("trace_id should not appear without context, got %q", out)
	}
}

func TestTraceHandlerPreservesWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := newTraceHandler(inner)
	log := slog.New(h).With("service", "test")

	log.Info("message")
	out := buf.String()
	if !strings.Contains(out, `"service":"test"`) {
		t.Errorf("With attrs must still appear, got %q", out)
	}
}

func TestNewTraceHandlerNilReturnsNil(t *testing.T) {
	if got := newTraceHandler(nil); got != nil {
		t.Error("newTraceHandler(nil) should return nil")
	}
}
