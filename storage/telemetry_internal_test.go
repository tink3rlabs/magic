package storage

import (
	"context"
	"testing"

	"github.com/tink3rlabs/magic/observability/obstest"
	"github.com/tink3rlabs/magic/telemetry"
)

// legacyAdapter implements StorageAdapter but NOT
// ContextualStorageAdapter. It exercises the metrics-only fallback
// path in wrapForTelemetry.
type legacyAdapter struct {
	provider  StorageProviders
	createErr error
	calls     int
}

func (a *legacyAdapter) Execute(string) error                                { a.calls++; return nil }
func (a *legacyAdapter) Ping() error                                         { a.calls++; return nil }
func (a *legacyAdapter) GetType() StorageAdapterType                         { return "legacy" }
func (a *legacyAdapter) GetProvider() StorageProviders                       { return a.provider }
func (a *legacyAdapter) GetSchemaName() string                               { return "" }
func (a *legacyAdapter) CreateSchema() error                                 { a.calls++; return nil }
func (a *legacyAdapter) CreateMigrationTable() error                         { a.calls++; return nil }
func (a *legacyAdapter) UpdateMigrationTable(int, string, string) error      { a.calls++; return nil }
func (a *legacyAdapter) GetLatestMigration() (int, error)                    { a.calls++; return 0, nil }
func (a *legacyAdapter) Create(any, ...map[string]any) error                 { a.calls++; return a.createErr }
func (a *legacyAdapter) Get(any, map[string]any, ...map[string]any) error    { a.calls++; return nil }
func (a *legacyAdapter) Update(any, map[string]any, ...map[string]any) error { a.calls++; return nil }
func (a *legacyAdapter) Delete(any, map[string]any, ...map[string]any) error { a.calls++; return nil }
func (a *legacyAdapter) List(any, string, map[string]any, int, string, ...map[string]any) (string, error) {
	a.calls++
	return "", nil
}
func (a *legacyAdapter) Search(any, string, string, int, string, ...map[string]any) (string, error) {
	a.calls++
	return "", nil
}
func (a *legacyAdapter) Count(any, map[string]any, ...map[string]any) (int64, error) {
	a.calls++
	return 0, nil
}
func (a *legacyAdapter) Query(any, string, int, string, ...map[string]any) (string, error) {
	a.calls++
	return "", nil
}

func TestWrapForTelemetryNilInputReturnsNil(t *testing.T) {
	if got := wrapForTelemetry(nil); got != nil {
		t.Fatalf("wrapForTelemetry(nil) = %#v; want nil", got)
	}
}

func TestWrapForTelemetryDoesNotDoubleWrap(t *testing.T) {
	obs := obstest.NewTestObserver(t)
	_ = obs

	inner := &legacyAdapter{provider: "legacy-db"}
	first := wrapForTelemetry(inner)
	second := wrapForTelemetry(first)

	if first != second {
		t.Fatalf("double wrap produced a new wrapper; got %p != first %p", second, first)
	}
}

func TestLegacyAdapterGetsMetricsOnlyCoverage(t *testing.T) {
	obs := obstest.NewTestObserver(t)

	legacy := &legacyAdapter{provider: "legacy-db"}
	wrapped := wrapForTelemetry(legacy)

	// A legacy adapter must still satisfy StorageAdapter after
	// wrapping. It does not satisfy ContextualStorageAdapter's
	// ctx methods of the underlying type, but the wrapper does
	// (the ctx methods delegate to the non-ctx path).
	if _, ok := wrapped.(ContextualStorageAdapter); !ok {
		t.Fatalf("wrapper should always implement ContextualStorageAdapter")
	}

	ctx := context.Background()
	if err := wrapped.(ContextualStorageAdapter).CreateContext(ctx, &struct{ ID string }{ID: "x"}); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}
	if err := wrapped.Create(&struct{ ID string }{ID: "y"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if legacy.calls != 2 {
		t.Fatalf("legacy inner calls = %d; want 2", legacy.calls)
	}

	// Metrics still recorded for both the ctx and non-ctx paths.
	c := obs.Metrics.CounterValue(
		"magic_storage_operations_total",
		telemetry.Label{Key: "provider", Value: "legacy-db"},
		telemetry.Label{Key: "operation", Value: "create"},
		telemetry.Label{Key: "status", Value: "ok"},
	)
	if c != 2 {
		t.Fatalf("create counter = %v; want 2", c)
	}

	// Spans are skipped for non-contextual adapters even when the
	// caller provided a context, because we cannot link to the
	// underlying operation in a meaningful way.
	if got := len(obs.Spans.Ended()); got != 0 {
		t.Fatalf("expected 0 spans for legacy adapter; got %d", got)
	}
}

func TestLegacyAdapterProviderLabelFallsBackToType(t *testing.T) {
	obs := obstest.NewTestObserver(t)

	// Empty provider should cause the wrapper to fall back to
	// GetType so the "provider" label is never empty.
	legacy := &legacyAdapter{provider: ""}
	wrapped := wrapForTelemetry(legacy)

	if err := wrapped.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	if c := obs.Metrics.CounterValue(
		"magic_storage_operations_total",
		telemetry.Label{Key: "provider", Value: "legacy"},
		telemetry.Label{Key: "operation", Value: "ping"},
		telemetry.Label{Key: "status", Value: "ok"},
	); c != 1 {
		t.Fatalf("ping counter with fallback provider label = %v; want 1", c)
	}
}

func TestModelNameHandlesNilAndPointerAndSlice(t *testing.T) {
	type M struct{}

	if got := modelName(nil); got != "" {
		t.Fatalf("modelName(nil) = %q; want empty", got)
	}
	if got := modelName(&M{}); got != "M" {
		t.Fatalf("modelName(&M{}) = %q; want M", got)
	}
	if got := modelName(&[]M{}); got != "M" {
		t.Fatalf("modelName(&[]M{}) = %q; want M", got)
	}
}
