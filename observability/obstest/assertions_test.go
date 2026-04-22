package obstest_test

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/tink3rlabs/magic/observability/obstest"
	"github.com/tink3rlabs/magic/telemetry"
)

// recordingTB satisfies obstest.TestingTB and captures the first
// Errorf / Fatalf call so tests can exercise the failure path of
// an assertion without actually failing the outer test.
type recordingTB struct {
	errored bool
	fatal   bool
	lastMsg string
}

func (r *recordingTB) Helper()                            {}
func (r *recordingTB) Errorf(f string, a ...any)          { r.errored = true; r.lastMsg = fmt.Sprintf(f, a...) }
func (r *recordingTB) Fatalf(f string, a ...any)          { r.fatal = true; r.lastMsg = fmt.Sprintf(f, a...) }

func newCounter(t *testing.T, obs *obstest.TestObserver, name string, labels ...string) telemetry.Counter {
	t.Helper()
	c, err := obs.Telemetry.Metrics.Counter(telemetry.MetricDefinition{
		Name:   name,
		Kind:   telemetry.KindCounter,
		Labels: labels,
	})
	if err != nil {
		t.Fatalf("Counter(%s): %v", name, err)
	}
	return c
}

func TestAssertCounterMatchesValue(t *testing.T) {
	obs := obstest.NewTestObserver(t)
	c := newCounter(t, obs, "widgets_total", "color")
	c.Add(3, telemetry.Label{Key: "color", Value: "blue"})

	obs.AssertCounter(t, "widgets_total", 3, telemetry.Label{Key: "color", Value: "blue"})
}

func TestAssertCounterFailsOnMismatch(t *testing.T) {
	obs := obstest.NewTestObserver(t)
	c := newCounter(t, obs, "widgets_total", "color")
	c.Add(2, telemetry.Label{Key: "color", Value: "blue"})

	tb := &recordingTB{}
	obs.AssertCounter(tb, "widgets_total", 5, telemetry.Label{Key: "color", Value: "blue"})
	if !tb.fatal {
		t.Fatalf("expected Fatalf on counter mismatch")
	}
	if !strings.Contains(tb.lastMsg, "widgets_total") || !strings.Contains(tb.lastMsg, "want 5") {
		t.Errorf("unexpected message: %s", tb.lastMsg)
	}
}

func TestAssertHistogramObservedReturnsCount(t *testing.T) {
	obs := obstest.NewTestObserver(t)
	h, err := obs.Telemetry.Metrics.Histogram(telemetry.MetricDefinition{
		Name: "lat", Kind: telemetry.KindHistogram,
	})
	if err != nil {
		t.Fatalf("Histogram: %v", err)
	}
	h.Observe(0.1)
	h.Observe(0.2)

	if n := obs.AssertHistogramObserved(t, "lat"); n != 2 {
		t.Errorf("count = %d; want 2", n)
	}
}

func TestAssertHistogramObservedFailsWhenEmpty(t *testing.T) {
	obs := obstest.NewTestObserver(t)
	_, err := obs.Telemetry.Metrics.Histogram(telemetry.MetricDefinition{
		Name: "lat", Kind: telemetry.KindHistogram,
	})
	if err != nil {
		t.Fatalf("Histogram: %v", err)
	}

	tb := &recordingTB{}
	obs.AssertHistogramObserved(tb, "lat")
	if !tb.fatal {
		t.Fatalf("expected Fatalf on zero observations")
	}
}

func TestAssertHistogramCountMatches(t *testing.T) {
	obs := obstest.NewTestObserver(t)
	h, _ := obs.Telemetry.Metrics.Histogram(telemetry.MetricDefinition{
		Name: "lat", Kind: telemetry.KindHistogram,
	})
	h.Observe(0.1)
	h.Observe(0.2)
	h.Observe(0.3)

	obs.AssertHistogramCount(t, "lat", 3)

	tb := &recordingTB{}
	obs.AssertHistogramCount(tb, "lat", 10)
	if !tb.fatal {
		t.Fatalf("expected mismatch to fail")
	}
}

func TestAssertGaugeAndUpDownCounter(t *testing.T) {
	obs := obstest.NewTestObserver(t)

	g, _ := obs.Telemetry.Metrics.Gauge(telemetry.MetricDefinition{
		Name: "queue_depth", Kind: telemetry.KindGauge,
	})
	g.Set(7)
	obs.AssertGauge(t, "queue_depth", 7)

	u, _ := obs.Telemetry.Metrics.UpDownCounter(telemetry.MetricDefinition{
		Name: "inflight", Kind: telemetry.KindUpDownCounter,
	})
	u.Add(5)
	u.Add(-2)
	obs.AssertUpDownCounter(t, "inflight", 3)

	tb := &recordingTB{}
	obs.AssertGauge(tb, "queue_depth", 99)
	if !tb.fatal {
		t.Fatalf("expected gauge mismatch to fail")
	}
}

func TestAssertSpanReturnsRecordedSpan(t *testing.T) {
	obs := obstest.NewTestObserver(t)

	_, span := obs.Telemetry.Tracer.Start(context.Background(), "unit.op")
	span.End()

	got := obs.AssertSpan(t, "unit.op")
	if got == nil || got.Name() != "unit.op" {
		t.Fatalf("AssertSpan returned %v", got)
	}
}

func TestAssertSpanFailsWhenMissing(t *testing.T) {
	obs := obstest.NewTestObserver(t)

	tb := &recordingTB{}
	obs.AssertSpan(tb, "never-created")
	if !tb.fatal {
		t.Fatalf("expected Fatalf on missing span")
	}
}

func TestAssertNoSpanFailsWhenPresent(t *testing.T) {
	obs := obstest.NewTestObserver(t)

	_, span := obs.Telemetry.Tracer.Start(context.Background(), "unwanted")
	span.End()

	tb := &recordingTB{}
	obs.AssertNoSpan(tb, "unwanted")
	if !tb.errored {
		t.Fatalf("expected Errorf on unexpected span")
	}
}

func TestAssertNoSpanPassesWhenAbsent(t *testing.T) {
	obs := obstest.NewTestObserver(t)
	obs.AssertNoSpan(t, "nothing-here")
}
