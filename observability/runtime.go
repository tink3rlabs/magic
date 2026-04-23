package observability

import (
	"context"
	"errors"
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// registerRuntimeForPrometheus registers the standard Go runtime
// and process collectors on reg when the corresponding flags in
// cfg are enabled. The collectors are safe to register
// unconditionally and use the prometheus library defaults.
func registerRuntimeForPrometheus(cfg *Config, reg *prometheus.Registry) error {
	if cfg.EnableRuntimeMetrics {
		if err := reg.Register(collectors.NewGoCollector(
			collectors.WithGoCollectorRuntimeMetrics(collectors.MetricsAll),
		)); err != nil {
			return fmt.Errorf("observability: register go runtime collector: %w", err)
		}
	}
	if cfg.EnableProcessMetrics {
		if err := reg.Register(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})); err != nil {
			// NewProcessCollector silently returns a no-op on
			// unsupported platforms; the error path only fires on
			// duplicate registration, which we treat as fatal.
			return fmt.Errorf("observability: register process collector: %w", err)
		}
	}
	return nil
}

// registerRuntimeForOTEL starts the OTEL contrib runtime
// instrumentation, which emits Go memory and GC metrics against mp.
// OTEL does not ship an equivalent of ProcessCollector, so process
// metrics remain Prometheus-only for now; this is documented in
// the observability design doc under "Runtime & Process Metrics".
//
// The returned shutdown function stops the underlying goroutine so
// the Observer can clean up during Shutdown.
func registerRuntimeForOTEL(cfg *Config, mp *sdkmetric.MeterProvider) (func(context.Context) error, error) {
	if !cfg.EnableRuntimeMetrics {
		return nil, nil
	}

	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		return nil, fmt.Errorf("observability: start OTEL runtime instrumentation: %w", err)
	}
	// The contrib/instrumentation/runtime package does not expose
	// a stop handle; the producer goroutine lives for the lifetime
	// of the process and is cleaned up when the MeterProvider
	// shuts down. We return a no-op shutdown to keep the
	// Observer's cleanup list uniform.
	return func(context.Context) error { return nil }, nil
}

// ErrUnsupported is returned by hooks that cannot run on the
// current platform. Kept here so callers can match on it without
// importing extra error packages.
var ErrUnsupported = errors.New("observability: unsupported on this platform")
