// Package telemetry provides the backend-neutral observability
// primitives that magic core packages (storage, pubsub, logger,
// and the HTTP middleware) depend on.
//
// The goal of this package is to decouple instrumentation from any
// particular metrics or tracing backend and to keep the core
// package import graph small: only OpenTelemetry's trace API,
// semantic conventions, and propagation packages are allowed
// transitively. Concrete backends (Prometheus, OTLP) are installed
// by the observability package during Init.
//
// Until observability.Init runs, a no-op Telemetry is in effect so
// that instrumented code always has a safe, non-nil target.
package telemetry

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

// Telemetry is the container for the observability primitives used
// by magic core packages. It is intentionally small; additional
// backends (loggers, profilers) can be added in future releases
// without breaking callers.
type Telemetry struct {
	// Metrics is the backend-neutral metrics factory. Never nil.
	Metrics MetricsBackend
	// Tracer is the OpenTelemetry tracer used to create spans.
	// Never nil; defaults to a no-op tracer.
	Tracer trace.Tracer
}

// NewNoop returns a Telemetry backed by no-op implementations.
// Safe to use as a zero-configuration default.
func NewNoop() *Telemetry {
	return &Telemetry{
		Metrics: noopMetricsBackend{},
		Tracer:  noop.NewTracerProvider().Tracer("github.com/tink3rlabs/magic/telemetry"),
	}
}

var global atomic.Pointer[Telemetry]

func init() {
	global.Store(NewNoop())
}

// Global returns the process-wide Telemetry. The returned value is
// always non-nil; before SetGlobal is called (typically by
// observability.Init) it is a no-op Telemetry.
func Global() *Telemetry {
	return global.Load()
}

// SetGlobal installs t as the process-wide Telemetry. Passing nil
// resets Global to a no-op Telemetry, which is the correct behavior
// during observability.Shutdown.
func SetGlobal(t *Telemetry) {
	if t == nil {
		global.Store(NewNoop())
		return
	}
	global.Store(t)
}

type ctxKey struct{}

// WithContext returns a context that carries t. FromContext will
// return t when asked about the returned context (or any derived
// context). Passing a nil t returns ctx unchanged.
func WithContext(ctx context.Context, t *Telemetry) context.Context {
	if t == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKey{}, t)
}

// FromContext returns the Telemetry attached to ctx via
// WithContext, or Global() if none is attached. The returned value
// is always non-nil.
func FromContext(ctx context.Context) *Telemetry {
	if ctx != nil {
		if t, ok := ctx.Value(ctxKey{}).(*Telemetry); ok && t != nil {
			return t
		}
	}
	return Global()
}
