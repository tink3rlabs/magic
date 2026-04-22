package obstest

import (
	"strings"
	"testing"

	"github.com/tink3rlabs/magic/telemetry"
)

func TestMemoryBackendCounterAccumulatesAndSuppressesNegative(t *testing.T) {
	b := NewMemoryBackend()
	c, _ := b.Counter(telemetry.MetricDefinition{Name: "c", Kind: telemetry.KindCounter})

	c.Add(3)
	c.Add(-5) // must be suppressed
	c.Add(2)

	if got := b.CounterValue("c"); got != 5 {
		t.Errorf("CounterValue = %v, want 5 (negative suppressed)", got)
	}
}

func TestMemoryBackendCounterPerLabelSet(t *testing.T) {
	b := NewMemoryBackend()
	c, _ := b.Counter(telemetry.MetricDefinition{Name: "c", Kind: telemetry.KindCounter})

	c.Add(1, telemetry.Label{Key: "a", Value: "x"})
	c.Add(4, telemetry.Label{Key: "a", Value: "y"})
	// Different caller ordering of the same label set should key into the same bucket.
	c.Add(2, telemetry.Label{Key: "a", Value: "x"})

	if got := b.CounterValue("c", telemetry.Label{Key: "a", Value: "x"}); got != 3 {
		t.Errorf("x = %v, want 3", got)
	}
	if got := b.CounterValue("c", telemetry.Label{Key: "a", Value: "y"}); got != 4 {
		t.Errorf("y = %v, want 4", got)
	}
	if got := b.CounterValue("c", telemetry.Label{Key: "missing", Value: "z"}); got != 0 {
		t.Errorf("unknown label set should be 0, got %v", got)
	}
}

func TestMemoryBackendHistogramObservations(t *testing.T) {
	b := NewMemoryBackend()
	h, _ := b.Histogram(telemetry.MetricDefinition{Name: "h", Kind: telemetry.KindHistogram})

	h.Observe(0.1)
	h.Observe(0.2)
	h.Observe(0.3)

	obs := b.HistogramObservations("h")
	if len(obs) != 3 {
		t.Fatalf("want 3 observations, got %d", len(obs))
	}
	for i, want := range []float64{0.1, 0.2, 0.3} {
		if obs[i] != want {
			t.Errorf("obs[%d] = %v, want %v", i, obs[i], want)
		}
	}

	// Mutating the returned slice must not affect the backend.
	obs[0] = 999
	if again := b.HistogramObservations("h"); again[0] == 999 {
		t.Error("returned slice must be a copy")
	}

	if got := b.HistogramCount("h"); got != 3 {
		t.Errorf("count = %d, want 3", got)
	}
	if got := b.HistogramSum("h"); got < 0.59 || got > 0.61 {
		t.Errorf("sum = %v, want ~0.6", got)
	}

	if got := b.HistogramObservations("missing"); len(got) != 0 {
		t.Errorf("missing histogram must return empty slice, got %v", got)
	}
}

func TestMemoryBackendGaugeAndUpDownStandalone(t *testing.T) {
	b := NewMemoryBackend()
	g, _ := b.Gauge(telemetry.MetricDefinition{Name: "g", Kind: telemetry.KindGauge})
	u, _ := b.UpDownCounter(telemetry.MetricDefinition{Name: "u", Kind: telemetry.KindUpDownCounter})

	g.Set(10)
	g.Set(7) // latest wins
	u.Add(5)
	u.Add(-2)

	if got := b.GaugeValue("g"); got != 7 {
		t.Errorf("gauge = %v, want 7", got)
	}
	if got := b.UpDownValue("u"); got != 3 {
		t.Errorf("updown = %v, want 3", got)
	}
}

func TestMemoryBackendDefinitionsRememberedAndCopied(t *testing.T) {
	b := NewMemoryBackend()
	_, _ = b.Counter(telemetry.MetricDefinition{Name: "c", Kind: telemetry.KindCounter, Help: "desc"})
	_, _ = b.Histogram(telemetry.MetricDefinition{Name: "h", Kind: telemetry.KindHistogram})

	defs := b.Definitions()
	if len(defs) != 2 {
		t.Errorf("want 2 definitions, got %d", len(defs))
	}
	if defs["c"].Help != "desc" {
		t.Error("Help field lost")
	}

	// Returned map must be a copy — mutations should not leak.
	defs["c"] = telemetry.MetricDefinition{Name: "tampered"}
	if b.Definitions()["c"].Name == "tampered" {
		t.Error("Definitions must return a copy")
	}
}

func TestMemoryBackendResetClearsObservationsKeepsDefs(t *testing.T) {
	b := NewMemoryBackend()
	c, _ := b.Counter(telemetry.MetricDefinition{Name: "c", Kind: telemetry.KindCounter})
	c.Add(2)

	if b.CounterValue("c") == 0 {
		t.Fatal("setup failed")
	}
	b.Reset()
	if b.CounterValue("c") != 0 {
		t.Error("Reset must clear counter observations")
	}
	if _, ok := b.Definitions()["c"]; !ok {
		t.Error("Reset must preserve definitions")
	}
}

func TestMemoryBackendString(t *testing.T) {
	b := NewMemoryBackend()
	c, _ := b.Counter(telemetry.MetricDefinition{Name: "c", Kind: telemetry.KindCounter})
	h, _ := b.Histogram(telemetry.MetricDefinition{Name: "h", Kind: telemetry.KindHistogram})
	g, _ := b.Gauge(telemetry.MetricDefinition{Name: "g", Kind: telemetry.KindGauge})
	u, _ := b.UpDownCounter(telemetry.MetricDefinition{Name: "u", Kind: telemetry.KindUpDownCounter})

	c.Add(1)
	h.Observe(0.5)
	g.Set(10)
	u.Add(3)

	s := b.String()
	for _, want := range []string{
		"counters", "histograms", "gauges", "updowncounters",
		"c{", "h{", "g{", "u{",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("String output missing %q:\n%s", want, s)
		}
	}
}

func TestMemoryBackendStringEmpty(t *testing.T) {
	if got := NewMemoryBackend().String(); got != "" {
		t.Errorf("empty backend should produce empty string, got %q", got)
	}
}
