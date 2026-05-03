package observability

import (
	"go.opentelemetry.io/otel/trace"

	"github.com/tink3rlabs/magic/telemetry"
)

// HTTPMiddlewareState is the immutable snapshot of built-in HTTP
// instruments and tracer wiring required by the observability HTTP
// middleware implementation in the middlewares package.
type HTTPMiddlewareState struct {
	Tracer trace.Tracer

	RequestsTotal    telemetry.Counter
	RequestDuration  telemetry.Histogram
	RequestSize      telemetry.Histogram
	ResponseSize     telemetry.Histogram
	RequestsInFlight telemetry.UpDownCounter
}

// HTTPMiddlewareState returns the built-in HTTP instrumentation
// state installed during Init. A nil return means observability
// has not been initialized or the observer is invalid.
func (o *Observer) HTTPMiddlewareState() *HTTPMiddlewareState {
	if o == nil || o.telem == nil {
		return nil
	}
	return &HTTPMiddlewareState{
		Tracer:           o.telem.Tracer,
		RequestsTotal:    o.httpRequestsTotal,
		RequestDuration:  o.httpRequestDuration,
		RequestSize:      o.httpRequestSize,
		ResponseSize:     o.httpResponseSize,
		RequestsInFlight: o.httpRequestsInFlight,
	}
}
