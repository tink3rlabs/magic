package logger

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestMapLogLevel(t *testing.T) {
	cases := []struct {
		in   string
		want slog.Level
	}{
		{"debug", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"error", slog.LevelError},
		{"", slog.LevelInfo},        // default
		{"NOPE", slog.LevelInfo},    // default
		{"DEBUG", slog.LevelInfo},   // case-sensitive: uppercase hits default
	}
	for _, tc := range cases {
		if got := MapLogLevel(tc.in); got != tc.want {
			t.Errorf("MapLogLevel(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

// captureStdout redirects os.Stdout for the lifetime of fn and
// returns everything written by fn. Used to exercise logger.Init,
// which hard-wires its handler to os.Stdout.
func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w

	var buf bytes.Buffer
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(&buf, r)
	}()

	fn()

	os.Stdout = orig
	_ = w.Close()
	wg.Wait()
	_ = r.Close()
	return buf.String()
}

// withRestoredDefaultLogger captures the process-wide default
// slog logger and restores it after the test, since logger.Init
// clobbers it.
func withRestoredDefaultLogger(t *testing.T) {
	t.Helper()
	orig := slog.Default()
	t.Cleanup(func() { slog.SetDefault(orig) })
}

func TestInitJSONWritesStructuredOutput(t *testing.T) {
	withRestoredDefaultLogger(t)

	out := captureStdout(t, func() {
		Init(&Config{Level: slog.LevelInfo, JSON: true})
		slog.Info("hello", "k", "v")
	})

	if !strings.Contains(out, `"msg":"hello"`) {
		t.Errorf("expected JSON msg field, got %q", out)
	}
	if !strings.Contains(out, `"k":"v"`) {
		t.Errorf("expected JSON attr, got %q", out)
	}
}

func TestInitTextWritesTextOutput(t *testing.T) {
	withRestoredDefaultLogger(t)

	out := captureStdout(t, func() {
		Init(&Config{Level: slog.LevelInfo, JSON: false})
		slog.Info("hello", "k", "v")
	})

	if !strings.Contains(out, "msg=hello") {
		t.Errorf("expected text msg=hello, got %q", out)
	}
	if strings.Contains(out, `"msg"`) {
		t.Errorf("text handler should not emit JSON, got %q", out)
	}
}

func TestInitRespectsConfiguredLevel(t *testing.T) {
	withRestoredDefaultLogger(t)

	out := captureStdout(t, func() {
		Init(&Config{Level: slog.LevelWarn, JSON: true})
		slog.Info("should-be-dropped")
		slog.Warn("should-appear")
	})

	if strings.Contains(out, "should-be-dropped") {
		t.Errorf("info below level should be suppressed, got %q", out)
	}
	if !strings.Contains(out, "should-appear") {
		t.Errorf("warn-level message missing, got %q", out)
	}
}

func TestInitInstallsTraceHandler(t *testing.T) {
	withRestoredDefaultLogger(t)

	// Build a context that carries a valid SpanContext so the
	// wrapper has something to inject. If Init did not wrap the
	// base handler, trace_id/span_id would never appear.
	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("t").Start(context.Background(), "op")
	defer span.End()

	out := captureStdout(t, func() {
		Init(&Config{Level: slog.LevelInfo, JSON: true})
		slog.InfoContext(ctx, "hello")
	})

	if !strings.Contains(out, `"trace_id"`) {
		t.Errorf("Init must install the trace handler; got %q", out)
	}
	if !strings.Contains(out, `"span_id"`) {
		t.Errorf("Init must install the trace handler; got %q", out)
	}
}

func TestTraceHandlerWithGroup(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := newTraceHandler(inner)

	grouped, ok := h.(*traceHandler).WithGroup("svc").(*traceHandler)
	if !ok {
		t.Fatal("WithGroup should return a *traceHandler")
	}
	if grouped.inner == inner {
		t.Error("WithGroup must wrap the inner handler, not reuse it")
	}

	log := slog.New(grouped)
	log.Info("hello", "k", "v")
	out := buf.String()

	// Under a group, attributes are nested: {"svc":{"k":"v"}}.
	if !strings.Contains(out, `"svc":{`) {
		t.Errorf("WithGroup must namespace attributes; got %q", out)
	}
	if !strings.Contains(out, `"k":"v"`) {
		t.Errorf("grouped attribute missing; got %q", out)
	}
}

func TestTraceHandlerWithGroupPreservesTraceCorrelation(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := newTraceHandler(inner).(*traceHandler).WithGroup("g")

	tp := sdktrace.NewTracerProvider()
	ctx, span := tp.Tracer("t").Start(context.Background(), "op")
	defer span.End()

	slog.New(h).InfoContext(ctx, "hi")
	out := buf.String()
	if !strings.Contains(out, `"trace_id"`) {
		t.Errorf("trace_id should still appear after WithGroup; got %q", out)
	}
}

func TestTraceHandlerWithAttrsReturnsNewHandler(t *testing.T) {
	var buf bytes.Buffer
	inner := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	h := newTraceHandler(inner).(*traceHandler)

	withAttrs, ok := h.WithAttrs([]slog.Attr{slog.String("svc", "api")}).(*traceHandler)
	if !ok {
		t.Fatal("WithAttrs should return a *traceHandler")
	}
	if withAttrs.inner == h.inner {
		t.Error("WithAttrs must wrap a new inner handler")
	}
}

func TestTraceHandlerEnabledDelegates(t *testing.T) {
	inner := slog.NewJSONHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelWarn})
	h := newTraceHandler(inner)

	if h.Enabled(context.Background(), slog.LevelDebug) {
		t.Error("debug should be disabled when inner level is warn")
	}
	if !h.Enabled(context.Background(), slog.LevelError) {
		t.Error("error should be enabled when inner level is warn")
	}
}

// TestFatalExitsOne verifies logger.Fatal calls os.Exit(1). It
// does so by re-executing the test binary with an env var that
// triggers a helper in TestMain-equivalent form; the child runs
// Fatal and the parent inspects the exit code.
func TestFatalExitsOne(t *testing.T) {
	if os.Getenv("LOGGER_FATAL_HELPER") == "1" {
		Fatal("boom", "why", "because")
		return
	}

	cmd := exec.Command(os.Args[0], "-test.run=TestFatalExitsOne")
	cmd.Env = append(os.Environ(), "LOGGER_FATAL_HELPER=1")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	cmd.Stdout = &stderr

	err := cmd.Run()
	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("expected *exec.ExitError, got %T: %v (output: %s)", err, err, stderr.String())
	}
	if code := exitErr.ExitCode(); code != 1 {
		t.Errorf("exit code = %d, want 1 (output: %s)", code, stderr.String())
	}
	// The message should have been logged via slog.Error before exit.
	if !strings.Contains(stderr.String(), "boom") {
		t.Errorf("expected %q in output, got %s", "boom", stderr.String())
	}
	if !strings.Contains(stderr.String(), "because") {
		t.Errorf("expected structured arg in output, got %s", stderr.String())
	}
}
