package obstest

import (
	"fmt"

	sdktrace "go.opentelemetry.io/otel/sdk/trace"

	"github.com/tink3rlabs/magic/telemetry"
)

// TestingTB is the minimal subset of testing.TB the assertion
// helpers depend on. Declared locally so obstest does not depend
// on the full *testing.T surface and can be driven from a test
// stub when exercising the failure path of an assertion.
type TestingTB interface {
	Helper()
	Errorf(format string, args ...any)
	Fatalf(format string, args ...any)
}

// AssertCounter fails the test when the named counter's value
// under the given labels does not equal want. Fatal on mismatch
// so the caller's subsequent assertions do not cascade.
func (o *TestObserver) AssertCounter(tb TestingTB, name string, want float64, labels ...telemetry.Label) {
	tb.Helper()
	got := o.Metrics.CounterValue(name, labels...)
	if got != want {
		tb.Fatalf("counter %s%s = %v; want %v\n%s",
			name, formatLabels(labels), got, want, o.Metrics)
	}
}

// AssertHistogramObserved fails the test when the named histogram
// has zero observations under the given labels. Returns the
// observation count so callers can make finer assertions on the
// same line.
func (o *TestObserver) AssertHistogramObserved(tb TestingTB, name string, labels ...telemetry.Label) int {
	tb.Helper()
	n := o.Metrics.HistogramCount(name, labels...)
	if n == 0 {
		tb.Fatalf("histogram %s%s recorded 0 observations; want > 0\n%s",
			name, formatLabels(labels), o.Metrics)
	}
	return n
}

// AssertHistogramCount fails the test when the named histogram's
// observation count under the given labels does not equal want.
func (o *TestObserver) AssertHistogramCount(tb TestingTB, name string, want int, labels ...telemetry.Label) {
	tb.Helper()
	got := o.Metrics.HistogramCount(name, labels...)
	if got != want {
		tb.Fatalf("histogram %s%s count = %d; want %d\n%s",
			name, formatLabels(labels), got, want, o.Metrics)
	}
}

// AssertGauge fails the test when the named gauge's latest Set
// value under the given labels does not equal want.
func (o *TestObserver) AssertGauge(tb TestingTB, name string, want float64, labels ...telemetry.Label) {
	tb.Helper()
	got := o.Metrics.GaugeValue(name, labels...)
	if got != want {
		tb.Fatalf("gauge %s%s = %v; want %v\n%s",
			name, formatLabels(labels), got, want, o.Metrics)
	}
}

// AssertUpDownCounter fails the test when the named up/down
// counter's cumulative value under the given labels does not
// equal want.
func (o *TestObserver) AssertUpDownCounter(tb TestingTB, name string, want float64, labels ...telemetry.Label) {
	tb.Helper()
	got := o.Metrics.UpDownValue(name, labels...)
	if got != want {
		tb.Fatalf("updowncounter %s%s = %v; want %v\n%s",
			name, formatLabels(labels), got, want, o.Metrics)
	}
}

// AssertSpan returns the first recorded span with the given name.
// Fails the test when no matching span is found.
//
// Span equality is by name only; tests that need to distinguish
// multiple spans of the same name should inspect the returned
// span's attributes or iterate Spans.Ended() directly.
func (o *TestObserver) AssertSpan(tb TestingTB, name string) sdktrace.ReadOnlySpan {
	tb.Helper()
	for _, s := range o.Spans.Ended() {
		if s.Name() == name {
			return s
		}
	}
	tb.Fatalf("no span named %q was recorded; saw %v", name, spanNames(o.Spans.Ended()))
	return nil
}

// AssertNoSpan fails the test when any span with the given name
// was recorded. Useful for verifying that legacy (non-contextual)
// adapters skip span emission.
func (o *TestObserver) AssertNoSpan(tb TestingTB, name string) {
	tb.Helper()
	for _, s := range o.Spans.Ended() {
		if s.Name() == name {
			tb.Errorf("span %q was recorded but should not have been", name)
			return
		}
	}
}

// formatLabels renders a small {k="v", ...} suffix for assertion
// error messages. Returns an empty string when there are no
// labels, so name-only assertions read naturally.
func formatLabels(labels []telemetry.Label) string {
	if len(labels) == 0 {
		return ""
	}
	s := "{"
	for i, l := range labels {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf("%s=%q", l.Key, l.Value)
	}
	return s + "}"
}

func spanNames(spans []sdktrace.ReadOnlySpan) []string {
	out := make([]string, 0, len(spans))
	for _, s := range spans {
		out = append(out, s.Name())
	}
	return out
}
