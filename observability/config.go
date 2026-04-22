package observability

import (
	"fmt"
	"regexp"
	"time"

	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// MetricsMode selects how metrics are exposed to collectors.
type MetricsMode string

const (
	// MetricsModePrometheus exposes metrics via a pull-based
	// /metrics handler returned by Observer.MetricsHandler.
	MetricsModePrometheus MetricsMode = "prometheus"
	// MetricsModeOTLP pushes metrics over OTLP/gRPC to the
	// configured collector endpoint.
	MetricsModeOTLP MetricsMode = "otlp"
)

// Valid reports whether m is one of the supported modes.
func (m MetricsMode) Valid() bool {
	switch m {
	case MetricsModePrometheus, MetricsModeOTLP:
		return true
	default:
		return false
	}
}

// Config configures the observability stack. Construct it directly
// or start from DefaultConfig and override the fields you care
// about. Init validates the config and returns a descriptive error
// on misuse.
type Config struct {
	// ServiceName is the logical service name; becomes the
	// service.name resource attribute on traces and metrics.
	// Required and non-empty.
	ServiceName string

	// ServiceVersion becomes service.version. Optional.
	ServiceVersion string

	// Environment becomes deployment.environment.name. Optional.
	Environment string

	// ResourceAttributes are extra key/value attributes merged
	// into the OTEL resource for both traces and metrics.
	// The built-in service.* and deployment.* keys take
	// precedence on conflict.
	ResourceAttributes map[string]string

	// EnableTracing turns distributed tracing on or off. When
	// false, magic core packages emit no spans even if a tracer
	// is otherwise configured.
	EnableTracing bool

	// TracesOTLPEndpoint is the gRPC endpoint for the OTLP trace
	// exporter (for example "otel-collector:4317"). When empty,
	// Init falls back to OTEL_EXPORTER_OTLP_TRACES_ENDPOINT and
	// then OTEL_EXPORTER_OTLP_ENDPOINT from the environment.
	// If none resolve, Init returns an error when EnableTracing
	// is true.
	TracesOTLPEndpoint string

	// TracesOTLPInsecure sends spans over plaintext gRPC when
	// true; otherwise TLS is used (the gRPC system roots).
	TracesOTLPInsecure bool

	// SamplingRatio controls the parent-based root sampler. A
	// nil pointer (the default) keeps OTEL's default of always
	// sampling root spans. 0.0 disables sampling entirely;
	// 1.0 samples every root span.
	SamplingRatio *float64

	// Sampler, when non-nil, overrides SamplingRatio. Escape
	// hatch for callers that need a fully custom sampler.
	Sampler sdktrace.Sampler

	// Propagator, when non-nil, overrides the default W3C
	// tracecontext+baggage propagator.
	Propagator propagation.TextMapPropagator

	// MetricsMode selects between Prometheus scrape exposition
	// and OTLP push. Required; zero value is rejected.
	MetricsMode MetricsMode

	// MetricsOTLPEndpoint is the gRPC endpoint for the OTLP
	// metric exporter. Used only when MetricsMode is
	// MetricsModeOTLP. Falls back to
	// OTEL_EXPORTER_OTLP_METRICS_ENDPOINT and then
	// OTEL_EXPORTER_OTLP_ENDPOINT like the trace endpoint.
	MetricsOTLPEndpoint string

	// MetricsOTLPInsecure sends metrics over plaintext gRPC
	// when true.
	MetricsOTLPInsecure bool

	// MetricsPushInterval controls how often the OTLP periodic
	// reader pushes. Defaults to 30s when zero. Ignored in
	// Prometheus mode.
	MetricsPushInterval time.Duration

	// AllowUndeclaredLabels relaxes strict-label enforcement on
	// custom metrics. When false (default) observations with
	// label keys not declared in MetricDefinition.Labels are
	// dropped and a one-shot warning is logged.
	AllowUndeclaredLabels bool

	// EnableRuntimeMetrics registers Go runtime metrics
	// (GC stats, goroutines, memstats) on Init. Defaults to
	// true via DefaultConfig.
	EnableRuntimeMetrics bool

	// EnableProcessMetrics registers process metrics (CPU, RSS,
	// open file descriptors) on Init. Defaults to true via
	// DefaultConfig. On platforms where these metrics are
	// unavailable the registration is silently skipped.
	EnableProcessMetrics bool

	// MetricsNamespace, when non-empty, is prefixed to every
	// custom metric name registered through Observer.Counter,
	// Observer.Histogram, and so on. The built-in magic_*
	// metrics are unaffected.
	MetricsNamespace string
}

// DefaultConfig returns a Config with sensible defaults. Callers
// only need to supply ServiceName and MetricsMode (and, when
// using OTLP, the endpoints).
func DefaultConfig() Config {
	return Config{
		EnableRuntimeMetrics: true,
		EnableProcessMetrics: true,
		MetricsPushInterval:  30 * time.Second,
	}
}

// metricNameRE is the Prometheus / OTLP metric name allow-list.
// Matches the spec: [a-zA-Z_:][a-zA-Z0-9_:]*
var metricNameRE = regexp.MustCompile(`^[a-zA-Z_:][a-zA-Z0-9_:]*$`)

// labelNameRE is the Prometheus label name allow-list. Matches
// the spec: [a-zA-Z_][a-zA-Z0-9_]* and disallows names beginning
// with "__" which are reserved for internal use.
var labelNameRE = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// validate checks the config for fatal misconfigurations. It does
// not mutate the receiver; callers that rely on default values
// should compose with DefaultConfig.
func (c Config) validate() error {
	if c.ServiceName == "" {
		return fmt.Errorf("observability: ServiceName is required")
	}
	if !c.MetricsMode.Valid() {
		return fmt.Errorf("observability: invalid MetricsMode %q (expected %q or %q)",
			c.MetricsMode, MetricsModePrometheus, MetricsModeOTLP)
	}
	if c.SamplingRatio != nil {
		r := *c.SamplingRatio
		if r < 0 || r > 1 {
			return fmt.Errorf("observability: SamplingRatio %.3f out of range [0,1]", r)
		}
	}
	if c.MetricsPushInterval < 0 {
		return fmt.Errorf("observability: MetricsPushInterval must be >= 0")
	}
	if c.MetricsNamespace != "" && !metricNameRE.MatchString(c.MetricsNamespace) {
		return fmt.Errorf("observability: MetricsNamespace %q is not a valid metric name prefix", c.MetricsNamespace)
	}
	return nil
}
