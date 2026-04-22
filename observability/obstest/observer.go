package obstest

import (
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/tink3rlabs/magic/telemetry"
)

// TestObserver bundles an in-memory metrics backend and an OTEL
// SpanRecorder behind a single handle. It installs itself as the
// process-wide telemetry.Global so instrumented code written
// against the telemetry package is automatically observable by
// tests.
//
// Typical usage:
//
//	obs := obstest.NewTestObserver(t)
//	myHandler.ServeHTTP(w, r) // uses telemetry.FromContext(...)
//	if got := obs.Metrics.CounterValue("http_requests_total"); got != 1 {
//	    t.Errorf("want 1 request, got %v", got)
//	}
type TestObserver struct {
	// Metrics is the in-memory recorder for all metric
	// observations.
	Metrics *MemoryBackend
	// Spans records every span created via Telemetry.Tracer.
	Spans *tracetest.SpanRecorder
	// Telemetry is the installed Telemetry value.
	Telemetry *telemetry.Telemetry

	restore *telemetry.Telemetry
}

// NewTestObserver creates a TestObserver and installs it on the
// telemetry global. If tb is non-nil, TestObserver.Close is
// registered with tb.Cleanup so tests do not have to remember to
// call it explicitly.
//
// The tb parameter is declared as an interface matching
// *testing.T (and *testing.B) so obstest can be used from
// benchmarks without pulling in the testing package at import
// time.
func NewTestObserver(tb interface{ Cleanup(func()) }) *TestObserver {
	mem := NewMemoryBackend()
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(recorder),
	)

	telem := &telemetry.Telemetry{
		Metrics: mem,
		Tracer:  tp.Tracer("github.com/tink3rlabs/magic/obstest"),
	}
	prev := telemetry.Global()
	telemetry.SetGlobal(telem)

	o := &TestObserver{
		Metrics:   mem,
		Spans:     recorder,
		Telemetry: telem,
		restore:   prev,
	}
	if tb != nil {
		tb.Cleanup(o.Close)
	}
	return o
}

// Close restores the previous telemetry global. It is safe to
// call multiple times.
func (o *TestObserver) Close() {
	if o == nil {
		return
	}
	telemetry.SetGlobal(o.restore)
	o.restore = nil
}
