package observability

import "time"

// Names of the built-in metrics emitted by the magic core packages.
// These are stable public constants so callers can grep dashboards
// and alerts against canonical identifiers.
//
// They remain plain string constants so values pass through to Prometheus
// and OTLP registration without conversion. A named string type (e.g.
// type BuiltInMetricName string) would need to propagate through
// telemetry APIs and all call sites; that is left for a deliberate
// future refactor if stronger typing is required.
const (
	// HTTP (emitted by middlewares.Observability).
	HTTPRequestsTotal          = "http_requests_total"
	HTTPRequestDurationSeconds = "http_request_duration_seconds"
	HTTPRequestSizeBytes       = "http_request_size_bytes"
	HTTPResponseSizeBytes      = "http_response_size_bytes"
	HTTPRequestsInFlight       = "http_requests_in_flight"

	// Storage (emitted by instrumented storage adapters in Phase 2).
	StorageOperationsTotal          = "magic_storage_operations_total"
	StorageOperationDurationSeconds = "magic_storage_operation_duration_seconds"
	StorageOperationErrorsTotal     = "magic_storage_operation_errors_total"

	// PubSub (emitted by instrumented publishers in Phase 3).
	PubSubMessagesTotal          = "magic_pubsub_messages_total"
	PubSubPublishDurationSeconds = "magic_pubsub_publish_duration_seconds"
	PubSubErrorsTotal            = "magic_pubsub_errors_total"
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
	// pubsubDurationBuckets matches the Prometheus default
	// bucket set. Publish is network-bound and rarely faster
	// than a few milliseconds, so sub-millisecond resolution is
	// not needed.
	pubsubDurationBuckets = []float64{
		0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
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

// Labels used by the built-in storage metrics. Declared here so
// the instrumented storage wrapper and dashboards reference
// canonical label keys.
const (
	LabelStorageProvider  = "provider"
	LabelStorageOperation = "operation"
	LabelStorageStatus    = "status"
)

// Values used for the "status" label on storage metrics. Kept
// small and stable so operators can alert on a predictable
// low-cardinality set.
const (
	StorageStatusOK    = "ok"
	StorageStatusError = "error"
)

// Canonical storage operation names emitted as the "operation"
// label on storage metrics and as the span-name suffix for
// storage tracing ("storage.<op>"). Schema/migration methods run
// once at startup outside any request context and are
// deliberately not instrumented.
const (
	StorageOpCreate  = "create"
	StorageOpGet     = "get"
	StorageOpUpdate  = "update"
	StorageOpDelete  = "delete"
	StorageOpList    = "list"
	StorageOpSearch  = "search"
	StorageOpCount   = "count"
	StorageOpQuery   = "query"
	StorageOpExecute = "execute"
	StorageOpPing    = "ping"
)

// Labels used by the built-in pubsub metrics. Kept small and
// stable so operators can alert on a predictable low-cardinality
// set. See docs/observability.md for the cardinality note on
// `destination` (SNS topic ARNs embed the AWS account ID).
const (
	LabelPubSubProvider    = "provider"
	LabelPubSubDestination = "destination"
	LabelPubSubOperation   = "operation"
	LabelPubSubStatus      = "status"
)

// Values used for the "status" label on pubsub metrics.
const (
	PubSubStatusOK    = "ok"
	PubSubStatusError = "error"
)

// Canonical pubsub operation names. Only "publish" is supported
// in v1; consume/ack/nack are deferred until a Consumer interface
// lands in the pubsub package.
const (
	PubSubOpPublish = "publish"
)

// defaultMetricsPushInterval matches the OTEL SDK default and is
// used when Config.MetricsPushInterval is the zero value.
const defaultMetricsPushInterval = 30 * time.Second
