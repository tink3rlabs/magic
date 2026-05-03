package observability

import (
	"fmt"
	"reflect"
	"sync"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/tink3rlabs/magic/telemetry"
)

// prometheusBackend is a telemetry.MetricsBackend backed by a
// prometheus.Registry. It caches instruments by name to
// transparently deduplicate repeated registrations (for example
// when two subsystems both call Observer.Counter with the same
// definition) and returns an error on genuine conflicts.
type prometheusBackend struct {
	reg                   *prometheus.Registry
	allowUndeclaredLabels bool

	mu      sync.Mutex
	entries map[string]*promEntry
}

// promEntry keeps the concrete prometheus collector plus the
// definition used to create it so duplicate registrations can be
// validated against the original shape.
type promEntry struct {
	def        telemetry.MetricDefinition
	collector  any // *prometheus.CounterVec / HistogramVec / GaugeVec
	instrument any // the pre-built *promCounter / *promHistogram / etc.
}

func newPrometheusBackend(reg *prometheus.Registry, allowUndeclaredLabels bool) *prometheusBackend {
	return &prometheusBackend{
		reg:                   reg,
		allowUndeclaredLabels: allowUndeclaredLabels,
		entries:               map[string]*promEntry{},
	}
}

// register is the common path for all instrument kinds. On first
// registration of a name it creates the collector, registers it
// with the Prometheus registry, and caches an entry. Subsequent
// calls with an equivalent definition return the cached instrument.
// A definition mismatch produces an error.
func (b *prometheusBackend) register(def telemetry.MetricDefinition, build func() (collector any, instrument any, err error)) (*promEntry, error) {
	if def.Name == "" {
		return nil, fmt.Errorf("prometheus backend: MetricDefinition.Name is required")
	}
	if !metricNameRE.MatchString(def.Name) {
		return nil, fmt.Errorf("prometheus backend: metric name %q is not a valid Prometheus name", def.Name)
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	if existing, ok := b.entries[def.Name]; ok {
		if !definitionsEquivalent(existing.def, def) {
			return nil, fmt.Errorf("prometheus backend: metric %q already registered with a different shape", def.Name)
		}
		return existing, nil
	}

	col, inst, err := build()
	if err != nil {
		return nil, err
	}

	collector, ok := col.(prometheus.Collector)
	if !ok {
		return nil, fmt.Errorf("prometheus backend: internal error: build did not return a prometheus.Collector")
	}
	if err := b.reg.Register(collector); err != nil {
		// Promote AlreadyRegisteredError into a reuse.
		if are, isAlready := err.(prometheus.AlreadyRegisteredError); isAlready {
			col = are.ExistingCollector
			collector = are.ExistingCollector
			_ = collector
			// Rebuild the instrument wrapper around the existing collector.
			inst = rewrapPromInstrument(def, col)
			if inst == nil {
				return nil, fmt.Errorf("prometheus backend: metric %q already registered as incompatible collector", def.Name)
			}
		} else {
			return nil, fmt.Errorf("prometheus backend: register %q: %w", def.Name, err)
		}
	}

	e := &promEntry{def: def, collector: col, instrument: inst}
	b.entries[def.Name] = e
	return e, nil
}

// definitionsEquivalent returns true when two definitions describe
// the same metric (kind, label set, buckets). It does not compare
// Help text: backends typically normalize Help to the first
// registration and a caller accidentally passing a different help
// string should not be fatal.
func definitionsEquivalent(a, b telemetry.MetricDefinition) bool {
	if a.Name != b.Name || a.Kind != b.Kind || a.Unit != b.Unit {
		return false
	}
	if !stringSliceEqual(a.Labels, b.Labels) {
		return false
	}
	if a.Kind == telemetry.KindHistogram && !reflect.DeepEqual(a.Buckets, b.Buckets) {
		return false
	}
	return true
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// rewrapPromInstrument reconstructs the telemetry instrument
// wrapper around an already-registered collector (returned from
// AlreadyRegisteredError). Returns nil if the existing collector's
// type does not match def.Kind.
func rewrapPromInstrument(def telemetry.MetricDefinition, col any) any {
	switch def.Kind {
	case telemetry.KindCounter:
		if cv, ok := col.(*prometheus.CounterVec); ok {
			return &promCounter{vec: cv, def: def}
		}
	case telemetry.KindHistogram:
		if hv, ok := col.(*prometheus.HistogramVec); ok {
			return &promHistogram{vec: hv, def: def}
		}
	case telemetry.KindGauge:
		if gv, ok := col.(*prometheus.GaugeVec); ok {
			return &promGauge{vec: gv, def: def}
		}
	case telemetry.KindUpDownCounter:
		if gv, ok := col.(*prometheus.GaugeVec); ok {
			return &promUpDownCounter{vec: gv, def: def}
		}
	}
	return nil
}

func (b *prometheusBackend) Counter(def telemetry.MetricDefinition) (telemetry.Counter, error) {
	if def.Kind != telemetry.KindCounter {
		return nil, fmt.Errorf("prometheus backend: Counter() requires KindCounter, got %s", def.Kind)
	}
	e, err := b.register(def, func() (any, any, error) {
		vec := prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: def.Name,
			Help: def.Help,
		}, append([]string(nil), def.Labels...))
		inst := &promCounter{vec: vec, def: def}
		return vec, inst, nil
	})
	if err != nil {
		return nil, err
	}
	c, ok := e.instrument.(*promCounter)
	if !ok {
		return nil, fmt.Errorf("prometheus backend: metric %q is not a counter", def.Name)
	}
	c.allowUndeclared = b.allowUndeclaredLabels
	return c, nil
}

func (b *prometheusBackend) Histogram(def telemetry.MetricDefinition) (telemetry.Histogram, error) {
	if def.Kind != telemetry.KindHistogram {
		return nil, fmt.Errorf("prometheus backend: Histogram() requires KindHistogram, got %s", def.Kind)
	}
	e, err := b.register(def, func() (any, any, error) {
		opts := prometheus.HistogramOpts{
			Name: def.Name,
			Help: def.Help,
		}
		if len(def.Buckets) > 0 {
			opts.Buckets = append([]float64(nil), def.Buckets...)
		}
		vec := prometheus.NewHistogramVec(opts, append([]string(nil), def.Labels...))
		inst := &promHistogram{vec: vec, def: def}
		return vec, inst, nil
	})
	if err != nil {
		return nil, err
	}
	h, ok := e.instrument.(*promHistogram)
	if !ok {
		return nil, fmt.Errorf("prometheus backend: metric %q is not a histogram", def.Name)
	}
	h.allowUndeclared = b.allowUndeclaredLabels
	return h, nil
}

func (b *prometheusBackend) Gauge(def telemetry.MetricDefinition) (telemetry.Gauge, error) {
	if def.Kind != telemetry.KindGauge {
		return nil, fmt.Errorf("prometheus backend: Gauge() requires KindGauge, got %s", def.Kind)
	}
	e, err := b.register(def, func() (any, any, error) {
		vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: def.Name,
			Help: def.Help,
		}, append([]string(nil), def.Labels...))
		inst := &promGauge{vec: vec, def: def}
		return vec, inst, nil
	})
	if err != nil {
		return nil, err
	}
	g, ok := e.instrument.(*promGauge)
	if !ok {
		return nil, fmt.Errorf("prometheus backend: metric %q is not a gauge", def.Name)
	}
	g.allowUndeclared = b.allowUndeclaredLabels
	return g, nil
}

func (b *prometheusBackend) UpDownCounter(def telemetry.MetricDefinition) (telemetry.UpDownCounter, error) {
	if def.Kind != telemetry.KindUpDownCounter {
		return nil, fmt.Errorf("prometheus backend: UpDownCounter() requires KindUpDownCounter, got %s", def.Kind)
	}
	e, err := b.register(def, func() (any, any, error) {
		vec := prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: def.Name,
			Help: def.Help,
		}, append([]string(nil), def.Labels...))
		inst := &promUpDownCounter{vec: vec, def: def}
		return vec, inst, nil
	})
	if err != nil {
		return nil, err
	}
	ud, ok := e.instrument.(*promUpDownCounter)
	if !ok {
		return nil, fmt.Errorf("prometheus backend: metric %q is not an up/down counter", def.Name)
	}
	ud.allowUndeclared = b.allowUndeclaredLabels
	return ud, nil
}

// ----- instrument wrappers -----

type promCounter struct {
	vec             *prometheus.CounterVec
	def             telemetry.MetricDefinition
	allowUndeclared bool
}

func (c *promCounter) Add(v float64, labels ...telemetry.Label) {
	if v < 0 {
		telemetry.WarnOnce(labelSuppressionKey(c.def.Name, "negative-counter"),
			"observability: counter Add with negative value suppressed",
			"metric", c.def.Name, "value", v)
		return
	}
	values, err := projectLabels(c.def, !c.allowUndeclared, labels)
	if err != nil {
		telemetry.WarnOnce(labelSuppressionKey(c.def.Name, "labels"),
			"observability: counter observation dropped: "+err.Error(),
			"metric", c.def.Name)
		return
	}
	c.vec.WithLabelValues(values...).Add(v)
}

type promHistogram struct {
	vec             *prometheus.HistogramVec
	def             telemetry.MetricDefinition
	allowUndeclared bool
}

func (h *promHistogram) Observe(v float64, labels ...telemetry.Label) {
	values, err := projectLabels(h.def, !h.allowUndeclared, labels)
	if err != nil {
		telemetry.WarnOnce(labelSuppressionKey(h.def.Name, "labels"),
			"observability: histogram observation dropped: "+err.Error(),
			"metric", h.def.Name)
		return
	}
	h.vec.WithLabelValues(values...).Observe(v)
}

type promGauge struct {
	vec             *prometheus.GaugeVec
	def             telemetry.MetricDefinition
	allowUndeclared bool
}

func (g *promGauge) Set(v float64, labels ...telemetry.Label) {
	values, err := projectLabels(g.def, !g.allowUndeclared, labels)
	if err != nil {
		telemetry.WarnOnce(labelSuppressionKey(g.def.Name, "labels"),
			"observability: gauge observation dropped: "+err.Error(),
			"metric", g.def.Name)
		return
	}
	g.vec.WithLabelValues(values...).Set(v)
}

type promUpDownCounter struct {
	vec             *prometheus.GaugeVec
	def             telemetry.MetricDefinition
	allowUndeclared bool
}

func (u *promUpDownCounter) Add(v float64, labels ...telemetry.Label) {
	values, err := projectLabels(u.def, !u.allowUndeclared, labels)
	if err != nil {
		telemetry.WarnOnce(labelSuppressionKey(u.def.Name, "labels"),
			"observability: up/down counter observation dropped: "+err.Error(),
			"metric", u.def.Name)
		return
	}
	u.vec.WithLabelValues(values...).Add(v)
}
