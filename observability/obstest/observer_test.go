package obstest

import (
	"context"
	"testing"

	"github.com/tink3rlabs/magic/telemetry"
)

func TestNewTestObserverInstallsGlobal(t *testing.T) {
	before := telemetry.Global()

	// Run the observer inside a subtest so its t.Cleanup (which
	// restores the global) fires before we assert the restoration.
	var installed *telemetry.Telemetry
	t.Run("install", func(st *testing.T) {
		obs := NewTestObserver(st)
		installed = obs.Telemetry
		if telemetry.Global() != obs.Telemetry {
			st.Error("NewTestObserver should install Telemetry as global")
		}
	})

	if telemetry.Global() == installed {
		t.Error("subtest cleanup should have restored the previous global")
	}
	if telemetry.Global() != before {
		t.Error("global should match the pre-subtest value")
	}
}

func TestMemoryBackendCounter(t *testing.T) {
	obs := NewTestObserver(t)

	c, err := obs.Telemetry.Metrics.Counter(telemetry.MetricDefinition{
		Name:   "orders_total",
		Labels: []string{"status"},
	})
	if err != nil {
		t.Fatalf("Counter: %v", err)
	}
	c.Add(1, telemetry.Label{Key: "status", Value: "ok"})
	c.Add(2, telemetry.Label{Key: "status", Value: "ok"})
	c.Add(4, telemetry.Label{Key: "status", Value: "fail"})

	if got := obs.Metrics.CounterValue("orders_total",
		telemetry.Label{Key: "status", Value: "ok"}); got != 3 {
		t.Errorf("ok counter = %v, want 3", got)
	}
	if got := obs.Metrics.CounterValue("orders_total",
		telemetry.Label{Key: "status", Value: "fail"}); got != 4 {
		t.Errorf("fail counter = %v, want 4", got)
	}
}

func TestMemoryBackendHistogram(t *testing.T) {
	obs := NewTestObserver(t)

	h, err := obs.Telemetry.Metrics.Histogram(telemetry.MetricDefinition{
		Name: "latency_seconds",
	})
	if err != nil {
		t.Fatalf("Histogram: %v", err)
	}
	h.Observe(0.1)
	h.Observe(0.2)
	h.Observe(0.3)

	if got := obs.Metrics.HistogramCount("latency_seconds"); got != 3 {
		t.Errorf("count = %d, want 3", got)
	}
	if got := obs.Metrics.HistogramSum("latency_seconds"); got < 0.59 || got > 0.61 {
		t.Errorf("sum = %v, want ~0.6", got)
	}
}

func TestMemoryBackendGaugeAndUpDown(t *testing.T) {
	obs := NewTestObserver(t)

	g, _ := obs.Telemetry.Metrics.Gauge(telemetry.MetricDefinition{Name: "temp"})
	g.Set(20)
	g.Set(22)
	if got := obs.Metrics.GaugeValue("temp"); got != 22 {
		t.Errorf("gauge = %v, want 22", got)
	}

	ud, _ := obs.Telemetry.Metrics.UpDownCounter(telemetry.MetricDefinition{Name: "queue_depth"})
	ud.Add(3)
	ud.Add(-1)
	if got := obs.Metrics.UpDownValue("queue_depth"); got != 2 {
		t.Errorf("updown = %v, want 2", got)
	}
}

func TestMemoryBackendLabelOrderAgnostic(t *testing.T) {
	obs := NewTestObserver(t)

	c, _ := obs.Telemetry.Metrics.Counter(telemetry.MetricDefinition{
		Name:   "requests_total",
		Labels: []string{"method", "route"},
	})
	c.Add(1, telemetry.Label{Key: "method", Value: "GET"}, telemetry.Label{Key: "route", Value: "/"})

	// Same labels, reverse order
	if got := obs.Metrics.CounterValue("requests_total",
		telemetry.Label{Key: "route", Value: "/"},
		telemetry.Label{Key: "method", Value: "GET"}); got != 1 {
		t.Errorf("expected 1 after reverse-order lookup, got %v", got)
	}
}

func TestSpansRecorded(t *testing.T) {
	obs := NewTestObserver(t)

	_, span := obs.Telemetry.Tracer.Start(context.Background(), "op")
	span.End()

	spans := obs.Spans.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 ended span, got %d", len(spans))
	}
	if spans[0].Name() != "op" {
		t.Errorf("span name = %q, want %q", spans[0].Name(), "op")
	}
}

func TestResetClearsData(t *testing.T) {
	obs := NewTestObserver(t)
	c, _ := obs.Telemetry.Metrics.Counter(telemetry.MetricDefinition{Name: "x"})
	c.Add(1)
	obs.Metrics.Reset()
	if got := obs.Metrics.CounterValue("x"); got != 0 {
		t.Errorf("reset should clear counters, got %v", got)
	}
}
