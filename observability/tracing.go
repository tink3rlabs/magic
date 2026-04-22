package observability

import (
	"context"
	"fmt"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

// resolveOTLPEndpoint returns the OTLP endpoint for the given
// signal. It prefers an explicitly-configured endpoint; otherwise
// it falls back to the signal-specific environment variable and
// finally the generic OTEL_EXPORTER_OTLP_ENDPOINT.
func resolveOTLPEndpoint(explicit, signalEnv string) string {
	if explicit != "" {
		return explicit
	}
	if v := os.Getenv(signalEnv); v != "" {
		return v
	}
	return os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
}

// setupTracer builds and installs the OTEL TracerProvider based on
// cfg. It returns the constructed provider and a shutdown function
// that must be invoked before process exit. When tracing is
// disabled (cfg.EnableTracing false) a no-op TracerProvider is
// installed and the shutdown function is a no-op.
func setupTracer(ctx context.Context, cfg Config, res *resource.Resource) (trace.TracerProvider, func(context.Context) error, error) {
	propagator := cfg.Propagator
	if propagator == nil {
		propagator = propagation.NewCompositeTextMapPropagator(
			propagation.TraceContext{},
			propagation.Baggage{},
		)
	}
	otel.SetTextMapPropagator(propagator)

	if !cfg.EnableTracing {
		noopTP := tracenoop.NewTracerProvider()
		otel.SetTracerProvider(noopTP)
		return noopTP, func(context.Context) error { return nil }, nil
	}

	endpoint := resolveOTLPEndpoint(cfg.TracesOTLPEndpoint, "OTEL_EXPORTER_OTLP_TRACES_ENDPOINT")
	if endpoint == "" {
		return nil, nil, fmt.Errorf("observability: EnableTracing=true but no OTLP traces endpoint configured (set TracesOTLPEndpoint or OTEL_EXPORTER_OTLP_[TRACES_]ENDPOINT)")
	}

	clientOpts := []otlptracegrpc.Option{
		otlptracegrpc.WithEndpoint(endpoint),
	}
	if cfg.TracesOTLPInsecure {
		clientOpts = append(clientOpts, otlptracegrpc.WithInsecure())
	}

	exp, err := otlptrace.New(ctx, otlptracegrpc.NewClient(clientOpts...))
	if err != nil {
		return nil, nil, fmt.Errorf("observability: create OTLP trace exporter: %w", err)
	}

	sampler := cfg.Sampler
	if sampler == nil {
		sampler = resolveSampler(cfg.SamplingRatio)
	}

	provider := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sampler)),
	)
	otel.SetTracerProvider(provider)

	return provider, provider.Shutdown, nil
}

// resolveSampler produces a root sampler based on the ratio.
// nil pointer -> AlwaysSample (OTEL default for roots)
// 0.0         -> NeverSample
// 1.0         -> AlwaysSample
// (0,1)       -> TraceIDRatioBased
func resolveSampler(ratio *float64) sdktrace.Sampler {
	if ratio == nil {
		return sdktrace.AlwaysSample()
	}
	switch {
	case *ratio <= 0:
		return sdktrace.NeverSample()
	case *ratio >= 1:
		return sdktrace.AlwaysSample()
	default:
		return sdktrace.TraceIDRatioBased(*ratio)
	}
}
