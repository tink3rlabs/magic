package observability

import "time"

// Names of the built-in metrics emitted by the magic core packages.
// These are stable public constants so callers can grep dashboards
// and alerts against canonical identifiers.
const (
	// HTTP (emitted by ChiMiddleware).
	HTTPRequestsTotal          = "http_requests_total"
	HTTPRequestDurationSeconds = "http_request_duration_seconds"
	HTTPRequestSizeBytes       = "http_request_size_bytes"
	HTTPResponseSizeBytes      = "http_response_size_bytes"
	HTTPRequestsInFlight       = "http_requests_in_flight"

	// Storage (emitted by instrumented storage adapters in Phase 2).
	StorageOperationsTotal          = "magic_storage_operations_total"
	StorageOperationDurationSeconds = "magic_storage_operation_duration_seconds"

	// PubSub (emitted by instrumented publishers in Phase 3).
	PubSubPublishTotal           = "magic_pubsub_publish_total"
	PubSubPublishDurationSeconds = "magic_pubsub_publish_duration_seconds"
)

// Bucket boundaries for built-in histograms. The HTTP and storage
// buckets extend below the Prometheus default (0.005) to capture
// sub-millisecond operations like in-memory storage hits.
var (
	httpDurationBuckets = []float64{
		0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
	}
	storageDurationBuckets = []float64{
		0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5,
	}
)

// Labels used by the built-in HTTP metrics. Declared here so the
// middleware and any documentation can reference canonical label
// keys.
const (
	LabelHTTPMethod     = "method"
	LabelHTTPRoute      = "route"
	LabelHTTPStatusCode = "status_code"
)

// defaultMetricsPushInterval matches the OTEL SDK default and is
// used when Config.MetricsPushInterval is the zero value.
const defaultMetricsPushInterval = 30 * time.Second
