package pubsub

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/tink3rlabs/magic/telemetry"
)

// ContextualPublisher is an extension interface that adds a
// context-aware sibling to Publisher.Publish. Publishers shipped
// in the magic repository implement both interfaces; the
// non-Context method delegates to PublishContext with
// context.Background(). Third-party publishers are free to
// implement only Publisher, in which case the instrumented
// wrapper falls back to metrics-only coverage.
//
// PublishContext receives the caller's context so that:
//
//   - the SDK honors the context's deadline / cancellation
//   - the wrapper can start a child span on that context
//   - trace propagation headers produced from the span context
//     are injected into the outbound message metadata via the
//     standard propagator
type ContextualPublisher interface {
	Publisher
	PublishContext(ctx context.Context, topic, message string, params map[string]any) error
}

// Metric + label names, kept in sync with
// observability/builtins.go. Redeclared here to avoid an import
// cycle (observability depends on pubsub transitively through
// the instrumented wrapper's lookup of telemetry.Global()).
const (
	metricPubSubMessagesTotal          = "magic_pubsub_messages_total"
	metricPubSubPublishDurationSeconds = "magic_pubsub_publish_duration_seconds"
	metricPubSubErrorsTotal            = "magic_pubsub_errors_total"

	labelProvider    = "provider"
	labelDestination = "destination"
	labelOperation   = "operation"
	labelStatus      = "status"

	statusOK    = "ok"
	statusError = "error"

	opPublish = "publish"

	// MessageAttributesParamKey is the agreed-upon params map
	// key that carries message-level metadata between the
	// instrumented wrapper and the in-repo publishers. The
	// value type is map[string]string; publishers convert
	// those string pairs into their native per-system
	// representation (e.g. SNS MessageAttributeValue with
	// DataType="String"). Callers may also populate this key
	// directly to supply their own attributes.
	MessageAttributesParamKey = "MessageAttributes"

	// maxPublishAttributes is the per-message cap enforced by
	// SNS (and many comparable systems) on MessageAttributes.
	// The wrapper uses this cap when merging propagator keys
	// so it never pushes a message over the provider limit.
	maxPublishAttributes = 10
)

// pubsubDurationBuckets mirrors observability.pubsubDurationBuckets
// (the Prometheus default set). It must match the shape registered
// by observability so a Prometheus registry does not reject a
// re-registration with a different bucket set.
var pubsubDurationBuckets = []float64{
	0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10,
}

// instrumentedPublisher wraps a Publisher with tracing and
// metrics. It implements ContextualPublisher so callers always
// have access to the context-aware method; when the underlying
// publisher does not implement ContextualPublisher, spans are
// skipped (to avoid orphan root spans) but metrics are still
// recorded from wall-clock timing.
type instrumentedPublisher struct {
	inner    Publisher
	ctxInner ContextualPublisher
	provider string
	telem    *telemetry.Telemetry

	messagesTotal  telemetry.Counter
	errorsTotal    telemetry.Counter
	publishLatency telemetry.Histogram
}

// wrapForTelemetry returns an instrumented Publisher that records
// built-in pubsub metrics and, when the underlying publisher is
// contextual, emits producer spans with W3C-propagated trace
// context in outbound message metadata.
//
// provider should be the PublisherType string (e.g. "sns") so
// metrics and span attributes are labeled with the concrete
// publisher kind. It is always safe to call: if telemetry has
// not been initialized the wrapper uses no-op instruments and
// adds negligible overhead.
func wrapForTelemetry(inner Publisher, provider string) Publisher {
	if inner == nil {
		return nil
	}
	if _, ok := inner.(*instrumentedPublisher); ok {
		return inner
	}

	w := &instrumentedPublisher{
		inner:    inner,
		provider: provider,
		telem:    telemetry.Global(),
	}
	if c, ok := inner.(ContextualPublisher); ok {
		w.ctxInner = c
	} else {
		telemetry.WarnOnce(
			fmt.Sprintf("pubsub.legacy-publisher:%T", inner),
			"pubsub publisher does not implement ContextualPublisher; traces will not be linked (metrics-only coverage)",
			slog.String("publisher_type", fmt.Sprintf("%T", inner)),
		)
	}

	w.registerInstruments()
	return w
}

// registerInstruments looks up (or creates, idempotently) the
// built-in pubsub instruments. Registration failures are logged
// and the instruments fall back to no-ops so instrumentation
// never breaks real publish calls.
func (w *instrumentedPublisher) registerInstruments() {
	labels := []string{labelProvider, labelDestination, labelOperation, labelStatus}
	durationLabels := []string{labelProvider, labelDestination, labelOperation}
	errorLabels := []string{labelProvider, labelDestination, labelOperation}

	if c, err := w.telem.Metrics.Counter(telemetry.MetricDefinition{
		Name:   metricPubSubMessagesTotal,
		Help:   "Total pubsub messages published, labeled by provider, destination, operation, and final status.",
		Kind:   telemetry.KindCounter,
		Labels: labels,
	}); err == nil {
		w.messagesTotal = c
	} else {
		slog.Warn("pubsub: failed to register messages counter", "error", err)
	}

	if h, err := w.telem.Metrics.Histogram(telemetry.MetricDefinition{
		Name:    metricPubSubPublishDurationSeconds,
		Help:    "Pubsub publish wall-clock duration in seconds, from instrumentation entry to publisher return.",
		Unit:    telemetry.UnitSeconds,
		Kind:    telemetry.KindHistogram,
		Labels:  durationLabels,
		Buckets: pubsubDurationBuckets,
	}); err == nil {
		w.publishLatency = h
	} else {
		slog.Warn("pubsub: failed to register duration histogram", "error", err)
	}

	if c, err := w.telem.Metrics.Counter(telemetry.MetricDefinition{
		Name:   metricPubSubErrorsTotal,
		Help:   "Total pubsub publish attempts that returned an error, labeled by provider, destination, and operation.",
		Kind:   telemetry.KindCounter,
		Labels: errorLabels,
	}); err == nil {
		w.errorsTotal = c
	} else {
		slog.Warn("pubsub: failed to register errors counter", "error", err)
	}
}

// tracer returns the tracer to use for this wrapper, or nil when
// spans should be skipped. Legacy (non-contextual) publishers
// have no way to receive a parent span context, so emitting spans
// against them would produce orphan roots that pollute trace UIs.
func (w *instrumentedPublisher) tracer() trace.Tracer {
	if w.ctxInner == nil {
		return nil
	}
	if w.telem == nil || w.telem.Tracer == nil {
		return nil
	}
	return w.telem.Tracer
}

// Publish delegates to PublishContext with context.Background so
// legacy callers still participate in metrics (but not in any
// parent trace, because there is no context to extract one from).
func (w *instrumentedPublisher) Publish(topic, message string, params map[string]any) error {
	return w.PublishContext(context.Background(), topic, message, params)
}

// PublishContext starts a producer span (when tracing is
// available), injects W3C trace context into outbound message
// attributes, calls the underlying publisher, records wall-clock
// duration, and reports the outcome as ok/error on both the span
// and the built-in metrics.
func (w *instrumentedPublisher) PublishContext(ctx context.Context, topic, message string, params map[string]any) (err error) {
	start := time.Now()
	var span trace.Span

	if t := w.tracer(); t != nil {
		ctx, span = t.Start(ctx, "pubsub.publish",
			trace.WithSpanKind(trace.SpanKindProducer),
			trace.WithAttributes(
				semconv.MessagingSystemKey.String(w.provider),
				semconv.MessagingDestinationName(topic),
				attribute.String("messaging.operation", opPublish),
				attribute.String("magic.pubsub.provider", w.provider),
				attribute.Int("messaging.message.body.size", len(message)),
			),
		)
		// Inject trace context into the outbound message
		// metadata so the downstream consumer can link to
		// this publish span. Only happens when we actually
		// produced a span so legacy paths stay untouched.
		params = injectTraceContext(ctx, params)
	}

	defer func() {
		status := statusOK
		if err != nil {
			status = statusError
			if span != nil {
				span.RecordError(err)
				span.SetStatus(codes.Error, err.Error())
			}
		}
		if span != nil {
			span.End()
		}

		providerDestOp := []telemetry.Label{
			{Key: labelProvider, Value: w.provider},
			{Key: labelDestination, Value: topic},
			{Key: labelOperation, Value: opPublish},
		}
		if w.messagesTotal != nil {
			w.messagesTotal.Add(1,
				telemetry.Label{Key: labelProvider, Value: w.provider},
				telemetry.Label{Key: labelDestination, Value: topic},
				telemetry.Label{Key: labelOperation, Value: opPublish},
				telemetry.Label{Key: labelStatus, Value: status},
			)
		}
		if w.publishLatency != nil {
			w.publishLatency.Observe(time.Since(start).Seconds(), providerDestOp...)
		}
		if status == statusError && w.errorsTotal != nil {
			w.errorsTotal.Add(1, providerDestOp...)
		}
	}()

	if w.ctxInner != nil {
		return w.ctxInner.PublishContext(ctx, topic, message, params)
	}
	return w.inner.Publish(topic, message, params)
}

// injectTraceContext merges propagator-injected trace keys into
// params[MessageAttributesParamKey] without overwriting
// user-supplied keys. It enforces the provider-level
// MessageAttributes cap (maxPublishAttributes) by dropping the
// lowest-priority propagation headers first: baggage, then
// tracestate. traceparent is never dropped — if the cap would
// still be exceeded after dropping those two, the wrapper logs a
// warn-once and leaves user attributes untouched (traceparent is
// skipped in that degenerate case).
func injectTraceContext(ctx context.Context, params map[string]any) map[string]any {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if len(carrier) == 0 {
		return params
	}

	if params == nil {
		params = map[string]any{}
	}

	// Start from a copy of whatever the caller already placed
	// in MessageAttributes so their keys remain authoritative.
	merged := map[string]string{}
	if existing, ok := params[MessageAttributesParamKey].(map[string]string); ok {
		for k, v := range existing {
			merged[k] = v
		}
	}

	// Inject propagator keys in priority order:
	// traceparent > tracestate > baggage.
	priorities := []string{"traceparent", "tracestate", "baggage"}
	for _, key := range priorities {
		v, ok := carrier[key]
		if !ok {
			continue
		}
		if _, userHas := merged[key]; userHas {
			continue
		}
		if len(merged) >= maxPublishAttributes {
			// Couldn't fit this one; remaining
			// lower-priority keys won't fit either.
			telemetry.WarnOnce(
				"pubsub.attribute-limit-dropped:"+key,
				"pubsub message attribute limit reached; dropping propagation header",
				slog.String("dropped_header", key),
				slog.Int("limit", maxPublishAttributes),
			)
			break
		}
		merged[key] = v
	}

	params[MessageAttributesParamKey] = merged
	return params
}
