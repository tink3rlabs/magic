package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/codes"

	"github.com/tink3rlabs/magic/observability/obstest"
	"github.com/tink3rlabs/magic/storage"
	"github.com/tink3rlabs/magic/telemetry"
)

// phaseTwoItem is a dedicated model used by the instrumentation
// tests so they do not collide with any other package test that
// shares the underlying sqlite singleton.
type phaseTwoItem struct {
	Id   string `json:"id" gorm:"primaryKey;column:id"`
	Name string `json:"name" gorm:"column:name"`
}

func (phaseTwoItem) TableName() string { return "phase_two_items" }

// newInstrumentedMemory installs a TestObserver and returns a
// freshly wrapped memory adapter. The schema is created lazily
// because tests run against the shared in-memory sqlite singleton.
func newInstrumentedMemory(t *testing.T) (*obstest.TestObserver, storage.ContextualStorageAdapter) {
	t.Helper()
	obs := obstest.NewTestObserver(t)

	adapter, err := storage.StorageAdapterFactory{}.GetInstance(storage.MEMORY, nil)
	if err != nil {
		t.Fatalf("GetInstance(MEMORY): %v", err)
	}
	ctx, ok := adapter.(storage.ContextualStorageAdapter)
	if !ok {
		t.Fatalf("wrapped memory adapter does not satisfy ContextualStorageAdapter")
	}

	if err := adapter.Execute(`CREATE TABLE IF NOT EXISTS phase_two_items (id TEXT PRIMARY KEY, name TEXT)`); err != nil {
		t.Fatalf("create table: %v", err)
	}
	// Start each test with an empty table.
	if err := adapter.Execute(`DELETE FROM phase_two_items`); err != nil {
		t.Fatalf("truncate: %v", err)
	}
	// The Execute+DELETE above already emitted metrics; clear
	// them so each test starts from a clean counter state.
	obs.Metrics.Reset()
	obs.Spans.Reset()

	return obs, ctx
}

func TestWrappedAdapterExposesContextualInterface(t *testing.T) {
	obs := obstest.NewTestObserver(t)
	defer obs.Close()

	adapter, err := storage.StorageAdapterFactory{}.GetInstance(storage.MEMORY, nil)
	if err != nil {
		t.Fatalf("GetInstance: %v", err)
	}
	if _, ok := adapter.(storage.ContextualStorageAdapter); !ok {
		t.Fatalf("expected wrapper to implement ContextualStorageAdapter")
	}
	if got := adapter.GetType(); got != storage.MEMORY {
		t.Fatalf("GetType = %q; want %q", got, storage.MEMORY)
	}
	if got := adapter.GetProvider(); got != storage.SQLITE {
		t.Fatalf("GetProvider = %q; want %q", got, storage.SQLITE)
	}
	// Memory adapter reports an empty schema name; the wrapper
	// must still delegate that cleanly.
	if got := adapter.GetSchemaName(); got != "" {
		t.Fatalf("GetSchemaName = %q; want empty", got)
	}
}

func TestContextualOperationsEmitSpansAndMetrics(t *testing.T) {
	obs, adapter := newInstrumentedMemory(t)

	ctx := context.Background()
	if err := adapter.CreateContext(ctx, &phaseTwoItem{Id: "1", Name: "alpha"}); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}

	got := phaseTwoItem{}
	if err := adapter.GetContext(ctx, &got, map[string]any{"id": "1"}); err != nil {
		t.Fatalf("GetContext: %v", err)
	}
	if got.Name != "alpha" {
		t.Fatalf("Name = %q; want %q", got.Name, "alpha")
	}

	var list []phaseTwoItem
	if _, err := adapter.ListContext(ctx, &list, "id", nil, 10, ""); err != nil {
		t.Fatalf("ListContext: %v", err)
	}

	counter := obs.Metrics.CounterValue(
		"magic_storage_operations_total",
		telemetry.Label{Key: "provider", Value: "sqlite"},
		telemetry.Label{Key: "operation", Value: "create"},
		telemetry.Label{Key: "status", Value: "ok"},
	)
	if counter != 1 {
		t.Fatalf("create counter = %v; want 1", counter)
	}

	if n := obs.Metrics.HistogramCount(
		"magic_storage_operation_duration_seconds",
		telemetry.Label{Key: "provider", Value: "sqlite"},
		telemetry.Label{Key: "operation", Value: "get"},
	); n != 1 {
		t.Fatalf("get duration observations = %d; want 1", n)
	}

	// Errors counter must stay at zero for the happy path.
	if got := obs.Metrics.CounterValue(
		"magic_storage_operation_errors_total",
		telemetry.Label{Key: "provider", Value: "sqlite"},
		telemetry.Label{Key: "operation", Value: "create"},
	); got != 0 {
		t.Fatalf("create error counter = %v; want 0", got)
	}

	spans := obs.Spans.Ended()
	names := make(map[string]int, len(spans))
	for _, s := range spans {
		names[s.Name()]++
	}
	for _, want := range []string{"storage.create", "storage.get", "storage.list"} {
		if names[want] < 1 {
			t.Fatalf("span %q was not recorded; names: %v", want, names)
		}
	}

	// Each span should carry provider + operation attributes.
	for _, s := range spans {
		if !strings.HasPrefix(s.Name(), "storage.") {
			continue
		}
		attrs := map[string]string{}
		for _, kv := range s.Attributes() {
			attrs[string(kv.Key)] = kv.Value.Emit()
		}
		if attrs["magic.storage.provider"] != "sqlite" {
			t.Fatalf("span %q missing provider attribute: %v", s.Name(), attrs)
		}
		if attrs["magic.storage.operation"] == "" {
			t.Fatalf("span %q missing operation attribute: %v", s.Name(), attrs)
		}
	}
}

func TestNonContextMethodsStillRecordMetrics(t *testing.T) {
	obs, adapter := newInstrumentedMemory(t)

	// Use the legacy (non-ctx) method path. This exercises the
	// one-line delegate and makes sure the wrapper still records
	// metrics for callers that have not migrated to *Context
	// methods.
	if err := adapter.Create(&phaseTwoItem{Id: "1", Name: "alpha"}); err != nil {
		t.Fatalf("Create: %v", err)
	}

	if got := obs.Metrics.CounterValue(
		"magic_storage_operations_total",
		telemetry.Label{Key: "provider", Value: "sqlite"},
		telemetry.Label{Key: "operation", Value: "create"},
		telemetry.Label{Key: "status", Value: "ok"},
	); got != 1 {
		t.Fatalf("create counter = %v; want 1", got)
	}
	// Span is emitted because the underlying adapter is still
	// contextual; the non-ctx wrapper method delegates to the
	// *Context method which opens a span on context.Background.
	if len(obs.Spans.Ended()) == 0 {
		t.Fatalf("expected at least one span for Create call")
	}
}

func TestGetMissingIsRecordedAsOkButSpanIsNotError(t *testing.T) {
	obs, adapter := newInstrumentedMemory(t)

	got := phaseTwoItem{}
	err := adapter.GetContext(context.Background(), &got, map[string]any{"id": "missing"})
	if !errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("GetContext err = %v; want ErrNotFound", err)
	}

	// ErrNotFound is expected; status label is "ok" and the
	// error counter is not incremented.
	if c := obs.Metrics.CounterValue(
		"magic_storage_operations_total",
		telemetry.Label{Key: "provider", Value: "sqlite"},
		telemetry.Label{Key: "operation", Value: "get"},
		telemetry.Label{Key: "status", Value: "ok"},
	); c != 1 {
		t.Fatalf("get ok counter = %v; want 1", c)
	}
	if c := obs.Metrics.CounterValue(
		"magic_storage_operation_errors_total",
		telemetry.Label{Key: "provider", Value: "sqlite"},
		telemetry.Label{Key: "operation", Value: "get"},
	); c != 0 {
		t.Fatalf("get error counter = %v; want 0", c)
	}

	spans := obs.Spans.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d; want 1", len(spans))
	}
	if status := spans[0].Status(); status.Code == codes.Error {
		t.Fatalf("span should not be marked error for ErrNotFound; got %v", status)
	}
}

func TestRealErrorsAreRecordedOnMetricsAndSpan(t *testing.T) {
	obs, adapter := newInstrumentedMemory(t)

	// Get with an empty filter returns a real error (not ErrNotFound).
	got := phaseTwoItem{}
	err := adapter.GetContext(context.Background(), &got, map[string]any{})
	if err == nil {
		t.Fatalf("GetContext with empty filter should have returned an error")
	}
	if errors.Is(err, storage.ErrNotFound) {
		t.Fatalf("unexpected ErrNotFound; want a validation error")
	}

	if c := obs.Metrics.CounterValue(
		"magic_storage_operations_total",
		telemetry.Label{Key: "provider", Value: "sqlite"},
		telemetry.Label{Key: "operation", Value: "get"},
		telemetry.Label{Key: "status", Value: "error"},
	); c != 1 {
		t.Fatalf("get error-status counter = %v; want 1", c)
	}
	if c := obs.Metrics.CounterValue(
		"magic_storage_operation_errors_total",
		telemetry.Label{Key: "provider", Value: "sqlite"},
		telemetry.Label{Key: "operation", Value: "get"},
	); c != 1 {
		t.Fatalf("get error counter = %v; want 1", c)
	}

	spans := obs.Spans.Ended()
	if len(spans) != 1 {
		t.Fatalf("spans = %d; want 1", len(spans))
	}
	if spans[0].Status().Code != codes.Error {
		t.Fatalf("span status = %v; want Error", spans[0].Status())
	}
	if len(spans[0].Events()) == 0 {
		t.Fatalf("expected RecordError to have added a span event")
	}
}

// TestMigrationMethodsAreNotInstrumented verifies the design
// decision to pass migration operations straight through the
// wrapper: they are one-shot startup operations with no
// request-scoped context and no long-term observability value, so
// they emit no spans and no metrics.
func TestMigrationMethodsAreNotInstrumented(t *testing.T) {
	obs, adapter := newInstrumentedMemory(t)

	if err := adapter.CreateSchema(); err != nil {
		t.Fatalf("CreateSchema: %v", err)
	}
	if err := adapter.CreateMigrationTable(); err != nil {
		t.Fatalf("CreateMigrationTable: %v", err)
	}
	if err := adapter.UpdateMigrationTable(1, "init", "bootstrap"); err != nil {
		t.Fatalf("UpdateMigrationTable: %v", err)
	}
	if _, err := adapter.GetLatestMigration(); err != nil {
		t.Fatalf("GetLatestMigration: %v", err)
	}

	if spans := obs.Spans.Ended(); len(spans) != 0 {
		names := make([]string, 0, len(spans))
		for _, s := range spans {
			names = append(names, s.Name())
		}
		t.Fatalf("migration operations must not emit spans; got %v", names)
	}

	// Every storage counter series must remain at zero.
	for _, op := range []string{"create", "get", "update", "delete", "list", "search", "count", "query", "execute", "ping"} {
		if c := obs.Metrics.CounterValue(
			"magic_storage_operations_total",
			telemetry.Label{Key: "provider", Value: "sqlite"},
			telemetry.Label{Key: "operation", Value: op},
			telemetry.Label{Key: "status", Value: "ok"},
		); c != 0 {
			t.Fatalf("migration path incremented %s counter = %v; want 0", op, c)
		}
	}
}

func TestAllCRUDContextMethodsAreInstrumented(t *testing.T) {
	obs, adapter := newInstrumentedMemory(t)
	ctx := context.Background()

	if err := adapter.CreateContext(ctx, &phaseTwoItem{Id: "a", Name: "one"}); err != nil {
		t.Fatalf("CreateContext: %v", err)
	}
	if err := adapter.UpdateContext(ctx, &phaseTwoItem{Id: "a", Name: "two"}, map[string]any{"id": "a"}); err != nil {
		t.Fatalf("UpdateContext: %v", err)
	}

	var list []phaseTwoItem
	if _, err := adapter.SearchContext(ctx, &list, "id", "", 5, ""); err != nil {
		t.Fatalf("SearchContext: %v", err)
	}

	if _, err := adapter.CountContext(ctx, &[]phaseTwoItem{}, map[string]any{"id": "a"}); err != nil {
		t.Fatalf("CountContext: %v", err)
	}

	// QueryContext is not implemented on the sql adapter; we
	// still expect a span + error status to be recorded.
	if _, err := adapter.QueryContext(ctx, &list, "SELECT 1", 5, ""); err == nil {
		t.Fatalf("QueryContext expected error (not implemented)")
	}

	if err := adapter.DeleteContext(ctx, &phaseTwoItem{}, map[string]any{"id": "a"}); err != nil {
		t.Fatalf("DeleteContext: %v", err)
	}

	if err := adapter.ExecuteContext(ctx, "SELECT 1"); err != nil {
		t.Fatalf("ExecuteContext: %v", err)
	}
	if err := adapter.PingContext(ctx); err != nil {
		t.Fatalf("PingContext: %v", err)
	}

	wantOps := []string{"create", "update", "search", "count", "query", "delete", "execute", "ping"}
	for _, op := range wantOps {
		if c := obs.Metrics.CounterValue(
			"magic_storage_operations_total",
			telemetry.Label{Key: "provider", Value: "sqlite"},
			telemetry.Label{Key: "operation", Value: op},
			telemetry.Label{Key: "status", Value: "ok"},
		); c == 0 {
			// Query returns an error; its OK-status counter
			// should stay at 0 and its error counter should be 1.
			if op == "query" {
				if e := obs.Metrics.CounterValue(
					"magic_storage_operation_errors_total",
					telemetry.Label{Key: "provider", Value: "sqlite"},
					telemetry.Label{Key: "operation", Value: "query"},
				); e != 1 {
					t.Fatalf("query error counter = %v; want 1", e)
				}
				continue
			}
			t.Fatalf("%s counter was not incremented", op)
		}
	}
}

func TestAllCRUDNonContextMethodsDelegate(t *testing.T) {
	obs, adapter := newInstrumentedMemory(t)

	if err := adapter.Create(&phaseTwoItem{Id: "nc", Name: "x"}); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got := phaseTwoItem{}
	if err := adapter.Get(&got, map[string]any{"id": "nc"}); err != nil {
		t.Fatalf("Get: %v", err)
	}
	if err := adapter.Update(&phaseTwoItem{Id: "nc", Name: "y"}, map[string]any{"id": "nc"}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	var list []phaseTwoItem
	if _, err := adapter.List(&list, "id", nil, 5, ""); err != nil {
		t.Fatalf("List: %v", err)
	}
	if _, err := adapter.Search(&list, "id", "", 5, ""); err != nil {
		t.Fatalf("Search: %v", err)
	}
	if _, err := adapter.Count(&[]phaseTwoItem{}, nil); err != nil {
		t.Fatalf("Count: %v", err)
	}
	if _, err := adapter.Query(&list, "SELECT 1", 5, ""); err == nil {
		t.Fatalf("Query expected error")
	}
	if err := adapter.Delete(&phaseTwoItem{}, map[string]any{"id": "nc"}); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if err := adapter.Execute("SELECT 1"); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if err := adapter.Ping(); err != nil {
		t.Fatalf("Ping: %v", err)
	}

	// Each non-ctx method should have funneled through a ctx
	// variant and produced a span; exact span counts vary with
	// sqlite behaviour, but at minimum every successful op above
	// should appear in the list.
	names := map[string]bool{}
	for _, s := range obs.Spans.Ended() {
		names[s.Name()] = true
	}
	for _, want := range []string{
		"storage.create", "storage.get", "storage.update",
		"storage.list", "storage.search", "storage.count",
		"storage.query", "storage.delete", "storage.execute",
		"storage.ping",
	} {
		if !names[want] {
			t.Fatalf("missing span %q; got: %v", want, names)
		}
	}
}

