package observability

import (
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/tink3rlabs/magic/telemetry"
)

func TestPromBackendCounterNegativeSuppressed(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newPrometheusBackend(reg, false)

	c, err := b.Counter(telemetry.MetricDefinition{
		Name: "neg_counter", Kind: telemetry.KindCounter,
	})
	if err != nil {
		t.Fatalf("Counter: %v", err)
	}
	c.Add(-1) // must be suppressed
	c.Add(3)

	mf, _ := reg.Gather()
	for _, fam := range mf {
		if fam.GetName() != "neg_counter" {
			continue
		}
		got := fam.GetMetric()[0].GetCounter().GetValue()
		if got != 3 {
			t.Errorf("value = %v, want 3", got)
		}
		return
	}
	t.Error("neg_counter family not found")
}

func TestPromBackendGauge(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newPrometheusBackend(reg, false)

	g, err := b.Gauge(telemetry.MetricDefinition{
		Name: "room_temp", Kind: telemetry.KindGauge, Labels: []string{"room"},
	})
	if err != nil {
		t.Fatalf("Gauge: %v", err)
	}
	g.Set(21, telemetry.Label{Key: "room", Value: "a"})
	g.Set(22, telemetry.Label{Key: "room", Value: "a"})
	g.Set(10, telemetry.Label{Key: "room", Value: "b"})

	mf, _ := reg.Gather()
	found := 0
	for _, fam := range mf {
		if fam.GetName() != "room_temp" {
			continue
		}
		for _, m := range fam.GetMetric() {
			found++
			for _, l := range m.GetLabel() {
				if l.GetName() == "room" && l.GetValue() == "a" {
					if v := m.GetGauge().GetValue(); v != 22 {
						t.Errorf("room=a gauge = %v, want 22 (latest Set wins)", v)
					}
				}
			}
		}
	}
	if found != 2 {
		t.Errorf("want 2 series, got %d", found)
	}
}

func TestPromBackendGaugeStrictLabelDrop(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newPrometheusBackend(reg, false) // strict

	g, err := b.Gauge(telemetry.MetricDefinition{
		Name: "g_strict", Kind: telemetry.KindGauge, Labels: []string{"k"},
	})
	if err != nil {
		t.Fatalf("Gauge: %v", err)
	}
	g.Set(1, telemetry.Label{Key: "unexpected", Value: "v"}) // dropped

	mf, _ := reg.Gather()
	for _, fam := range mf {
		if fam.GetName() == "g_strict" && len(fam.GetMetric()) > 0 {
			t.Errorf("strict gauge should have no series after undeclared label, got %d", len(fam.GetMetric()))
		}
	}
}

func TestPromBackendHistogramStrictLabelDrop(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newPrometheusBackend(reg, false) // strict

	h, err := b.Histogram(telemetry.MetricDefinition{
		Name: "h_strict", Kind: telemetry.KindHistogram, Labels: []string{"a"},
	})
	if err != nil {
		t.Fatalf("Histogram: %v", err)
	}
	h.Observe(1, telemetry.Label{Key: "b", Value: "x"}) // dropped

	mf, _ := reg.Gather()
	for _, fam := range mf {
		if fam.GetName() == "h_strict" && len(fam.GetMetric()) > 0 {
			t.Errorf("strict histogram should have no series after undeclared label")
		}
	}
}

func TestPromBackendUpDownStrictLabelDrop(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newPrometheusBackend(reg, false)

	u, err := b.UpDownCounter(telemetry.MetricDefinition{
		Name: "u_strict", Kind: telemetry.KindUpDownCounter, Labels: []string{"a"},
	})
	if err != nil {
		t.Fatalf("UpDownCounter: %v", err)
	}
	u.Add(1, telemetry.Label{Key: "z", Value: "v"})

	mf, _ := reg.Gather()
	for _, fam := range mf {
		if fam.GetName() == "u_strict" && len(fam.GetMetric()) > 0 {
			t.Errorf("strict up/down should have no series after undeclared label")
		}
	}
}

func TestPromBackendInvalidNameRejected(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newPrometheusBackend(reg, false)

	if _, err := b.Counter(telemetry.MetricDefinition{Name: "", Kind: telemetry.KindCounter}); err == nil {
		t.Error("empty name must be rejected")
	}
	if _, err := b.Counter(telemetry.MetricDefinition{Name: "bad-name", Kind: telemetry.KindCounter}); err == nil {
		t.Error("invalid name must be rejected")
	}
}

func TestPromBackendWrongKindRejected(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newPrometheusBackend(reg, false)

	if _, err := b.Counter(telemetry.MetricDefinition{Name: "x", Kind: telemetry.KindHistogram}); err == nil ||
		!strings.Contains(err.Error(), "KindCounter") {
		t.Errorf("expected KindCounter error, got %v", err)
	}
	if _, err := b.Histogram(telemetry.MetricDefinition{Name: "x", Kind: telemetry.KindCounter}); err == nil ||
		!strings.Contains(err.Error(), "KindHistogram") {
		t.Errorf("expected KindHistogram error, got %v", err)
	}
	if _, err := b.Gauge(telemetry.MetricDefinition{Name: "x", Kind: telemetry.KindCounter}); err == nil ||
		!strings.Contains(err.Error(), "KindGauge") {
		t.Errorf("expected KindGauge error, got %v", err)
	}
	if _, err := b.UpDownCounter(telemetry.MetricDefinition{Name: "x", Kind: telemetry.KindCounter}); err == nil ||
		!strings.Contains(err.Error(), "KindUpDownCounter") {
		t.Errorf("expected KindUpDownCounter error, got %v", err)
	}
}

func TestPromBackendDuplicateRegistrationReturnsCached(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newPrometheusBackend(reg, false)

	def := telemetry.MetricDefinition{Name: "dup", Kind: telemetry.KindCounter, Labels: []string{"k"}}
	c1, err := b.Counter(def)
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	c2, err := b.Counter(def)
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if c1 != c2 {
		t.Error("duplicate registration should return cached instrument")
	}
}

func TestPromBackendAlreadyRegisteredRewraps(t *testing.T) {
	reg := prometheus.NewRegistry()
	b := newPrometheusBackend(reg, false)

	// Pre-register a CounterVec directly on the registry with
	// the same name and label set. The backend's first
	// registration should observe AlreadyRegisteredError and
	// rewrap the existing collector.
	existing := prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "rewrapped_total",
		Help: "pre-existing",
	}, []string{"k"})
	if err := reg.Register(existing); err != nil {
		t.Fatalf("pre-register: %v", err)
	}
	// Bump the pre-registered counter so we can verify the
	// rewrapped instrument shares state with it.
	existing.WithLabelValues("v").Add(7)

	c, err := b.Counter(telemetry.MetricDefinition{
		Name: "rewrapped_total", Help: "pre-existing", Kind: telemetry.KindCounter,
		Labels: []string{"k"},
	})
	if err != nil {
		t.Fatalf("Counter: %v", err)
	}
	c.Add(3, telemetry.Label{Key: "k", Value: "v"})

	mf, _ := reg.Gather()
	var total float64
	for _, fam := range mf {
		if fam.GetName() != "rewrapped_total" {
			continue
		}
		for _, m := range fam.GetMetric() {
			total += m.GetCounter().GetValue()
		}
	}
	if total != 10 {
		t.Errorf("want combined counter of 10 (7 pre + 3 via backend), got %v", total)
	}
}

func TestRewrapPromInstrumentRejectsMismatchedKind(t *testing.T) {
	existing := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "g"}, []string{"k"})

	got := rewrapPromInstrument(telemetry.MetricDefinition{
		Name: "g", Kind: telemetry.KindCounter, Labels: []string{"k"},
	}, existing)
	if got != nil {
		t.Error("rewrapping a gauge as a counter must return nil")
	}
}

func TestRewrapPromInstrumentCoversAllKinds(t *testing.T) {
	// Counter
	cv := prometheus.NewCounterVec(prometheus.CounterOpts{Name: "c"}, []string{"k"})
	if got := rewrapPromInstrument(telemetry.MetricDefinition{
		Name: "c", Kind: telemetry.KindCounter, Labels: []string{"k"},
	}, cv); got == nil {
		t.Error("counter rewrap returned nil")
	}
	// Histogram
	hv := prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: "h"}, []string{"k"})
	if got := rewrapPromInstrument(telemetry.MetricDefinition{
		Name: "h", Kind: telemetry.KindHistogram, Labels: []string{"k"},
	}, hv); got == nil {
		t.Error("histogram rewrap returned nil")
	}
	// Gauge
	gv := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "g"}, []string{"k"})
	if got := rewrapPromInstrument(telemetry.MetricDefinition{
		Name: "g", Kind: telemetry.KindGauge, Labels: []string{"k"},
	}, gv); got == nil {
		t.Error("gauge rewrap returned nil")
	}
	// UpDownCounter (Prometheus models with GaugeVec)
	uv := prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "u"}, []string{"k"})
	if got := rewrapPromInstrument(telemetry.MetricDefinition{
		Name: "u", Kind: telemetry.KindUpDownCounter, Labels: []string{"k"},
	}, uv); got == nil {
		t.Error("up/down rewrap returned nil")
	}
}

func TestDefinitionsEquivalent(t *testing.T) {
	a := telemetry.MetricDefinition{
		Name: "x", Kind: telemetry.KindHistogram,
		Labels:  []string{"a", "b"},
		Buckets: []float64{1, 2, 3},
	}
	b := a
	if !definitionsEquivalent(a, b) {
		t.Error("identical defs must be equivalent")
	}

	b = a
	b.Labels = []string{"a"}
	if definitionsEquivalent(a, b) {
		t.Error("different label count should not be equivalent")
	}

	b = a
	b.Labels = []string{"a", "c"}
	if definitionsEquivalent(a, b) {
		t.Error("different label keys should not be equivalent")
	}

	b = a
	b.Kind = telemetry.KindCounter
	if definitionsEquivalent(a, b) {
		t.Error("different kind should not be equivalent")
	}

	b = a
	b.Unit = "s"
	if definitionsEquivalent(a, b) {
		t.Error("different unit should not be equivalent")
	}

	b = a
	b.Buckets = []float64{1, 2}
	if definitionsEquivalent(a, b) {
		t.Error("different histogram buckets should not be equivalent")
	}
}

func TestStringSliceEqual(t *testing.T) {
	if !stringSliceEqual(nil, nil) {
		t.Error("nil slices should be equal")
	}
	if !stringSliceEqual([]string{}, []string{}) {
		t.Error("empty slices should be equal")
	}
	if stringSliceEqual([]string{"a"}, []string{"a", "b"}) {
		t.Error("different lengths should not be equal")
	}
	if stringSliceEqual([]string{"a", "b"}, []string{"a", "c"}) {
		t.Error("different contents should not be equal")
	}
	if !stringSliceEqual([]string{"a", "b"}, []string{"a", "b"}) {
		t.Error("identical should be equal")
	}
}
