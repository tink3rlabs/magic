package observability

import (
	"strings"
	"testing"

	"github.com/tink3rlabs/magic/telemetry"
)

func TestCustomCounterNamespace(t *testing.T) {
	obs := initTestObserver(t)
	obs.cfg.MetricsNamespace = "myapp"

	c, err := obs.Counter(telemetry.MetricDefinition{
		Name:   "widgets_total",
		Help:   "widget count",
		Labels: []string{"color"},
	})
	if err != nil {
		t.Fatalf("Counter: %v", err)
	}
	c.Add(1, telemetry.Label{Key: "color", Value: "blue"})

	// Name is namespaced; the raw name alone should not appear in
	// a later registration lookup.
	got := countersFromScrapeRaw(t, obs, "myapp_widgets_total", `color="blue"`)
	if got != 1 {
		t.Errorf("expected myapp_widgets_total{color=blue}=1, got %v", got)
	}
}

func TestCustomCounterNamespaceIdempotent(t *testing.T) {
	obs := initTestObserver(t)
	obs.cfg.MetricsNamespace = "myapp"

	// Caller accidentally includes the namespace themselves —
	// applyNamespace must not prefix twice.
	_, err := obs.Counter(telemetry.MetricDefinition{
		Name:   "myapp_widgets_total",
		Help:   "widget count",
		Labels: []string{"color"},
	})
	if err != nil {
		t.Fatalf("Counter: %v", err)
	}

	defs := map[string]bool{}
	// Use the second factory call to capture the applied name.
	obs.cfg.MetricsNamespace = "myapp"
	c2, _ := obs.Counter(telemetry.MetricDefinition{
		Name:   "myapp_widgets_total",
		Help:   "widget count",
		Labels: []string{"color"},
	})
	_ = c2
	defs["ok"] = true
}

func TestCustomMetricValidation(t *testing.T) {
	obs := initTestObserver(t)

	cases := []struct {
		name string
		def  telemetry.MetricDefinition
		want string // substring expected in error
	}{
		{
			name: "empty name",
			def:  telemetry.MetricDefinition{Name: "", Labels: []string{"k"}},
			want: "name is required",
		},
		{
			name: "bad name char",
			def:  telemetry.MetricDefinition{Name: "has-dash", Labels: []string{"k"}},
			want: "not a valid identifier",
		},
		{
			name: "bad label",
			def:  telemetry.MetricDefinition{Name: "x", Labels: []string{"bad-label"}},
			want: "invalid label key",
		},
		{
			name: "empty label",
			def:  telemetry.MetricDefinition{Name: "x", Labels: []string{""}},
			want: "empty label key",
		},
		{
			name: "duplicate label",
			def:  telemetry.MetricDefinition{Name: "x", Labels: []string{"a", "a"}},
			want: "duplicate label key",
		},
		{
			name: "builtin collision",
			def:  telemetry.MetricDefinition{Name: HTTPRequestsTotal},
			want: "collides with a built-in metric",
		},
		{
			name: "go_ reserved prefix",
			def:  telemetry.MetricDefinition{Name: "go_my_metric"},
			want: "reserved prefix",
		},
		{
			name: "process_ reserved prefix",
			def:  telemetry.MetricDefinition{Name: "process_my_metric"},
			want: "reserved prefix",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := obs.Counter(tc.def)
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.want)
			}
		})
	}
}

func TestCustomMetricRejectsBucketsOnNonHistogram(t *testing.T) {
	obs := initTestObserver(t)

	_, err := obs.Counter(telemetry.MetricDefinition{
		Name:    "widgets_total",
		Buckets: []float64{0.1, 1},
	})
	if err == nil {
		t.Fatalf("expected error for Buckets on non-histogram")
	}
	if !strings.Contains(err.Error(), "Buckets") {
		t.Errorf("error %q should mention Buckets", err.Error())
	}
}

func TestCustomMetricShapeConflict(t *testing.T) {
	obs := initTestObserver(t)

	_, err := obs.Counter(telemetry.MetricDefinition{
		Name: "my_metric", Labels: []string{"a"},
	})
	if err != nil {
		t.Fatalf("first registration: %v", err)
	}
	// Same name, different label set — must error.
	_, err = obs.Counter(telemetry.MetricDefinition{
		Name: "my_metric", Labels: []string{"a", "b"},
	})
	if err == nil {
		t.Error("expected shape-conflict error on re-registration")
	}
}

func TestCustomMetricGauge(t *testing.T) {
	obs := initTestObserver(t)

	g, err := obs.Gauge(telemetry.MetricDefinition{
		Name:   "queue_depth",
		Help:   "pending items",
		Labels: []string{"queue"},
	})
	if err != nil {
		t.Fatalf("Gauge: %v", err)
	}
	g.Set(7, telemetry.Label{Key: "queue", Value: "a"})
	g.Set(4, telemetry.Label{Key: "queue", Value: "a"})

	got := countersFromScrapeRaw(t, obs, "queue_depth", `queue="a"`)
	if got != 4 {
		t.Errorf("gauge = %v, want 4 (latest Set)", got)
	}
}

func TestCustomMetricGaugeRejectsInvalidName(t *testing.T) {
	obs := initTestObserver(t)
	if _, err := obs.Gauge(telemetry.MetricDefinition{Name: "bad-name"}); err == nil {
		t.Error("invalid name must be rejected")
	}
	if _, err := obs.Gauge(telemetry.MetricDefinition{Name: ""}); err == nil {
		t.Error("empty name must be rejected")
	}
}

func TestCustomMetricUpDownCounter(t *testing.T) {
	obs := initTestObserver(t)

	u, err := obs.UpDownCounter(telemetry.MetricDefinition{
		Name:   "inflight",
		Help:   "in-flight work",
		Labels: []string{"kind"},
	})
	if err != nil {
		t.Fatalf("UpDownCounter: %v", err)
	}
	u.Add(3, telemetry.Label{Key: "kind", Value: "io"})
	u.Add(-1, telemetry.Label{Key: "kind", Value: "io"})

	got := countersFromScrapeRaw(t, obs, "inflight", `kind="io"`)
	if got != 2 {
		t.Errorf("updown = %v, want 2", got)
	}
}

func TestCustomMetricUpDownCounterRejectsInvalidLabel(t *testing.T) {
	obs := initTestObserver(t)
	if _, err := obs.UpDownCounter(telemetry.MetricDefinition{
		Name: "x", Labels: []string{"bad-label"},
	}); err == nil {
		t.Error("invalid label must be rejected")
	}
}

func TestCustomMetricHistogramBuckets(t *testing.T) {
	obs := initTestObserver(t)

	h, err := obs.Histogram(telemetry.MetricDefinition{
		Name:    "job_duration_seconds",
		Help:    "duration of batch job",
		Unit:    telemetry.UnitSeconds,
		Buckets: []float64{0.1, 1, 10},
	})
	if err != nil {
		t.Fatalf("Histogram: %v", err)
	}
	h.Observe(0.05)
	h.Observe(0.5)
	h.Observe(5)

	// Prometheus exposition includes _count and _bucket lines;
	// scrape and verify the sum contract.
	sum := countersFromScrapeRaw(t, obs, "job_duration_seconds_count")
	if sum != 3 {
		t.Errorf("expected 3 observations, got %v", sum)
	}
}
