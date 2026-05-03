package observability

import (
	"testing"

	"github.com/tink3rlabs/magic/telemetry"
)

func TestProjectLabelsNoDeclaredLabels(t *testing.T) {
	def := telemetry.MetricDefinition{Name: "x", Kind: telemetry.KindCounter}

	got, err := projectLabels(def, true, nil)
	if err != nil || got != nil {
		t.Errorf("no labels, no input => nil: got (%v, %v)", got, err)
	}

	got, err = projectLabels(def, true, []telemetry.Label{{Key: "k", Value: "v"}})
	if err == nil {
		t.Error("strict mode should reject unexpected label when none declared")
	}
	_ = got
}

func TestProjectLabelsOrdersByDeclared(t *testing.T) {
	def := telemetry.MetricDefinition{
		Name:   "x",
		Kind:   telemetry.KindCounter,
		Labels: []string{"a", "b", "c"},
	}
	// Caller provides labels in arbitrary order.
	got, err := projectLabels(def, true, []telemetry.Label{
		{Key: "b", Value: "B"},
		{Key: "a", Value: "A"},
		{Key: "c", Value: "C"},
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	want := []string{"A", "B", "C"}
	if len(got) != 3 || got[0] != want[0] || got[1] != want[1] || got[2] != want[2] {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestProjectLabelsFillsMissingWithEmpty(t *testing.T) {
	def := telemetry.MetricDefinition{
		Name:   "x",
		Kind:   telemetry.KindCounter,
		Labels: []string{"a", "b"},
	}
	got, err := projectLabels(def, true, []telemetry.Label{{Key: "a", Value: "A"}})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(got) != 2 || got[0] != "A" || got[1] != "" {
		t.Errorf("missing label should be empty string, got %v", got)
	}
}

func TestProjectLabelsStrictRejectsUndeclared(t *testing.T) {
	def := telemetry.MetricDefinition{
		Name:   "x",
		Kind:   telemetry.KindCounter,
		Labels: []string{"a"},
	}
	_, err := projectLabels(def, true, []telemetry.Label{
		{Key: "a", Value: "A"},
		{Key: "unknown", Value: "X"},
	})
	if err == nil {
		t.Error("strict mode should reject undeclared labels")
	}
}

func TestProjectLabelsRelaxedIgnoresUndeclared(t *testing.T) {
	def := telemetry.MetricDefinition{
		Name:   "x",
		Kind:   telemetry.KindCounter,
		Labels: []string{"a"},
	}
	got, err := projectLabels(def, false, []telemetry.Label{
		{Key: "a", Value: "A"},
		{Key: "unknown", Value: "X"},
	})
	if err != nil {
		t.Fatalf("relaxed mode should silently drop unknowns, got err %v", err)
	}
	if len(got) != 1 || got[0] != "A" {
		t.Errorf("unexpected projection: %v", got)
	}
}
