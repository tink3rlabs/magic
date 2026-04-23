// Package observability wires up tracing, metrics, and logger
// correlation for services built on the magic library. A single
// call to Init during process bootstrap selects a metrics backend
// (Prometheus scrape or OTLP push), installs an OTEL TracerProvider
// (or a no-op when tracing is disabled), registers built-in HTTP
// and runtime metrics, and publishes the resulting backends to the
// neutral telemetry package that magic core packages consume.
//
// Typical usage:
//
//	cfg := observability.DefaultConfig()
//	cfg.ServiceName = "my-service"
//	cfg.MetricsMode = observability.MetricsModePrometheus
//	obs, err := observability.Init(ctx, cfg)
//	if err != nil { ... }
//	defer obs.Shutdown(context.Background())
//
//	router := chi.NewRouter()
//	router.Use(middlewares.ObservabilityWithOptions(obs, middlewares.ObservabilityOptions{
//	  SkipPaths:        []string{"/metrics"},
//	  SkipPathPrefixes: []string{"/health/"},
//	}))
//	router.Handle("/metrics", obs.MetricsHandler())
package observability

import (
	"context"
	"fmt"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/tink3rlabs/magic/telemetry"
)

// Observer is the live observability handle returned by Init. It
// owns the tracer provider, meter provider (OTLP mode) or
// Prometheus registry, the built-in HTTP instruments, and any
// custom instruments registered through the Counter/Histogram/...
// helpers.
//
// An Observer must be shut down exactly once, after the process
// stops serving traffic, to flush pending spans and metrics.
type Observer struct {
	cfg Config

	// Metric backend state. Exactly one of promRegistry /
	// meterProvider is non-nil depending on cfg.MetricsMode.
	promRegistry  *prometheus.Registry
	meterProvider *sdkmetric.MeterProvider

	tracerProvider trace.TracerProvider

	// telem is the installed Telemetry value; its Metrics field
	// is either a prometheusBackend or otelBackend, and its
	// Tracer is created from tracerProvider.
	telem *telemetry.Telemetry

	// Built-in HTTP instruments, wired by registerHTTPMetrics
	// and consumed by middlewares.Observability.
	httpRequestsTotal    telemetry.Counter
	httpRequestDuration  telemetry.Histogram
	httpRequestSize      telemetry.Histogram
	httpResponseSize     telemetry.Histogram
	httpRequestsInFlight telemetry.UpDownCounter

	// Built-in storage instruments, wired by registerStorageMetrics
	// and consumed by the storage instrumented wrapper through
	// telemetry.Global().Metrics (same backend, resolved by name).
	storageOpsTotal   telemetry.Counter
	storageOpDuration telemetry.Histogram
	storageOpErrors   telemetry.Counter

	// Built-in pubsub instruments, wired by registerPubSubMetrics
	// and consumed by the pubsub instrumented publisher wrapper
	// through telemetry.Global().Metrics.
	pubsubMessagesTotal   telemetry.Counter
	pubsubPublishDuration telemetry.Histogram
	pubsubErrorsTotal     telemetry.Counter

	// Shutdown fns, called in LIFO order.
	mu          sync.Mutex
	shutdownFns []func(context.Context) error
	shutdownDone bool
}

// Init configures and installs the observability stack. It
// validates cfg, builds the metrics backend and tracer, registers
// built-in instruments, and publishes the result to
// telemetry.SetGlobal so downstream packages pick it up.
//
// The returned Observer owns process-wide resources and must be
// shut down before exit.
func Init(ctx context.Context, cfg Config) (*Observer, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}

	res, err := buildResource(ctx, cfg)
	if err != nil {
		return nil, err
	}

	obs := &Observer{cfg: cfg}

	tracerProvider, tpShutdown, err := setupTracer(ctx, cfg, res)
	if err != nil {
		return nil, err
	}
	obs.tracerProvider = tracerProvider
	obs.shutdownFns = append(obs.shutdownFns, tpShutdown)

	var metricsBackend telemetry.MetricsBackend
	switch cfg.MetricsMode {
	case MetricsModePrometheus:
		obs.promRegistry = prometheus.NewRegistry()
		if err := registerRuntimeForPrometheus(&cfg, obs.promRegistry); err != nil {
			_ = obs.runShutdown(ctx)
			return nil, err
		}
		metricsBackend = newPrometheusBackend(obs.promRegistry, cfg.AllowUndeclaredLabels)

	case MetricsModeOTLP:
		mp, mpShutdown, err := setupMeterProvider(ctx, cfg, res)
		if err != nil {
			_ = obs.runShutdown(ctx)
			return nil, err
		}
		obs.meterProvider = mp
		obs.shutdownFns = append(obs.shutdownFns, mpShutdown)
		meter := mp.Meter("github.com/tink3rlabs/magic")
		metricsBackend = newOTELBackend(meter, cfg.AllowUndeclaredLabels)

		if runtimeShutdown, err := registerRuntimeForOTEL(&cfg, mp); err != nil {
			_ = obs.runShutdown(ctx)
			return nil, err
		} else if runtimeShutdown != nil {
			obs.shutdownFns = append(obs.shutdownFns, runtimeShutdown)
		}

	default:
		return nil, fmt.Errorf("observability: unsupported MetricsMode %q", cfg.MetricsMode)
	}

	obs.telem = &telemetry.Telemetry{
		Metrics: metricsBackend,
		Tracer:  tracerProvider.Tracer("github.com/tink3rlabs/magic"),
	}

	if err := obs.registerHTTPMetrics(); err != nil {
		_ = obs.runShutdown(ctx)
		return nil, err
	}
	if err := obs.registerStorageMetrics(); err != nil {
		_ = obs.runShutdown(ctx)
		return nil, err
	}
	if err := obs.registerPubSubMetrics(); err != nil {
		_ = obs.runShutdown(ctx)
		return nil, err
	}

	telemetry.SetGlobal(obs.telem)
	obs.shutdownFns = append(obs.shutdownFns, func(context.Context) error {
		telemetry.SetGlobal(nil)
		return nil
	})

	return obs, nil
}

// Shutdown flushes pending telemetry and releases resources. Safe
// to call multiple times; subsequent calls are no-ops. Errors from
// individual shutdown steps are joined and returned together so
// callers see the full picture in logs.
func (o *Observer) Shutdown(ctx context.Context) error {
	return o.runShutdown(ctx)
}

func (o *Observer) runShutdown(ctx context.Context) error {
	o.mu.Lock()
	if o.shutdownDone {
		o.mu.Unlock()
		return nil
	}
	o.shutdownDone = true
	fns := o.shutdownFns
	o.shutdownFns = nil
	o.mu.Unlock()

	var errs []error
	for i := len(fns) - 1; i >= 0; i-- {
		if err := fns[i](ctx); err != nil {
			errs = append(errs, err)
		}
	}
	if len(errs) == 0 {
		return nil
	}
	if len(errs) == 1 {
		return errs[0]
	}
	msg := "observability: shutdown encountered errors:"
	for _, e := range errs {
		msg += "\n  - " + e.Error()
	}
	return fmt.Errorf("%s", msg)
}

// Telemetry returns the Telemetry installed globally by Init.
// Most code should use telemetry.Global() or
// telemetry.FromContext() instead; this accessor is primarily
// useful for tests and advanced wiring scenarios.
func (o *Observer) Telemetry() *telemetry.Telemetry { return o.telem }

// TracerProvider returns the OTEL TracerProvider used by the
// installed Telemetry. Callers that need to obtain tracers for
// non-magic packages (for example, third-party libraries
// instrumented with OTEL) can use this handle.
func (o *Observer) TracerProvider() trace.TracerProvider { return o.tracerProvider }

// MeterProvider returns the OTEL MeterProvider when MetricsMode
// is MetricsModeOTLP. In Prometheus mode it returns nil because
// the Prometheus backend does not use an OTEL MeterProvider;
// callers should use MetricsHandler() for exposition instead.
func (o *Observer) MeterProvider() metric.MeterProvider {
	if o.meterProvider == nil {
		return nil
	}
	return o.meterProvider
}
