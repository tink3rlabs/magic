package observability

import (
	"fmt"

	"github.com/tink3rlabs/magic/telemetry"
)

// registerPubSubMetrics creates and stores the built-in pubsub
// instruments on the Observer. It runs during Init, after the
// metrics backend is wired up but before telemetry.SetGlobal, so
// that the first message published through the instrumented
// publisher wrapper already has live instruments.
//
// The instruments themselves live on the metrics backend; the
// instrumented publisher wrapper looks them up through
// telemetry.Global() at wrap time. Keeping references on the
// Observer ensures dashboards see HELP text immediately after
// Init (Prometheus otherwise omits HELP until the first sample).
func (o *Observer) registerPubSubMetrics() error {
	labels := []string{LabelPubSubProvider, LabelPubSubDestination, LabelPubSubOperation, LabelPubSubStatus}
	durationLabels := []string{LabelPubSubProvider, LabelPubSubDestination, LabelPubSubOperation}
	errorLabels := []string{LabelPubSubProvider, LabelPubSubDestination, LabelPubSubOperation}

	c, err := o.telem.Metrics.Counter(telemetry.MetricDefinition{
		Name:   PubSubMessagesTotal,
		Help:   "Total pubsub messages published, labeled by provider, destination, operation, and final status.",
		Kind:   telemetry.KindCounter,
		Labels: labels,
	})
	if err != nil {
		return fmt.Errorf("observability: register %s: %w", PubSubMessagesTotal, err)
	}
	o.pubsubMessagesTotal = c

	h, err := o.telem.Metrics.Histogram(telemetry.MetricDefinition{
		Name:    PubSubPublishDurationSeconds,
		Help:    "Pubsub publish wall-clock duration in seconds, from instrumentation entry to publisher return.",
		Unit:    telemetry.UnitSeconds,
		Kind:    telemetry.KindHistogram,
		Labels:  durationLabels,
		Buckets: pubsubDurationBuckets,
	})
	if err != nil {
		return fmt.Errorf("observability: register %s: %w", PubSubPublishDurationSeconds, err)
	}
	o.pubsubPublishDuration = h

	e, err := o.telem.Metrics.Counter(telemetry.MetricDefinition{
		Name:   PubSubErrorsTotal,
		Help:   "Total pubsub publish attempts that returned an error, labeled by provider, destination, and operation.",
		Kind:   telemetry.KindCounter,
		Labels: errorLabels,
	})
	if err != nil {
		return fmt.Errorf("observability: register %s: %w", PubSubErrorsTotal, err)
	}
	o.pubsubErrorsTotal = e

	return nil
}
