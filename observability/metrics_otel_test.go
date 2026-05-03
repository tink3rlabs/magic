package observability

import (
	"context"
	"strings"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/tink3rlabs/magic/telemetry"
)

// newOTELTestHarness builds an in-process OTEL MeterProvider backed
// by a ManualReader so tests can inspect recorded metrics without a
// live OTLP collector. Returns the backend under test plus a
// collect() helper that drains the reader into a ResourceMetrics.
func newOTELTestHarness(t *testing.T, strict bool) (*otelBackend, func() metricdata.ResourceMetrics) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = mp.Shutdown(context.Background()) })
	meter := mp.Meter("test")
	b := newOTELBackend(meter, !strict) // strict=false => allowUndeclared=true
	// The backend takes allowUndeclaredLabels. strict=true means
	// the backend must reject undeclared labels.
	b.allowUndeclaredLabels = !strict

	collect := func() metricdata.ResourceMetrics {
		var rm metricdata.ResourceMetrics
		if err := reader.Collect(context.Background(), &rm); err != nil {
			t.Fatalf("collect: %v", err)
		}
		return rm
	}
	return b, collect
}

func findMetric(rm metricdata.ResourceMetrics, name string) *metricdata.Metrics {
	for _, sm := range rm.ScopeMetrics {
		for i := range sm.Metrics {
			if sm.Metrics[i].Name == name {
				return &sm.Metrics[i]
			}
		}
	}
	return nil
}

func TestOTELBackendCounter(t *testing.T) {
	b, collect := newOTELTestHarness(t, true)

	c, err := b.Counter(telemetry.MetricDefinition{
		Name:   "orders_total",
		Help:   "orders",
		Kind:   telemetry.KindCounter,
		Labels: []string{"status"},
	})
	if err != nil {
		t.Fatalf("Counter: %v", err)
	}
	c.Add(2, telemetry.Label{Key: "status", Value: "ok"})
	c.Add(3, telemetry.Label{Key: "status", Value: "ok"})
	c.Add(1, telemetry.Label{Key: "status", Value: "fail"})

	m := findMetric(collect(), "orders_total")
	if m == nil {
		t.Fatal("orders_total not emitted")
		return
	}
	sum, ok := m.Data.(metricdata.Sum[float64])
	if !ok {
		t.Fatalf("expected Sum[float64], got %T", m.Data)
	}
	// Expect two datapoints (status=ok total 5, status=fail total 1).
	if len(sum.DataPoints) != 2 {
		t.Fatalf("want 2 datapoints, got %d", len(sum.DataPoints))
	}
	total := 0.0
	for _, dp := range sum.DataPoints {
		total += dp.Value
	}
	if total != 6 {
		t.Errorf("total = %v, want 6", total)
	}
}

func TestOTELBackendCounterNegativeSuppressed(t *testing.T) {
	b, collect := newOTELTestHarness(t, false)
	c, err := b.Counter(telemetry.MetricDefinition{
		Name: "c", Kind: telemetry.KindCounter,
	})
	if err != nil {
		t.Fatalf("Counter: %v", err)
	}
	c.Add(-1) // must be suppressed, no observation recorded
	c.Add(5)

	m := findMetric(collect(), "c")
	if m == nil {
		t.Fatal("no metric")
		return
	}
	sum := m.Data.(metricdata.Sum[float64])
	if sum.DataPoints[0].Value != 5 {
		t.Errorf("want 5, got %v", sum.DataPoints[0].Value)
	}
}

func TestOTELBackendHistogram(t *testing.T) {
	b, collect := newOTELTestHarness(t, true)

	h, err := b.Histogram(telemetry.MetricDefinition{
		Name:    "latency_seconds",
		Kind:    telemetry.KindHistogram,
		Unit:    telemetry.UnitSeconds,
		Buckets: []float64{0.1, 1, 10},
	})
	if err != nil {
		t.Fatalf("Histogram: %v", err)
	}
	h.Observe(0.05)
	h.Observe(0.5)
	h.Observe(5)

	m := findMetric(collect(), "latency_seconds")
	hist, ok := m.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("expected Histogram, got %T", m.Data)
	}
	if len(hist.DataPoints) != 1 {
		t.Fatalf("want 1 datapoint, got %d", len(hist.DataPoints))
	}
	dp := hist.DataPoints[0]
	if dp.Count != 3 {
		t.Errorf("count = %d, want 3", dp.Count)
	}
	// Buckets were honored — there should be exactly 4 counts (3+1).
	if len(dp.BucketCounts) != 4 {
		t.Errorf("bucket count entries = %d, want 4 (3 boundaries + overflow)", len(dp.BucketCounts))
	}
}

func TestOTELBackendGauge(t *testing.T) {
	b, collect := newOTELTestHarness(t, true)

	g, err := b.Gauge(telemetry.MetricDefinition{
		Name: "temp", Kind: telemetry.KindGauge, Labels: []string{"room"},
	})
	if err != nil {
		t.Fatalf("Gauge: %v", err)
	}
	g.Set(20, telemetry.Label{Key: "room", Value: "a"})
	g.Set(22, telemetry.Label{Key: "room", Value: "a"})

	m := findMetric(collect(), "temp")
	gauge, ok := m.Data.(metricdata.Gauge[float64])
	if !ok {
		t.Fatalf("expected Gauge, got %T", m.Data)
	}
	if len(gauge.DataPoints) != 1 || gauge.DataPoints[0].Value != 22 {
		t.Errorf("want 22, got %v", gauge.DataPoints)
	}
}

func TestOTELBackendUpDownCounter(t *testing.T) {
	b, collect := newOTELTestHarness(t, true)

	u, err := b.UpDownCounter(telemetry.MetricDefinition{
		Name: "queue", Kind: telemetry.KindUpDownCounter,
	})
	if err != nil {
		t.Fatalf("UpDownCounter: %v", err)
	}
	u.Add(5)
	u.Add(-2)

	m := findMetric(collect(), "queue")
	sum := m.Data.(metricdata.Sum[float64])
	if sum.DataPoints[0].Value != 3 {
		t.Errorf("want 3, got %v", sum.DataPoints[0].Value)
	}
}

func TestOTELBackendWrongKindRejected(t *testing.T) {
	b, _ := newOTELTestHarness(t, true)

	_, err := b.Counter(telemetry.MetricDefinition{Name: "x", Kind: telemetry.KindHistogram})
	if err == nil || !strings.Contains(err.Error(), "requires KindCounter") {
		t.Errorf("expected KindCounter error, got %v", err)
	}

	_, err = b.Histogram(telemetry.MetricDefinition{Name: "x", Kind: telemetry.KindCounter})
	if err == nil || !strings.Contains(err.Error(), "requires KindHistogram") {
		t.Errorf("expected KindHistogram error, got %v", err)
	}

	_, err = b.Gauge(telemetry.MetricDefinition{Name: "x", Kind: telemetry.KindCounter})
	if err == nil || !strings.Contains(err.Error(), "requires KindGauge") {
		t.Errorf("expected KindGauge error, got %v", err)
	}

	_, err = b.UpDownCounter(telemetry.MetricDefinition{Name: "x", Kind: telemetry.KindCounter})
	if err == nil || !strings.Contains(err.Error(), "requires KindUpDownCounter") {
		t.Errorf("expected KindUpDownCounter error, got %v", err)
	}
}

func TestOTELBackendInvalidNameRejected(t *testing.T) {
	b, _ := newOTELTestHarness(t, true)

	_, err := b.Counter(telemetry.MetricDefinition{Name: "", Kind: telemetry.KindCounter})
	if err == nil {
		t.Error("empty name must be rejected")
	}

	_, err = b.Counter(telemetry.MetricDefinition{Name: "bad-name", Kind: telemetry.KindCounter})
	if err == nil {
		t.Error("invalid name must be rejected")
	}
}

func TestOTELBackendDuplicateRegistrationReturnsSame(t *testing.T) {
	b, _ := newOTELTestHarness(t, true)

	def := telemetry.MetricDefinition{Name: "x", Kind: telemetry.KindCounter, Labels: []string{"a"}}
	c1, err := b.Counter(def)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	c2, err := b.Counter(def)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	// Same instance: the backend caches by name.
	if c1 != c2 {
		t.Error("expected duplicate registration to return the cached instrument")
	}
}

func TestOTELBackendShapeConflict(t *testing.T) {
	b, _ := newOTELTestHarness(t, true)

	_, err := b.Counter(telemetry.MetricDefinition{Name: "x", Kind: telemetry.KindCounter, Labels: []string{"a"}})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	_, err = b.Counter(telemetry.MetricDefinition{Name: "x", Kind: telemetry.KindCounter, Labels: []string{"a", "b"}})
	if err == nil {
		t.Error("expected shape conflict error on re-registration with different labels")
	}
}

func TestOTELAttributesStrictRejectsUndeclared(t *testing.T) {
	b, collect := newOTELTestHarness(t, true) // strict mode

	c, err := b.Counter(telemetry.MetricDefinition{
		Name: "x", Kind: telemetry.KindCounter, Labels: []string{"a"},
	})
	if err != nil {
		t.Fatalf("Counter: %v", err)
	}
	// Undeclared label "b" must cause the observation to be
	// dropped, leaving the counter at zero.
	c.Add(1, telemetry.Label{Key: "b", Value: "x"})

	rm := collect()
	if m := findMetric(rm, "x"); m != nil {
		// The metric instrument exists but should have no
		// datapoints.
		sum, ok := m.Data.(metricdata.Sum[float64])
		if ok && len(sum.DataPoints) > 0 {
			t.Errorf("expected 0 datapoints after strict rejection, got %d", len(sum.DataPoints))
		}
	}
}

func TestOTELAttributesRelaxedKeepsUndeclared(t *testing.T) {
	b, collect := newOTELTestHarness(t, false) // relaxed

	c, err := b.Counter(telemetry.MetricDefinition{
		Name: "x", Kind: telemetry.KindCounter,
	})
	if err != nil {
		t.Fatalf("Counter: %v", err)
	}
	c.Add(1, telemetry.Label{Key: "extra", Value: "v"})

	m := findMetric(collect(), "x")
	sum := m.Data.(metricdata.Sum[float64])
	if len(sum.DataPoints) != 1 || sum.DataPoints[0].Value != 1 {
		t.Errorf("want 1 datapoint with value 1, got %v", sum.DataPoints)
	}
}

func TestOTELAttributesNoLabelsStrictRejectsInput(t *testing.T) {
	b, collect := newOTELTestHarness(t, true)

	h, err := b.Histogram(telemetry.MetricDefinition{
		Name: "h", Kind: telemetry.KindHistogram,
	})
	if err != nil {
		t.Fatalf("Histogram: %v", err)
	}
	// Metric declares no labels; supplying one must drop the
	// observation under strict mode.
	h.Observe(1, telemetry.Label{Key: "any", Value: "x"})

	if m := findMetric(collect(), "h"); m != nil {
		hist := m.Data.(metricdata.Histogram[float64])
		if len(hist.DataPoints) > 0 {
			t.Errorf("expected no datapoints after strict rejection, got %d", len(hist.DataPoints))
		}
	}
}
