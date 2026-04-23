package observability

import (
	"fmt"

	"github.com/tink3rlabs/magic/telemetry"
)

// registerHTTPMetrics creates and stores the built-in HTTP
// instruments on the Observer. It runs during Init, after the
// metrics backend is wired up but before telemetry.SetGlobal, so
// that the first request served by middlewares.Observability
// already has live instruments.
func (o *Observer) registerHTTPMetrics() error {
	methodRouteStatus := []string{LabelHTTPMethod, LabelHTTPRoute, LabelHTTPStatusCode}
	methodRoute := []string{LabelHTTPMethod, LabelHTTPRoute}

	c, err := o.telem.Metrics.Counter(telemetry.MetricDefinition{
		Name:   HTTPRequestsTotal,
		Help:   "Total HTTP requests processed, labeled by method, route template, and response status code.",
		Kind:   telemetry.KindCounter,
		Labels: methodRouteStatus,
	})
	if err != nil {
		return fmt.Errorf("observability: register %s: %w", HTTPRequestsTotal, err)
	}
	o.httpRequestsTotal = c

	h, err := o.telem.Metrics.Histogram(telemetry.MetricDefinition{
		Name:    HTTPRequestDurationSeconds,
		Help:    "HTTP request wall-clock duration in seconds, from the middleware entry to the final response write.",
		Unit:    telemetry.UnitSeconds,
		Kind:    telemetry.KindHistogram,
		Labels:  methodRouteStatus,
		Buckets: httpDurationBuckets,
	})
	if err != nil {
		return fmt.Errorf("observability: register %s: %w", HTTPRequestDurationSeconds, err)
	}
	o.httpRequestDuration = h

	rs, err := o.telem.Metrics.Histogram(telemetry.MetricDefinition{
		Name:   HTTPRequestSizeBytes,
		Help:   "HTTP request body size in bytes as reported by Content-Length. Requests without a valid Content-Length contribute a 0 observation.",
		Unit:   telemetry.UnitBytes,
		Kind:   telemetry.KindHistogram,
		Labels: methodRoute,
	})
	if err != nil {
		return fmt.Errorf("observability: register %s: %w", HTTPRequestSizeBytes, err)
	}
	o.httpRequestSize = rs

	resp, err := o.telem.Metrics.Histogram(telemetry.MetricDefinition{
		Name:   HTTPResponseSizeBytes,
		Help:   "HTTP response body size in bytes written to the client (excluding headers).",
		Unit:   telemetry.UnitBytes,
		Kind:   telemetry.KindHistogram,
		Labels: methodRouteStatus,
	})
	if err != nil {
		return fmt.Errorf("observability: register %s: %w", HTTPResponseSizeBytes, err)
	}
	o.httpResponseSize = resp

	inflight, err := o.telem.Metrics.UpDownCounter(telemetry.MetricDefinition{
		Name:   HTTPRequestsInFlight,
		Help:   "Number of HTTP requests currently being served.",
		Unit:   telemetry.UnitDimensionless,
		Kind:   telemetry.KindUpDownCounter,
		Labels: methodRoute,
	})
	if err != nil {
		return fmt.Errorf("observability: register %s: %w", HTTPRequestsInFlight, err)
	}
	o.httpRequestsInFlight = inflight

	return nil
}
