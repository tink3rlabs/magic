package pubsub

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"

	"github.com/tink3rlabs/magic/observability/obstest"
	"github.com/tink3rlabs/magic/telemetry"
)

// These tests live in the pubsub package (rather than pubsub_test)
// so they can reach the unexported wrapForTelemetry helper. The
// factory-level test (TestPublisherFactoryWrapsSNSForTelemetry)
// still exercises the public entry point.

// fakeCtxPublisher captures every call so tests can assert what
// the wrapper passed through, and drives error paths via an
// optional error hook. It implements both Publisher and
// ContextualPublisher.
type fakeCtxPublisher struct {
	mu     sync.Mutex
	calls  []fakePublishCall
	retErr error
}

type fakePublishCall struct {
	ctx     context.Context
	topic   string
	message string
	params  map[string]any
}

func (f *fakeCtxPublisher) Publish(topic, message string, params map[string]any) error {
	return f.PublishContext(context.Background(), topic, message, params)
}

func (f *fakeCtxPublisher) PublishContext(ctx context.Context, topic, message string, params map[string]any) error {
	f.mu.Lock()
	f.calls = append(f.calls, fakePublishCall{
		ctx: ctx, topic: topic, message: message, params: params,
	})
	err := f.retErr
	f.mu.Unlock()
	return err
}

func (f *fakeCtxPublisher) lastCall(t *testing.T) fakePublishCall {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) == 0 {
		t.Fatalf("expected at least one publish call")
	}
	return f.calls[len(f.calls)-1]
}

// legacyPublisher only implements Publisher (no PublishContext).
// The wrapper must still record metrics against it while skipping
// span emission (legacy path).
type legacyPublisher struct {
	calls int
}

func (l *legacyPublisher) Publish(topic, message string, params map[string]any) error {
	l.calls++
	return nil
}

// setup installs a TestObserver and a W3C TraceContext propagator
// on the OTEL global so the wrapper's Inject actually populates
// carrier keys. The caller is responsible for nothing — cleanup
// is wired via t.Cleanup.
func setupObserver(t *testing.T) *obstest.TestObserver {
	t.Helper()
	obs := obstest.NewTestObserver(t)

	// Install a real propagator; the OTEL default is a no-op
	// composite that silently skips Inject.
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTextMapPropagator(prev) })
	return obs
}

func TestPublisherFactoryWrapsSNSForTelemetry(t *testing.T) {
	// Factory goes through the public entry point.
	// GetSNSPublisher loads AWS config but never opens a
	// network connection; the test only asserts the wrapper
	// is applied (observable through the ContextualPublisher
	// extension interface that the wrapper always satisfies).
	p, err := PublisherFactory{}.GetInstance(SNS, map[string]string{"region": "us-east-1"})
	if err != nil {
		t.Fatalf("GetInstance(SNS): %v", err)
	}
	if _, ok := p.(ContextualPublisher); !ok {
		t.Fatalf("expected factory to return a ContextualPublisher; got %T", p)
	}
}

func TestPublishContextEmitsSpanAndMetricsOnSuccess(t *testing.T) {
	obs := setupObserver(t)
	inner := &fakeCtxPublisher{}
	pub := wrapForTelemetry(inner, "fake").(ContextualPublisher)

	if err := pub.PublishContext(context.Background(), "orders", "payload", nil); err != nil {
		t.Fatalf("PublishContext: %v", err)
	}

	spans := obs.Spans.Ended()
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	sp := spans[0]
	if sp.Name() != "pubsub.publish" {
		t.Fatalf("span name = %q; want pubsub.publish", sp.Name())
	}
	if sp.Status().Code == codes.Error {
		t.Fatalf("span status = Error on success path")
	}

	successLabels := []telemetry.Label{
		{Key: "provider", Value: "fake"},
		{Key: "destination", Value: "orders"},
		{Key: "operation", Value: "publish"},
		{Key: "status", Value: "ok"},
	}
	if got := obs.Metrics.CounterValue("magic_pubsub_messages_total", successLabels...); got != 1 {
		t.Fatalf("messages_total{ok} = %v; want 1", got)
	}

	durLabels := []telemetry.Label{
		{Key: "provider", Value: "fake"},
		{Key: "destination", Value: "orders"},
		{Key: "operation", Value: "publish"},
	}
	if got := obs.Metrics.HistogramCount("magic_pubsub_publish_duration_seconds", durLabels...); got != 1 {
		t.Fatalf("duration histogram count = %d; want 1", got)
	}
	if got := obs.Metrics.CounterValue("magic_pubsub_errors_total", durLabels...); got != 0 {
		t.Fatalf("errors_total on success = %v; want 0", got)
	}
}

func TestPublishContextRecordsErrorStatus(t *testing.T) {
	obs := setupObserver(t)
	boom := errors.New("sns exploded")
	inner := &fakeCtxPublisher{retErr: boom}
	pub := wrapForTelemetry(inner, "fake").(ContextualPublisher)

	err := pub.PublishContext(context.Background(), "orders", "payload", nil)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v; want boom", err)
	}

	spans := obs.Spans.Ended()
	if len(spans) != 1 {
		t.Fatalf("want 1 span, got %d", len(spans))
	}
	if code := spans[0].Status().Code; code != codes.Error {
		t.Fatalf("span status = %v; want Error", code)
	}
	if len(spans[0].Events()) == 0 {
		t.Fatalf("expected span to record an error event")
	}

	errorLabels := []telemetry.Label{
		{Key: "provider", Value: "fake"},
		{Key: "destination", Value: "orders"},
		{Key: "operation", Value: "publish"},
	}
	if got := obs.Metrics.CounterValue("magic_pubsub_errors_total", errorLabels...); got != 1 {
		t.Fatalf("errors_total = %v; want 1", got)
	}
	errStatus := append([]telemetry.Label{}, errorLabels...)
	errStatus = append(errStatus, telemetry.Label{Key: "status", Value: "error"})
	if got := obs.Metrics.CounterValue("magic_pubsub_messages_total", errStatus...); got != 1 {
		t.Fatalf("messages_total{error} = %v; want 1", got)
	}
}

func TestPublishContextInjectsTraceContextIntoMessageAttributes(t *testing.T) {
	setupObserver(t)
	inner := &fakeCtxPublisher{}
	pub := wrapForTelemetry(inner, "fake").(ContextualPublisher)

	if err := pub.PublishContext(context.Background(), "orders", "body", nil); err != nil {
		t.Fatalf("PublishContext: %v", err)
	}

	call := inner.lastCall(t)
	attrsRaw, ok := call.params[MessageAttributesParamKey]
	if !ok {
		t.Fatalf("expected params[%q] to be populated", MessageAttributesParamKey)
	}
	attrs, ok := attrsRaw.(map[string]string)
	if !ok {
		t.Fatalf("MessageAttributes = %T; want map[string]string", attrsRaw)
	}
	tp, ok := attrs["traceparent"]
	if !ok {
		t.Fatalf("missing traceparent in %v", attrs)
	}
	// W3C shape: "00-<trace>-<span>-<flags>".
	if !strings.HasPrefix(tp, "00-") || strings.Count(tp, "-") != 3 {
		t.Fatalf("traceparent %q does not look W3C-shaped", tp)
	}
}

func TestPublishContextDoesNotOverwriteUserSuppliedTraceparent(t *testing.T) {
	setupObserver(t)
	inner := &fakeCtxPublisher{}
	pub := wrapForTelemetry(inner, "fake").(ContextualPublisher)

	userAttrs := map[string]string{
		"traceparent": "00-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa-bbbbbbbbbbbbbbbb-01",
	}
	params := map[string]any{MessageAttributesParamKey: userAttrs}
	if err := pub.PublishContext(context.Background(), "orders", "body", params); err != nil {
		t.Fatalf("PublishContext: %v", err)
	}

	call := inner.lastCall(t)
	attrs := call.params[MessageAttributesParamKey].(map[string]string)
	if attrs["traceparent"] != userAttrs["traceparent"] {
		t.Fatalf("traceparent was overwritten: got %q; want %q",
			attrs["traceparent"], userAttrs["traceparent"])
	}
}

func TestPublishContextDropsPropagatorKeysWhenAttributeLimitReached(t *testing.T) {
	setupObserver(t)
	inner := &fakeCtxPublisher{}
	pub := wrapForTelemetry(inner, "fake").(ContextualPublisher)

	// Pre-fill user attrs to the provider cap so no
	// propagator key can fit without dropping something.
	userAttrs := map[string]string{}
	for i := 0; i < 10; i++ {
		userAttrs[string(rune('a'+i))] = "v"
	}
	params := map[string]any{MessageAttributesParamKey: userAttrs}

	if err := pub.PublishContext(context.Background(), "orders", "body", params); err != nil {
		t.Fatalf("PublishContext: %v", err)
	}

	call := inner.lastCall(t)
	attrs := call.params[MessageAttributesParamKey].(map[string]string)
	if len(attrs) != 10 {
		t.Fatalf("attrs len = %d; want 10 (cap preserved)", len(attrs))
	}
	if _, has := attrs["traceparent"]; has {
		t.Fatalf("traceparent must be dropped when cap would be exceeded by user keys")
	}
}

func TestLegacyPublisherStillRecordsMetricsAndSkipsSpans(t *testing.T) {
	obs := setupObserver(t)

	legacy := &legacyPublisher{}
	pub := wrapForTelemetry(legacy, "legacy")

	if err := pub.Publish("orders", "payload", nil); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if legacy.calls != 1 {
		t.Fatalf("legacy.Publish call count = %d; want 1", legacy.calls)
	}
	if got := len(obs.Spans.Ended()); got != 0 {
		t.Fatalf("want 0 spans for legacy publisher, got %d", got)
	}
	okLabels := []telemetry.Label{
		{Key: "provider", Value: "legacy"},
		{Key: "destination", Value: "orders"},
		{Key: "operation", Value: "publish"},
		{Key: "status", Value: "ok"},
	}
	if got := obs.Metrics.CounterValue("magic_pubsub_messages_total", okLabels...); got != 1 {
		t.Fatalf("messages_total = %v; want 1", got)
	}
}

func TestPublishNonContextDelegatesAndRecordsMetrics(t *testing.T) {
	obs := setupObserver(t)
	inner := &fakeCtxPublisher{}
	pub := wrapForTelemetry(inner, "fake")

	if err := pub.Publish("orders", "payload", nil); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if n := len(inner.calls); n != 1 {
		t.Fatalf("inner called %d times; want 1", n)
	}
	// Background delegation still goes through the span path
	// because the inner is contextual. The caller just forfeits
	// any parent-trace linkage.
	if got := len(obs.Spans.Ended()); got != 1 {
		t.Fatalf("want 1 span, got %d", got)
	}
}

func TestWrapForTelemetryNilInputReturnsNil(t *testing.T) {
	if got := wrapForTelemetry(nil, "fake"); got != nil {
		t.Fatalf("wrap(nil) = %v; want nil", got)
	}
}

func TestWrapForTelemetryDoesNotDoubleWrap(t *testing.T) {
	setupObserver(t)
	inner := &fakeCtxPublisher{}
	once := wrapForTelemetry(inner, "fake")
	twice := wrapForTelemetry(once, "fake")
	if once != twice {
		t.Fatalf("re-wrap produced a new wrapper; want identity")
	}
}

func TestInjectTraceContextWithNoActiveSpanIsNoop(t *testing.T) {
	// Without a span on the context the TraceContext
	// propagator injects nothing, so params must be returned
	// unchanged (no MessageAttributes key introduced).
	otel.SetTextMapPropagator(propagation.TraceContext{})

	ctx := context.Background()
	before := map[string]any{"groupId": "g1"}
	after := injectTraceContext(ctx, before)
	if _, has := after[MessageAttributesParamKey]; has {
		t.Fatalf("expected no MessageAttributes to be injected; got %v", after)
	}
}
