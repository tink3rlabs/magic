package telemetry

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestGlobalDefaultsToNoop(t *testing.T) {
	t.Cleanup(func() { SetGlobal(nil) })

	got := Global()
	if got == nil {
		t.Fatal("Global() returned nil; expected no-op telemetry")
	}
	if got.Metrics == nil {
		t.Error("default Metrics is nil")
	}
	if got.Tracer == nil {
		t.Error("default Tracer is nil")
	}
}

func TestSetGlobalInstallsAndResets(t *testing.T) {
	t.Cleanup(func() { SetGlobal(nil) })

	custom := NewNoop()
	SetGlobal(custom)
	if Global() != custom {
		t.Fatal("Global() did not return the installed telemetry")
	}

	SetGlobal(nil)
	if Global() == nil {
		t.Fatal("Global() returned nil after reset")
	}
	if Global() == custom {
		t.Error("Global() still pointed at the previous instance after reset")
	}
}

func TestFromContextFallsBackToGlobal(t *testing.T) {
	t.Cleanup(func() { SetGlobal(nil) })

	installed := NewNoop()
	SetGlobal(installed)

	if got := FromContext(context.Background()); got != installed {
		t.Error("FromContext should fall back to Global when ctx has no telemetry")
	}
}

func TestFromContextReturnsAttached(t *testing.T) {
	t.Cleanup(func() { SetGlobal(nil) })

	attached := NewNoop()
	ctx := WithContext(context.Background(), attached)

	if got := FromContext(ctx); got != attached {
		t.Error("FromContext did not return the attached telemetry")
	}
}

func TestFromContextWithNilContext(t *testing.T) {
	t.Cleanup(func() { SetGlobal(nil) })

	// Should not panic and should return Global.
	//nolint:staticcheck // deliberately passing nil to exercise guard
	if got := FromContext(nil); got == nil {
		t.Error("FromContext(nil) returned nil")
	}
}

func TestWithContextNilTelemetryIsNoop(t *testing.T) {
	ctx := context.Background()
	if got := WithContext(ctx, nil); got != ctx {
		t.Error("WithContext(ctx, nil) must return the input context unchanged")
	}
}

func TestNoopMetricsBackendCreatesInstruments(t *testing.T) {
	b := noopMetricsBackend{}

	c, err := b.Counter(MetricDefinition{Name: "c", Kind: KindCounter})
	if err != nil || c == nil {
		t.Errorf("Counter: got (%v, %v)", c, err)
	}
	c.Add(1, Label{Key: "k", Value: "v"}) // must not panic

	h, err := b.Histogram(MetricDefinition{Name: "h", Kind: KindHistogram})
	if err != nil || h == nil {
		t.Errorf("Histogram: got (%v, %v)", h, err)
	}
	h.Observe(0.5)

	g, err := b.Gauge(MetricDefinition{Name: "g", Kind: KindGauge})
	if err != nil || g == nil {
		t.Errorf("Gauge: got (%v, %v)", g, err)
	}
	g.Set(42)

	ud, err := b.UpDownCounter(MetricDefinition{Name: "u", Kind: KindUpDownCounter})
	if err != nil || ud == nil {
		t.Errorf("UpDownCounter: got (%v, %v)", ud, err)
	}
	ud.Add(-1)
}

func TestNoopTracerProducesValidSpan(t *testing.T) {
	tp := NewNoop()
	ctx, span := tp.Tracer.Start(context.Background(), "op")
	defer span.End()

	// A no-op tracer still returns a span; it just has no recorder.
	if span == nil {
		t.Fatal("Tracer.Start returned nil span")
	}
	if trace.SpanContextFromContext(ctx).IsValid() {
		t.Error("expected no-op span context to be invalid")
	}
}

func TestMetricKindString(t *testing.T) {
	cases := map[MetricKind]string{
		KindCounter:       "counter",
		KindHistogram:     "histogram",
		KindGauge:         "gauge",
		KindUpDownCounter: "updowncounter",
		MetricKind(99):    "unknown",
	}
	for k, want := range cases {
		if got := k.String(); got != want {
			t.Errorf("MetricKind(%d).String() = %q, want %q", int(k), got, want)
		}
	}
}

func TestWarnOnceDeduplicates(t *testing.T) {
	resetWarnOnceForTest()
	t.Cleanup(resetWarnOnceForTest)

	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})))
	t.Cleanup(func() { slog.SetDefault(prev) })

	WarnOnce("key-a", "first call", "n", 1)
	WarnOnce("key-a", "second call", "n", 2)
	WarnOnce("key-b", "different key", "n", 3)

	out := buf.String()
	if !strings.Contains(out, "first call") {
		t.Errorf("expected first call in output, got: %q", out)
	}
	if strings.Contains(out, "second call") {
		t.Errorf("second call for same key should be suppressed, got: %q", out)
	}
	if !strings.Contains(out, "different key") {
		t.Errorf("different key should not be suppressed, got: %q", out)
	}
}
