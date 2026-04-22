package observability

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestResolveOTLPEndpointExplicit(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "signal:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "generic:4317")
	if got := resolveOTLPEndpoint("explicit:4317", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); got != "explicit:4317" {
		t.Errorf("explicit wins: got %q", got)
	}
}

func TestResolveOTLPEndpointSignalEnv(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "signal:4317")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "generic:4317")
	if got := resolveOTLPEndpoint("", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); got != "signal:4317" {
		t.Errorf("signal env: got %q", got)
	}
}

func TestResolveOTLPEndpointGenericEnv(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "generic:4317")
	if got := resolveOTLPEndpoint("", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); got != "generic:4317" {
		t.Errorf("generic env fallback: got %q", got)
	}
}

func TestResolveOTLPEndpointEmpty(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	if got := resolveOTLPEndpoint("", "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT"); got != "" {
		t.Errorf("no values should be empty: got %q", got)
	}
}

func TestResolveSamplerNilUsesAlways(t *testing.T) {
	s := resolveSampler(nil)
	if got := s.Description(); got != sdktrace.AlwaysSample().Description() {
		t.Errorf("nil ratio => AlwaysSample, got %q", got)
	}
}

func TestResolveSamplerZeroNeverSamples(t *testing.T) {
	r := 0.0
	if got := resolveSampler(&r).Description(); got != sdktrace.NeverSample().Description() {
		t.Errorf("ratio 0 => NeverSample, got %q", got)
	}
}

func TestResolveSamplerOneAlwaysSamples(t *testing.T) {
	r := 1.0
	if got := resolveSampler(&r).Description(); got != sdktrace.AlwaysSample().Description() {
		t.Errorf("ratio 1 => AlwaysSample, got %q", got)
	}
}

func TestResolveSamplerFractionReturnsRatio(t *testing.T) {
	r := 0.25
	if got := resolveSampler(&r).Description(); got != sdktrace.TraceIDRatioBased(0.25).Description() {
		t.Errorf("ratio 0.25 => TraceIDRatioBased, got %q", got)
	}
}

func TestResolveSamplerNegativeClampsToNever(t *testing.T) {
	r := -0.5 // validate() catches this but resolveSampler must be defensive
	if got := resolveSampler(&r).Description(); got != sdktrace.NeverSample().Description() {
		t.Errorf("negative ratio => NeverSample, got %q", got)
	}
}

func TestSetupTracerDisabledReturnsNoop(t *testing.T) {
	cfg := Config{EnableTracing: false}
	tp, shutdown, err := setupTracer(context.Background(), cfg, resource.Empty())
	if err != nil {
		t.Fatalf("setupTracer: %v", err)
	}
	if tp == nil {
		t.Fatal("expected non-nil TracerProvider")
	}
	_, span := tp.Tracer("t").Start(context.Background(), "s")
	if span.SpanContext().IsValid() {
		t.Error("noop tracer must not emit valid span contexts")
	}
	span.End()
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown: %v", err)
	}
}

func TestSetupTracerEnabledMissingEndpointErrors(t *testing.T) {
	// Clear envs so only explicit cfg matters.
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	cfg := Config{EnableTracing: true}
	_, _, err := setupTracer(context.Background(), cfg, resource.Empty())
	if err == nil {
		t.Error("expected error when EnableTracing=true and no endpoint configured")
	}
}

func TestSetupTracerEnabledWithEndpointSucceeds(t *testing.T) {
	cfg := Config{
		EnableTracing:      true,
		TracesOTLPEndpoint: "localhost:4317",
		TracesOTLPInsecure: true,
	}
	tp, shutdown, err := setupTracer(context.Background(), cfg, resource.Empty())
	if err != nil {
		t.Fatalf("setupTracer: %v", err)
	}
	if tp == nil {
		t.Error("expected non-nil TracerProvider")
	}
	// Shutdown with a quickly-cancelled context so we do not
	// wait for the gRPC dial to actually time out.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_ = shutdown(ctx)
}
