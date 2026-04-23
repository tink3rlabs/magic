package telemetry

// MetricKind identifies the instrument type of a registered metric.
type MetricKind int

const (
	// KindCounter is a monotonically-increasing cumulative counter.
	KindCounter MetricKind = iota
	// KindHistogram records a distribution of values.
	KindHistogram
	// KindGauge records an instantaneous value that replaces any
	// previous value.
	KindGauge
	// KindUpDownCounter tracks a sum that can increase or decrease
	// (e.g. in-flight requests, queue depth).
	KindUpDownCounter
)

// String returns the lowercase name of the kind (for use in
// diagnostics and error messages).
func (k MetricKind) String() string {
	switch k {
	case KindCounter:
		return "counter"
	case KindHistogram:
		return "histogram"
	case KindGauge:
		return "gauge"
	case KindUpDownCounter:
		return "updowncounter"
	default:
		return "unknown"
	}
}

// Unit is a UCUM-style unit hint (for example "s" for seconds,
// "By" for bytes, "1" for dimensionless). Backends that do not
// understand the unit ignore it.
type Unit string

// Common units used by built-in magic metrics.
const (
	UnitSeconds       Unit = "s"
	UnitBytes         Unit = "By"
	UnitDimensionless Unit = "1"
)

// Label is a key/value pair attached to a metric observation.
// Backends are responsible for normalising the value (for example
// the Prometheus backend never allows an unset label; callers
// should pass the empty string rather than omit the label).
type Label struct {
	Key   string
	Value string
}

// Labels is a lightweight constructor for a []Label slice from an
// alternating key/value string list. It mirrors the ergonomics of
// the slog.Attr helpers so typical call sites read as:
//
//	ordersCreated.Add(1, telemetry.Labels(
//	    "status",  "success",
//	    "channel", "web",
//	)...)
//
// Odd-length inputs drop the trailing unpaired key (its value is
// treated as the empty string). Empty input returns a nil slice
// so the caller can splat it with ... without introducing a
// zero-label observation allocation.
func Labels(kv ...string) []Label {
	if len(kv) == 0 {
		return nil
	}
	out := make([]Label, 0, (len(kv)+1)/2)
	for i := 0; i < len(kv); i += 2 {
		if i+1 >= len(kv) {
			out = append(out, Label{Key: kv[i]})
			break
		}
		out = append(out, Label{Key: kv[i], Value: kv[i+1]})
	}
	return out
}

// MetricDefinition declares the shape of a metric at registration
// time. Backends use this to pre-create instruments and, when
// strict-labels mode is on, to reject observations that carry
// label keys outside of Labels.
type MetricDefinition struct {
	// Name is the fully-qualified metric name (for example
	// "magic_http_requests_total"). Must be non-empty and conform
	// to the Prometheus naming rules: [a-zA-Z_:][a-zA-Z0-9_:]*.
	Name string

	// Help is a short human-readable description of what the
	// metric measures. Required by the Prometheus exposition
	// format and strongly recommended for OTLP.
	Help string

	// Unit is an optional UCUM unit hint.
	Unit Unit

	// Kind identifies the instrument type. Required.
	Kind MetricKind

	// Labels declares the ordered set of label keys that
	// observations are expected to carry. When strict-labels mode
	// is enabled on the backend, observations with keys outside
	// this set are dropped and a one-shot warning is logged.
	// An empty or nil Labels slice means the metric carries no
	// labels.
	Labels []string

	// Buckets is the histogram bucket upper bounds in ascending
	// order. Ignored for non-histogram kinds. When nil the backend
	// applies its built-in default (Prometheus default buckets or
	// the OTEL SDK default view).
	Buckets []float64
}

// MetricsBackend creates backend-neutral metric instruments.
// Implementations must be safe for concurrent use. Instruments
// returned from the same definition on a given backend must be
// usable concurrently and may be the same underlying instance
// (implementations are free to deduplicate).
type MetricsBackend interface {
	Counter(def MetricDefinition) (Counter, error)
	Histogram(def MetricDefinition) (Histogram, error)
	Gauge(def MetricDefinition) (Gauge, error)
	UpDownCounter(def MetricDefinition) (UpDownCounter, error)
}

// Counter accumulates monotonic values. Negative Add values are
// invalid; backends log a one-shot warning and ignore them.
type Counter interface {
	Add(value float64, labels ...Label)
}

// Histogram records a distribution of values.
type Histogram interface {
	Observe(value float64, labels ...Label)
}

// Gauge records an instantaneous value; subsequent Set calls
// replace the previous value. Modelled on the OpenTelemetry
// asynchronous gauge but exposed as a synchronous setter for
// ergonomics.
type Gauge interface {
	Set(value float64, labels ...Label)
}

// UpDownCounter tracks a value that can go up or down over time
// (for example in-flight requests or queue depth).
type UpDownCounter interface {
	Add(value float64, labels ...Label)
}
