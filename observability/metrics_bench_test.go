package observability

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/tink3rlabs/magic/telemetry"
)

// benchCounter returns a Prometheus-backed Counter registered on
// a fresh registry so benchmarks do not depend on cross-test
// metric state.
func benchCounter(b *testing.B, labels []string) telemetry.Counter {
	b.Helper()
	back := newPrometheusBackend(prometheus.NewRegistry(), false)
	c, err := back.Counter(telemetry.MetricDefinition{
		Name:   "bench_counter_total",
		Kind:   telemetry.KindCounter,
		Labels: labels,
	})
	if err != nil {
		b.Fatalf("Counter: %v", err)
	}
	return c
}

// BenchmarkPrometheusCounterAddNoLabels measures the absolute
// floor for a metric record: no label projection, no vector
// lookup, just a lock-free Prometheus counter increment.
func BenchmarkPrometheusCounterAddNoLabels(b *testing.B) {
	c := benchCounter(b, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Add(1)
	}
}

// BenchmarkPrometheusCounterAddThreeLabels exercises the
// projectLabels + WithLabelValues hot path, which is what every
// built-in storage/pubsub/http observation pays per call.
func BenchmarkPrometheusCounterAddThreeLabels(b *testing.B) {
	c := benchCounter(b, []string{"provider", "operation", "status"})
	providerL := telemetry.Label{Key: "provider", Value: "sqlite"}
	opL := telemetry.Label{Key: "operation", Value: "get"}
	statusL := telemetry.Label{Key: "status", Value: "ok"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Add(1, providerL, opL, statusL)
	}
}

// BenchmarkPrometheusHistogramObserveThreeLabels is the histogram
// equivalent. Histograms allocate the per-label-values series on
// first observation, then reuse it, so this benchmark measures
// steady-state cost.
func BenchmarkPrometheusHistogramObserveThreeLabels(b *testing.B) {
	back := newPrometheusBackend(prometheus.NewRegistry(), false)
	h, err := back.Histogram(telemetry.MetricDefinition{
		Name:    "bench_duration_seconds",
		Kind:    telemetry.KindHistogram,
		Labels:  []string{"provider", "operation", "status"},
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10},
	})
	if err != nil {
		b.Fatalf("Histogram: %v", err)
	}
	providerL := telemetry.Label{Key: "provider", Value: "sqlite"}
	opL := telemetry.Label{Key: "operation", Value: "get"}
	statusL := telemetry.Label{Key: "status", Value: "ok"}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		h.Observe(0.0012, providerL, opL, statusL)
	}
}

// BenchmarkNoopCounterAdd measures the cost paid by instrumented
// code when observability has NOT been initialized (the default
// for CLI tools or any service that doesn't call Init). Should
// be essentially free — this is the baseline we charge any
// consumer that never opts in.
func BenchmarkNoopCounterAdd(b *testing.B) {
	t := telemetry.NewNoop()
	c, err := t.Metrics.Counter(telemetry.MetricDefinition{
		Name: "bench_noop_total",
		Kind: telemetry.KindCounter,
	})
	if err != nil {
		b.Fatalf("Counter: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		c.Add(1, telemetry.Label{Key: "x", Value: "y"})
	}
}
