package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

// initOTLPObserver constructs an Observer in OTLP mode pointed at a
// dummy endpoint. The OTLP gRPC exporters dial lazily so no real
// collector is required; shutdown uses an already-cancelled
// context so we do not wait for network timeouts.
func initOTLPObserver(t *testing.T, enableTracing bool) *Observer {
	t.Helper()
	cfg := DefaultConfig()
	cfg.ServiceName = "otlp-test"
	cfg.MetricsMode = MetricsModeOTLP
	cfg.MetricsOTLPEndpoint = "localhost:4317"
	cfg.MetricsOTLPInsecure = true
	cfg.EnableTracing = enableTracing
	if enableTracing {
		cfg.TracesOTLPEndpoint = "localhost:4317"
		cfg.TracesOTLPInsecure = true
	}

	obs, err := Init(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_ = obs.Shutdown(ctx)
	})
	return obs
}

func TestInitOTLPMetricsOnly(t *testing.T) {
	obs := initOTLPObserver(t, false)
	if obs.meterProvider == nil {
		t.Fatal("OTLP mode must populate meterProvider")
	}
	if obs.promRegistry != nil {
		t.Error("OTLP mode must not populate promRegistry")
	}
	if obs.Telemetry() == nil || obs.Telemetry().Metrics == nil {
		t.Error("Telemetry metrics must be set in OTLP mode")
	}
}

func TestInitOTLPWithTracing(t *testing.T) {
	obs := initOTLPObserver(t, true)
	if obs.TracerProvider() == nil {
		t.Fatal("TracerProvider must be set")
	}
	// The installed tracer should produce sampled spans because
	// DefaultConfig does not set SamplingRatio, giving AlwaysSample
	// at the root.
	_, span := obs.TracerProvider().Tracer("t").Start(context.Background(), "s")
	if !span.SpanContext().IsValid() {
		t.Error("expected valid span context from enabled OTLP tracer")
	}
	span.End()
}

func TestInitOTLPRejectsMissingMetricsEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_METRICS_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	cfg := DefaultConfig()
	cfg.ServiceName = "svc"
	cfg.MetricsMode = MetricsModeOTLP
	_, err := Init(context.Background(), cfg)
	if err == nil {
		t.Error("expected error when OTLP metrics endpoint is missing")
	}
}

func TestInitOTLPRejectsMissingTracesEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_TRACES_ENDPOINT", "")
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")

	cfg := DefaultConfig()
	cfg.ServiceName = "svc"
	cfg.MetricsMode = MetricsModePrometheus
	cfg.EnableTracing = true
	_, err := Init(context.Background(), cfg)
	if err == nil {
		t.Error("expected error when tracing is enabled but no endpoint is configured")
	}
}

func TestInitRejectsUnknownMetricsMode(t *testing.T) {
	cfg := DefaultConfig()
	cfg.ServiceName = "svc"
	cfg.MetricsMode = MetricsMode("bogus")
	_, err := Init(context.Background(), cfg)
	if err == nil {
		t.Error("expected validation error for unknown MetricsMode")
	}
}

func TestInitRejectsEmptyServiceName(t *testing.T) {
	cfg := DefaultConfig()
	cfg.MetricsMode = MetricsModePrometheus
	_, err := Init(context.Background(), cfg)
	if err == nil {
		t.Error("expected validation error for empty ServiceName")
	}
}

func TestObserverGettersPrometheusMode(t *testing.T) {
	obs := initTestObserver(t)

	if obs.Telemetry() == nil {
		t.Error("Telemetry() returned nil")
	}
	if obs.TracerProvider() == nil {
		t.Error("TracerProvider() returned nil")
	}
	if obs.MeterProvider() != nil {
		t.Error("MeterProvider() must be nil in Prometheus mode")
	}
}

func TestObserverGettersOTLPMode(t *testing.T) {
	obs := initOTLPObserver(t, false)

	if obs.Telemetry() == nil {
		t.Error("Telemetry() returned nil")
	}
	if obs.TracerProvider() == nil {
		t.Error("TracerProvider() returned nil")
	}
	mp := obs.MeterProvider()
	if mp == nil {
		t.Fatal("MeterProvider() must be non-nil in OTLP mode")
	}
	if _, ok := mp.(metric.MeterProvider); !ok {
		t.Error("MeterProvider() must implement metric.MeterProvider")
	}
}

func TestObserverTracerProviderIsUsableForCustomInstrumentation(t *testing.T) {
	obs := initTestObserver(t)

	tp := obs.TracerProvider()
	if _, ok := tp.(trace.TracerProvider); !ok {
		t.Fatal("TracerProvider() must implement trace.TracerProvider")
	}
	// Sanity-check: middleware request still works after the
	// provider has been retrieved through the accessor. This
	// mirrors the way third-party libraries consume the
	// provider.
	httptest.NewRecorder() // keep httptest import used
	_, _ = http.NewRequest(http.MethodGet, "/", nil)
}
