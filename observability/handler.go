package observability

import (
	"encoding/json"
	"net/http"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// MetricsHandler returns the HTTP handler that should be mounted
// on the service's metrics endpoint.
//
// In MetricsModePrometheus the handler produces the standard
// Prometheus text exposition over the Observer's registry.
//
// In MetricsModeOTLP metrics are pushed to the collector out of
// band, so the handler returns a 404 with a JSON body explaining
// the situation. Crucially it does not return nil: chi would
// otherwise panic during routing. Callers can safely mount it
// unconditionally and treat the 404 as "not applicable".
func (o *Observer) MetricsHandler() http.Handler {
	if o == nil {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "observability not initialized", http.StatusServiceUnavailable)
		})
	}
	if o.cfg.MetricsMode == MetricsModePrometheus && o.promRegistry != nil {
		return promhttp.HandlerFor(o.promRegistry, promhttp.HandlerOpts{
			Registry:          o.promRegistry,
			EnableOpenMetrics: true,
		})
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status":  "not_found",
			"message": "metrics are exported via OTLP push; there is no scrape endpoint on this service",
		})
	})
}
