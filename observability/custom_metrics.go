package observability

import (
	"fmt"
	"strings"

	"github.com/tink3rlabs/magic/telemetry"
)

// Counter returns a custom Counter registered on the Observer's
// metrics backend. The metric name is prefixed with
// cfg.MetricsNamespace when non-empty. Repeated calls with an
// equivalent definition return the same instrument; a
// shape-conflicting re-registration yields an error.
//
// The definition's Kind is always normalized to KindCounter
// before registration — callers can leave it zero.
func (o *Observer) Counter(def telemetry.MetricDefinition) (telemetry.Counter, error) {
	def.Kind = telemetry.KindCounter
	def.Name = o.applyNamespace(def.Name)
	if err := validateCustomDef(def); err != nil {
		return nil, err
	}
	return o.telem.Metrics.Counter(def)
}

// Histogram returns a custom Histogram. See Counter for
// registration semantics. Note that in MetricsModeOTLP the
// Buckets field is ignored at runtime; declare custom histograms
// with OTEL Views registered at Init time for precise control
// over bucket boundaries in that mode.
func (o *Observer) Histogram(def telemetry.MetricDefinition) (telemetry.Histogram, error) {
	def.Kind = telemetry.KindHistogram
	def.Name = o.applyNamespace(def.Name)
	if err := validateCustomDef(def); err != nil {
		return nil, err
	}
	return o.telem.Metrics.Histogram(def)
}

// Gauge returns a custom Gauge. See Counter for registration
// semantics.
func (o *Observer) Gauge(def telemetry.MetricDefinition) (telemetry.Gauge, error) {
	def.Kind = telemetry.KindGauge
	def.Name = o.applyNamespace(def.Name)
	if err := validateCustomDef(def); err != nil {
		return nil, err
	}
	return o.telem.Metrics.Gauge(def)
}

// UpDownCounter returns a custom UpDownCounter. See Counter for
// registration semantics.
func (o *Observer) UpDownCounter(def telemetry.MetricDefinition) (telemetry.UpDownCounter, error) {
	def.Kind = telemetry.KindUpDownCounter
	def.Name = o.applyNamespace(def.Name)
	if err := validateCustomDef(def); err != nil {
		return nil, err
	}
	return o.telem.Metrics.UpDownCounter(def)
}

// applyNamespace prepends cfg.MetricsNamespace to name, joined by
// an underscore, unless the namespace is empty or the name
// already starts with the namespace prefix (so callers that want
// to opt in to their own prefixing can do so).
func (o *Observer) applyNamespace(name string) string {
	ns := o.cfg.MetricsNamespace
	if ns == "" || name == "" {
		return name
	}
	if strings.HasPrefix(name, ns+"_") {
		return name
	}
	return ns + "_" + name
}

// validateCustomDef sanity-checks a user-supplied MetricDefinition
// before it reaches the backend. Catches the common mistakes
// (empty name, reserved magic_* prefix collision, bad name
// characters) with a clear error message.
func validateCustomDef(def telemetry.MetricDefinition) error {
	if def.Name == "" {
		return fmt.Errorf("observability: metric name is required")
	}
	if !metricNameRE.MatchString(def.Name) {
		return fmt.Errorf("observability: metric name %q is not a valid identifier", def.Name)
	}
	for _, l := range def.Labels {
		if l == "" {
			return fmt.Errorf("observability: metric %q has an empty label key", def.Name)
		}
		if !labelNameRE.MatchString(l) {
			return fmt.Errorf("observability: metric %q has invalid label key %q", def.Name, l)
		}
	}
	return nil
}
