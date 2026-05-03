// Package obstest provides in-memory test harnesses for code that
// is instrumented via the magic telemetry package. The primary
// entry point is NewTestObserver, which installs a recording
// Telemetry as the global and exposes assertion helpers for
// counters, histograms, gauges, and spans.
//
// The harness is allocation-friendly and safe for concurrent use
// from within a single test; callers that share a TestObserver
// across goroutines should rely on the built-in mutexes but must
// still avoid calling the Reset / Close helpers from concurrent
// goroutines.
package obstest

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/tink3rlabs/magic/telemetry"
)

// MemoryBackend is a telemetry.MetricsBackend implementation that
// records every observation in-process. It is the default metrics
// backend attached to TestObserver.
//
// Observations are keyed by (metric name, sorted label string).
// The label string is built deterministically so that tests can
// call Lookup with a Labels map in any order.
type MemoryBackend struct {
	mu       sync.Mutex
	counters map[metricKey]float64
	hists    map[metricKey][]float64
	gauges   map[metricKey]float64
	updowns  map[metricKey]float64

	defs map[string]telemetry.MetricDefinition
}

// metricKey is the map key combining a metric name with a
// canonical serialization of its label values.
type metricKey struct {
	name   string
	labels string
}

// NewMemoryBackend returns an empty MemoryBackend.
func NewMemoryBackend() *MemoryBackend {
	return &MemoryBackend{
		counters: map[metricKey]float64{},
		hists:    map[metricKey][]float64{},
		gauges:   map[metricKey]float64{},
		updowns:  map[metricKey]float64{},
		defs:     map[string]telemetry.MetricDefinition{},
	}
}

func (b *MemoryBackend) remember(def telemetry.MetricDefinition) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.defs[def.Name] = def
}

// Counter implements telemetry.MetricsBackend.
func (b *MemoryBackend) Counter(def telemetry.MetricDefinition) (telemetry.Counter, error) {
	b.remember(def)
	return &memoryCounter{b: b, name: def.Name}, nil
}

// Histogram implements telemetry.MetricsBackend.
func (b *MemoryBackend) Histogram(def telemetry.MetricDefinition) (telemetry.Histogram, error) {
	b.remember(def)
	return &memoryHistogram{b: b, name: def.Name}, nil
}

// Gauge implements telemetry.MetricsBackend.
func (b *MemoryBackend) Gauge(def telemetry.MetricDefinition) (telemetry.Gauge, error) {
	b.remember(def)
	return &memoryGauge{b: b, name: def.Name}, nil
}

// UpDownCounter implements telemetry.MetricsBackend.
func (b *MemoryBackend) UpDownCounter(def telemetry.MetricDefinition) (telemetry.UpDownCounter, error) {
	b.remember(def)
	return &memoryUpDown{b: b, name: def.Name}, nil
}

// CounterValue returns the cumulative value of the named counter
// for the given label set, or 0 when nothing matches.
func (b *MemoryBackend) CounterValue(name string, labels ...telemetry.Label) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.counters[metricKey{name: name, labels: labelKey(labels)}]
}

// HistogramObservations returns a copy of the recorded values for
// the named histogram and label set (in observation order).
func (b *MemoryBackend) HistogramObservations(name string, labels ...telemetry.Label) []float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	src := b.hists[metricKey{name: name, labels: labelKey(labels)}]
	out := make([]float64, len(src))
	copy(out, src)
	return out
}

// HistogramCount returns the number of observations recorded for
// the named histogram and label set.
func (b *MemoryBackend) HistogramCount(name string, labels ...telemetry.Label) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.hists[metricKey{name: name, labels: labelKey(labels)}])
}

// HistogramSum returns the sum of observations recorded for the
// named histogram and label set.
func (b *MemoryBackend) HistogramSum(name string, labels ...telemetry.Label) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	var sum float64
	for _, v := range b.hists[metricKey{name: name, labels: labelKey(labels)}] {
		sum += v
	}
	return sum
}

// GaugeValue returns the latest value Set for the named gauge and
// label set, or 0 when nothing matches.
func (b *MemoryBackend) GaugeValue(name string, labels ...telemetry.Label) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.gauges[metricKey{name: name, labels: labelKey(labels)}]
}

// UpDownValue returns the cumulative sum for the named up/down
// counter and label set.
func (b *MemoryBackend) UpDownValue(name string, labels ...telemetry.Label) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.updowns[metricKey{name: name, labels: labelKey(labels)}]
}

// Reset clears all recorded observations while preserving the set
// of registered definitions. Useful between test cases within the
// same test function.
func (b *MemoryBackend) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counters = map[metricKey]float64{}
	b.hists = map[metricKey][]float64{}
	b.gauges = map[metricKey]float64{}
	b.updowns = map[metricKey]float64{}
}

// Definitions returns a snapshot of the MetricDefinitions that
// have been registered against this backend. Useful for assertions
// that a subsystem has declared its metrics during Init.
func (b *MemoryBackend) Definitions() map[string]telemetry.MetricDefinition {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]telemetry.MetricDefinition, len(b.defs))
	for k, v := range b.defs {
		out[k] = v
	}
	return out
}

// labelKey builds a deterministic string representation of a
// label set so observations with the same logical labels land in
// the same map bucket regardless of caller order.
func labelKey(labels []telemetry.Label) string {
	if len(labels) == 0 {
		return ""
	}
	pairs := make([]string, 0, len(labels))
	for _, l := range labels {
		pairs = append(pairs, l.Key+"="+l.Value)
	}
	sort.Strings(pairs)
	return strings.Join(pairs, ",")
}

// ----- instrument impls -----

type memoryCounter struct {
	b    *MemoryBackend
	name string
}

func (c *memoryCounter) Add(v float64, labels ...telemetry.Label) {
	if v < 0 {
		return
	}
	c.b.mu.Lock()
	defer c.b.mu.Unlock()
	c.b.counters[metricKey{name: c.name, labels: labelKey(labels)}] += v
}

type memoryHistogram struct {
	b    *MemoryBackend
	name string
}

func (h *memoryHistogram) Observe(v float64, labels ...telemetry.Label) {
	h.b.mu.Lock()
	defer h.b.mu.Unlock()
	k := metricKey{name: h.name, labels: labelKey(labels)}
	h.b.hists[k] = append(h.b.hists[k], v)
}

type memoryGauge struct {
	b    *MemoryBackend
	name string
}

func (g *memoryGauge) Set(v float64, labels ...telemetry.Label) {
	g.b.mu.Lock()
	defer g.b.mu.Unlock()
	g.b.gauges[metricKey{name: g.name, labels: labelKey(labels)}] = v
}

type memoryUpDown struct {
	b    *MemoryBackend
	name string
}

func (u *memoryUpDown) Add(v float64, labels ...telemetry.Label) {
	u.b.mu.Lock()
	defer u.b.mu.Unlock()
	u.b.updowns[metricKey{name: u.name, labels: labelKey(labels)}] += v
}

// Ensure MemoryBackend satisfies telemetry.MetricsBackend.
var _ telemetry.MetricsBackend = (*MemoryBackend)(nil)

// String returns a multi-line summary of the recorded state,
// useful when a test assertion fails. Intended for diagnostic
// messages rather than machine parsing.
func (b *MemoryBackend) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	var sb strings.Builder
	if len(b.counters) > 0 {
		sb.WriteString("counters:\n")
		for k, v := range b.counters {
			fmt.Fprintf(&sb, "  %s{%s} = %g\n", k.name, k.labels, v)
		}
	}
	if len(b.hists) > 0 {
		sb.WriteString("histograms:\n")
		for k, v := range b.hists {
			fmt.Fprintf(&sb, "  %s{%s} count=%d\n", k.name, k.labels, len(v))
		}
	}
	if len(b.gauges) > 0 {
		sb.WriteString("gauges:\n")
		for k, v := range b.gauges {
			fmt.Fprintf(&sb, "  %s{%s} = %g\n", k.name, k.labels, v)
		}
	}
	if len(b.updowns) > 0 {
		sb.WriteString("updowncounters:\n")
		for k, v := range b.updowns {
			fmt.Fprintf(&sb, "  %s{%s} = %g\n", k.name, k.labels, v)
		}
	}
	return sb.String()
}
