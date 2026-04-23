package observability

import (
	"fmt"

	"github.com/tink3rlabs/magic/telemetry"
)

// registerStorageMetrics creates and stores the built-in storage
// instruments on the Observer. It runs during Init, after the
// metrics backend is wired up but before telemetry.SetGlobal, so
// that the first operation served by the instrumented storage
// adapter already has live instruments.
//
// The instruments themselves live on the metrics backend; the
// instrumented storage wrapper looks them up through
// telemetry.Global() at wrap time. We keep references on the
// Observer as well so dashboards see HELP text immediately after
// Init (Prometheus otherwise omits HELP until the first sample).
func (o *Observer) registerStorageMetrics() error {
	labels := []string{LabelStorageProvider, LabelStorageOperation, LabelStorageStatus}
	providerOp := []string{LabelStorageProvider, LabelStorageOperation}

	c, err := o.telem.Metrics.Counter(telemetry.MetricDefinition{
		Name:   StorageOperationsTotal,
		Help:   "Total storage operations executed, labeled by provider, operation, and final status.",
		Kind:   telemetry.KindCounter,
		Labels: labels,
	})
	if err != nil {
		return fmt.Errorf("observability: register %s: %w", StorageOperationsTotal, err)
	}
	o.storageOpsTotal = c

	h, err := o.telem.Metrics.Histogram(telemetry.MetricDefinition{
		Name:    StorageOperationDurationSeconds,
		Help:    "Storage operation wall-clock duration in seconds, from instrumentation entry to adapter return.",
		Unit:    telemetry.UnitSeconds,
		Kind:    telemetry.KindHistogram,
		Labels:  providerOp,
		Buckets: storageDurationBuckets,
	})
	if err != nil {
		return fmt.Errorf("observability: register %s: %w", StorageOperationDurationSeconds, err)
	}
	o.storageOpDuration = h

	e, err := o.telem.Metrics.Counter(telemetry.MetricDefinition{
		Name:   StorageOperationErrorsTotal,
		Help:   "Total storage operations that returned an error, labeled by provider and operation.",
		Kind:   telemetry.KindCounter,
		Labels: providerOp,
	})
	if err != nil {
		return fmt.Errorf("observability: register %s: %w", StorageOperationErrorsTotal, err)
	}
	o.storageOpErrors = e

	return nil
}
