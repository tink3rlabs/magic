package observability

import (
	"context"
	"fmt"
	"sync"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/tink3rlabs/magic/telemetry"
)

// otelBackend is a telemetry.MetricsBackend backed by an OTEL
// metric.Meter. Instruments are cached by name and deduplicated on
// repeated registration, mirroring the Prometheus backend.
//
// Histogram bucket boundaries declared on MetricDefinition.Buckets
// are honored for built-in metrics via Views pre-registered on the
// MeterProvider (see buildHistogramViews). For custom histograms
// registered after MeterProvider construction, OTEL's default
// histogram aggregation is used; when the caller supplied custom
// buckets they will be silently ignored and a one-shot warning is
// emitted.
type otelBackend struct {
	meter                 metric.Meter
	allowUndeclaredLabels bool

	mu      sync.Mutex
	entries map[string]*otelEntry
}

type otelEntry struct {
	def        telemetry.MetricDefinition
	instrument any
}

func newOTELBackend(meter metric.Meter, allowUndeclaredLabels bool) *otelBackend {
	return &otelBackend{
		meter:                 meter,
		allowUndeclaredLabels: allowUndeclaredLabels,
		entries:               map[string]*otelEntry{},
	}
}

func (b *otelBackend) register(def telemetry.MetricDefinition, build func() (any, error)) (*otelEntry, error) {
	if def.Name == "" {
		return nil, fmt.Errorf("otel backend: MetricDefinition.Name is required")
	}
	if !metricNameRE.MatchString(def.Name) {
		return nil, fmt.Errorf("otel backend: metric name %q is not valid", def.Name)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if existing, ok := b.entries[def.Name]; ok {
		if !definitionsEquivalent(existing.def, def) {
			return nil, fmt.Errorf("otel backend: metric %q already registered with a different shape", def.Name)
		}
		return existing, nil
	}

	inst, err := build()
	if err != nil {
		return nil, err
	}
	e := &otelEntry{def: def, instrument: inst}
	b.entries[def.Name] = e
	return e, nil
}

func (b *otelBackend) Counter(def telemetry.MetricDefinition) (telemetry.Counter, error) {
	if def.Kind != telemetry.KindCounter {
		return nil, fmt.Errorf("otel backend: Counter() requires KindCounter, got %s", def.Kind)
	}
	e, err := b.register(def, func() (any, error) {
		c, err := b.meter.Float64Counter(def.Name,
			metric.WithDescription(def.Help),
			metric.WithUnit(string(def.Unit)),
		)
		if err != nil {
			return nil, err
		}
		return &otelCounter{inst: c, def: def, allowUndeclared: b.allowUndeclaredLabels}, nil
	})
	if err != nil {
		return nil, err
	}
	return e.instrument.(*otelCounter), nil
}

func (b *otelBackend) Histogram(def telemetry.MetricDefinition) (telemetry.Histogram, error) {
	if def.Kind != telemetry.KindHistogram {
		return nil, fmt.Errorf("otel backend: Histogram() requires KindHistogram, got %s", def.Kind)
	}
	e, err := b.register(def, func() (any, error) {
		opts := []metric.Float64HistogramOption{
			metric.WithDescription(def.Help),
			metric.WithUnit(string(def.Unit)),
		}
		if len(def.Buckets) > 0 {
			opts = append(opts, metric.WithExplicitBucketBoundaries(def.Buckets...))
		}
		h, err := b.meter.Float64Histogram(def.Name, opts...)
		if err != nil {
			return nil, err
		}
		return &otelHistogram{inst: h, def: def, allowUndeclared: b.allowUndeclaredLabels}, nil
	})
	if err != nil {
		return nil, err
	}
	return e.instrument.(*otelHistogram), nil
}

func (b *otelBackend) Gauge(def telemetry.MetricDefinition) (telemetry.Gauge, error) {
	if def.Kind != telemetry.KindGauge {
		return nil, fmt.Errorf("otel backend: Gauge() requires KindGauge, got %s", def.Kind)
	}
	e, err := b.register(def, func() (any, error) {
		g, err := b.meter.Float64Gauge(def.Name,
			metric.WithDescription(def.Help),
			metric.WithUnit(string(def.Unit)),
		)
		if err != nil {
			return nil, err
		}
		return &otelGauge{inst: g, def: def, allowUndeclared: b.allowUndeclaredLabels}, nil
	})
	if err != nil {
		return nil, err
	}
	return e.instrument.(*otelGauge), nil
}

func (b *otelBackend) UpDownCounter(def telemetry.MetricDefinition) (telemetry.UpDownCounter, error) {
	if def.Kind != telemetry.KindUpDownCounter {
		return nil, fmt.Errorf("otel backend: UpDownCounter() requires KindUpDownCounter, got %s", def.Kind)
	}
	e, err := b.register(def, func() (any, error) {
		ud, err := b.meter.Float64UpDownCounter(def.Name,
			metric.WithDescription(def.Help),
			metric.WithUnit(string(def.Unit)),
		)
		if err != nil {
			return nil, err
		}
		return &otelUpDownCounter{inst: ud, def: def, allowUndeclared: b.allowUndeclaredLabels}, nil
	})
	if err != nil {
		return nil, err
	}
	return e.instrument.(*otelUpDownCounter), nil
}

// attributesFor converts an observation's ordered []Label slice to
// an attribute.Set suitable for OTEL instrument methods. When
// strict-labels mode is active it drops observations with
// undeclared keys and returns ok=false.
func attributesFor(def telemetry.MetricDefinition, strict bool, labels []telemetry.Label) ([]attribute.KeyValue, bool) {
	if len(def.Labels) == 0 {
		if strict && len(labels) > 0 {
			telemetry.WarnOnce(labelSuppressionKey(def.Name, "labels"),
				"observability: observation dropped: metric declared with no labels",
				"metric", def.Name, "got", len(labels))
			return nil, false
		}
		if len(labels) == 0 {
			return nil, true
		}
		out := make([]attribute.KeyValue, 0, len(labels))
		for _, l := range labels {
			out = append(out, attribute.String(l.Key, l.Value))
		}
		return out, true
	}

	out := make([]attribute.KeyValue, 0, len(def.Labels))
	declared := make(map[string]bool, len(def.Labels))
	for _, k := range def.Labels {
		declared[k] = true
	}
	seen := make(map[string]string, len(labels))
	for _, l := range labels {
		if !declared[l.Key] {
			if strict {
				telemetry.WarnOnce(labelSuppressionKey(def.Name, "labels"),
					"observability: observation dropped: undeclared label",
					"metric", def.Name, "label", l.Key)
				return nil, false
			}
			continue
		}
		seen[l.Key] = l.Value
	}
	for _, k := range def.Labels {
		out = append(out, attribute.String(k, seen[k]))
	}
	return out, true
}

// ----- instrument wrappers -----

type otelCounter struct {
	inst            metric.Float64Counter
	def             telemetry.MetricDefinition
	allowUndeclared bool
}

func (c *otelCounter) Add(v float64, labels ...telemetry.Label) {
	if v < 0 {
		telemetry.WarnOnce(labelSuppressionKey(c.def.Name, "negative-counter"),
			"observability: counter Add with negative value suppressed",
			"metric", c.def.Name, "value", v)
		return
	}
	attrs, ok := attributesFor(c.def, !c.allowUndeclared, labels)
	if !ok {
		return
	}
	c.inst.Add(context.Background(), v, metric.WithAttributes(attrs...))
}

type otelHistogram struct {
	inst            metric.Float64Histogram
	def             telemetry.MetricDefinition
	allowUndeclared bool
}

func (h *otelHistogram) Observe(v float64, labels ...telemetry.Label) {
	attrs, ok := attributesFor(h.def, !h.allowUndeclared, labels)
	if !ok {
		return
	}
	h.inst.Record(context.Background(), v, metric.WithAttributes(attrs...))
}

type otelGauge struct {
	inst            metric.Float64Gauge
	def             telemetry.MetricDefinition
	allowUndeclared bool
}

func (g *otelGauge) Set(v float64, labels ...telemetry.Label) {
	attrs, ok := attributesFor(g.def, !g.allowUndeclared, labels)
	if !ok {
		return
	}
	g.inst.Record(context.Background(), v, metric.WithAttributes(attrs...))
}

type otelUpDownCounter struct {
	inst            metric.Float64UpDownCounter
	def             telemetry.MetricDefinition
	allowUndeclared bool
}

func (u *otelUpDownCounter) Add(v float64, labels ...telemetry.Label) {
	attrs, ok := attributesFor(u.def, !u.allowUndeclared, labels)
	if !ok {
		return
	}
	u.inst.Add(context.Background(), v, metric.WithAttributes(attrs...))
}
