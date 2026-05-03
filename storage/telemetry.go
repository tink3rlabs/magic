package storage

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.27.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/tink3rlabs/magic/telemetry"
)

// ContextualStorageAdapter is an extension interface that adds a
// context-aware sibling to every request-path I/O operation on
// StorageAdapter. Adapters shipped in the magic repository
// implement both interfaces; the non-Context methods delegate to
// the Context variants with context.Background(). Third-party
// adapters are free to implement only StorageAdapter, in which
// case the instrumented wrapper falls back to metrics-only
// coverage (see wrapForTelemetry).
//
// Note: schema/migration methods (CreateSchema,
// CreateMigrationTable, UpdateMigrationTable, GetLatestMigration)
// intentionally do NOT have Context variants. They are one-shot
// startup operations that run before any request-scoped context
// exists, they are not part of any distributed trace, and their
// failures already surface as fatal startup errors. Adding
// Context variants for them would be pure surface-area churn.
type ContextualStorageAdapter interface {
	StorageAdapter

	ExecuteContext(ctx context.Context, statement string) error
	PingContext(ctx context.Context) error

	CreateContext(ctx context.Context, item any, params ...map[string]any) error
	GetContext(ctx context.Context, dest any, filter map[string]any, params ...map[string]any) error
	UpdateContext(ctx context.Context, item any, filter map[string]any, params ...map[string]any) error
	DeleteContext(ctx context.Context, item any, filter map[string]any, params ...map[string]any) error
	ListContext(ctx context.Context, dest any, sortKey string, filter map[string]any, limit int, cursor string, params ...map[string]any) (string, error)
	SearchContext(ctx context.Context, dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (string, error)
	CountContext(ctx context.Context, dest any, filter map[string]any, params ...map[string]any) (int64, error)
	QueryContext(ctx context.Context, dest any, statement string, limit int, cursor string, params ...map[string]any) (string, error)
}

// Metric + label names and operation labels, kept in sync with
// observability/builtins.go. They are redeclared here to avoid a
// package-level import cycle (observability depends on storage).
const (
	metricStorageOperationsTotal          = "magic_storage_operations_total"
	metricStorageOperationDurationSeconds = "magic_storage_operation_duration_seconds"
	metricStorageOperationErrorsTotal     = "magic_storage_operation_errors_total"

	labelProvider  = "provider"
	labelOperation = "operation"
	labelStatus    = "status"

	statusOK    = "ok"
	statusError = "error"

	opCreate  = "create"
	opGet     = "get"
	opUpdate  = "update"
	opDelete  = "delete"
	opList    = "list"
	opSearch  = "search"
	opCount   = "count"
	opQuery   = "query"
	opExecute = "execute"
	opPing    = "ping"
)

// storageDurationBuckets mirrors observability.storageDurationBuckets.
// The instrumented wrapper must register the same shape so Prometheus
// does not reject a re-registration with a different bucket set.
var storageDurationBuckets = []float64{
	0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5,
}

// instrumentedAdapter wraps a storage adapter with tracing and
// metrics. It implements ContextualStorageAdapter so callers
// always have access to context-aware methods; when the underlying
// adapter does not implement ContextualStorageAdapter, spans are
// skipped (to avoid orphan root spans) but metrics are still
// recorded from wall-clock timing.
type instrumentedAdapter struct {
	inner    StorageAdapter
	ctxInner ContextualStorageAdapter
	provider string
	telem    *telemetry.Telemetry

	opsTotal  telemetry.Counter
	opErrors  telemetry.Counter
	opLatency telemetry.Histogram
}

// wrapForTelemetry returns an instrumented StorageAdapter that
// records built-in storage metrics and, when the underlying
// adapter is contextual, emits distributed tracing spans.
//
// It is always safe to call: if telemetry has not been initialized
// (the global is the no-op backend), the returned wrapper uses
// no-op instruments and adds negligible overhead.
func wrapForTelemetry(inner StorageAdapter) StorageAdapter {
	if inner == nil {
		return nil
	}
	// Avoid double-wrapping so callers that receive an already
	// wrapped adapter can safely re-wrap without stacking spans.
	if _, ok := inner.(*instrumentedAdapter); ok {
		return inner
	}

	t := telemetry.Global()
	provider := string(inner.GetProvider())
	if provider == "" {
		provider = string(inner.GetType())
	}

	w := &instrumentedAdapter{
		inner:    inner,
		provider: provider,
		telem:    t,
	}
	if c, ok := inner.(ContextualStorageAdapter); ok {
		w.ctxInner = c
	} else {
		telemetry.WarnOnce(
			fmt.Sprintf("storage.legacy-adapter:%T", inner),
			"storage adapter does not implement ContextualStorageAdapter; traces will not be linked (metrics-only coverage)",
			slog.String("adapter_type", fmt.Sprintf("%T", inner)),
		)
	}

	w.registerInstruments()
	return w
}

// UnwrapStorageAdapter implements TelemetryUnwrapper by returning the delegate
// adapter without telemetry wrapping.
func (w *instrumentedAdapter) UnwrapStorageAdapter() StorageAdapter {
	return w.inner
}

// registerInstruments looks up (or creates, idempotently) the
// built-in storage instruments. Registration failures are logged
// and the instruments fall back to no-ops so instrumentation never
// breaks real storage calls.
func (w *instrumentedAdapter) registerInstruments() {
	labels := []string{labelProvider, labelOperation, labelStatus}
	providerOp := []string{labelProvider, labelOperation}

	if c, err := w.telem.Metrics.Counter(telemetry.MetricDefinition{
		Name:   metricStorageOperationsTotal,
		Help:   "Total storage operations executed, labeled by provider, operation, and final status.",
		Kind:   telemetry.KindCounter,
		Labels: labels,
	}); err == nil {
		w.opsTotal = c
	} else {
		slog.Warn("storage: failed to register operations counter", "error", err)
	}

	if h, err := w.telem.Metrics.Histogram(telemetry.MetricDefinition{
		Name:    metricStorageOperationDurationSeconds,
		Help:    "Storage operation wall-clock duration in seconds, from instrumentation entry to adapter return.",
		Unit:    telemetry.UnitSeconds,
		Kind:    telemetry.KindHistogram,
		Labels:  providerOp,
		Buckets: storageDurationBuckets,
	}); err == nil {
		w.opLatency = h
	} else {
		slog.Warn("storage: failed to register duration histogram", "error", err)
	}

	if c, err := w.telem.Metrics.Counter(telemetry.MetricDefinition{
		Name:   metricStorageOperationErrorsTotal,
		Help:   "Total storage operations that returned an error, labeled by provider and operation.",
		Kind:   telemetry.KindCounter,
		Labels: providerOp,
	}); err == nil {
		w.opErrors = c
	} else {
		slog.Warn("storage: failed to register error counter", "error", err)
	}
}

// tracer returns the tracer to use for this wrapper, or nil when
// spans should be skipped. Legacy (non-contextual) adapters have
// no way to receive a parent span context, so emitting spans
// against them would produce orphan roots that pollute trace UIs.
func (w *instrumentedAdapter) tracer() trace.Tracer {
	if w.ctxInner == nil {
		return nil
	}
	if w.telem == nil || w.telem.Tracer == nil {
		return nil
	}
	return w.telem.Tracer
}

// observation captures per-call state so metric-only recording
// works the same for contextual and legacy paths.
type observation struct {
	op    string
	start time.Time
	span  trace.Span
}

func (w *instrumentedAdapter) begin(ctx context.Context, op string, extra ...attribute.KeyValue) (context.Context, *observation) {
	obs := &observation{
		op:    op,
		start: time.Now(),
	}
	if t := w.tracer(); t != nil {
		attrs := make([]attribute.KeyValue, 0, 3+len(extra))
		attrs = append(attrs,
			semconv.DBSystemKey.String(w.provider),
			attribute.String("magic.storage.provider", w.provider),
			attribute.String("magic.storage.operation", op),
		)
		attrs = append(attrs, extra...)
		ctx, obs.span = t.Start(ctx, "storage."+op, trace.WithAttributes(attrs...))
	}
	return ctx, obs
}

func (w *instrumentedAdapter) end(obs *observation, err error) {
	status := statusOK
	// ErrNotFound is a normal, expected result for Get; treat it
	// as success for status labeling to avoid alert noise. The
	// underlying error is still recorded on the span if present.
	if err != nil && !errors.Is(err, ErrNotFound) {
		status = statusError
	}

	if obs.span != nil {
		if status == statusError {
			obs.span.RecordError(err)
			obs.span.SetStatus(codes.Error, err.Error())
		}
		obs.span.End()
	}

	providerOp := []telemetry.Label{
		{Key: labelProvider, Value: w.provider},
		{Key: labelOperation, Value: obs.op},
	}
	if w.opsTotal != nil {
		w.opsTotal.Add(1,
			telemetry.Label{Key: labelProvider, Value: w.provider},
			telemetry.Label{Key: labelOperation, Value: obs.op},
			telemetry.Label{Key: labelStatus, Value: status},
		)
	}
	if w.opLatency != nil {
		w.opLatency.Observe(time.Since(obs.start).Seconds(), providerOp...)
	}
	if status == statusError && w.opErrors != nil {
		w.opErrors.Add(1, providerOp...)
	}
}

// modelName reflects the element type of a slice or single value
// pointer. Emits the empty string when the type cannot be
// determined so that span attributes remain low cardinality.
func modelName(v any) string {
	if v == nil {
		return ""
	}
	t := reflect.TypeOf(v)
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	return t.Name()
}

// Pass-through identity methods. These do no I/O and so do not
// warrant instrumentation.

func (w *instrumentedAdapter) GetType() StorageAdapterType   { return w.inner.GetType() }
func (w *instrumentedAdapter) GetProvider() StorageProviders { return w.inner.GetProvider() }
func (w *instrumentedAdapter) GetSchemaName() string         { return w.inner.GetSchemaName() }

// -- Instrumented I/O: non-ctx variants delegate to ctx variants.

func (w *instrumentedAdapter) Execute(statement string) error {
	return w.ExecuteContext(context.Background(), statement)
}

func (w *instrumentedAdapter) ExecuteContext(ctx context.Context, statement string) (err error) {
	ctx, obs := w.begin(ctx, opExecute)
	defer func() { w.end(obs, err) }()
	if w.ctxInner != nil {
		return w.ctxInner.ExecuteContext(ctx, statement)
	}
	return w.inner.Execute(statement)
}

func (w *instrumentedAdapter) Ping() error {
	return w.PingContext(context.Background())
}

func (w *instrumentedAdapter) PingContext(ctx context.Context) (err error) {
	ctx, obs := w.begin(ctx, opPing)
	defer func() { w.end(obs, err) }()
	if w.ctxInner != nil {
		return w.ctxInner.PingContext(ctx)
	}
	return w.inner.Ping()
}

// Schema/migration methods pass straight through with no
// instrumentation. They run once at startup, outside any request
// context; a span would always be an orphan root and steady-state
// metrics against one-shot calls are noise. Failures surface as
// fatal startup errors on their own.

func (w *instrumentedAdapter) CreateSchema() error {
	return w.inner.CreateSchema()
}

func (w *instrumentedAdapter) CreateMigrationTable() error {
	return w.inner.CreateMigrationTable()
}

func (w *instrumentedAdapter) UpdateMigrationTable(id int, name string, desc string) error {
	return w.inner.UpdateMigrationTable(id, name, desc)
}

func (w *instrumentedAdapter) GetLatestMigration() (int, error) {
	return w.inner.GetLatestMigration()
}

// CRUD operations emit spans + metrics.

func (w *instrumentedAdapter) Create(item any, params ...map[string]any) error {
	return w.CreateContext(context.Background(), item, params...)
}

func (w *instrumentedAdapter) CreateContext(ctx context.Context, item any, params ...map[string]any) (err error) {
	ctx, obs := w.begin(ctx, opCreate, attribute.String("magic.storage.model", modelName(item)))
	defer func() { w.end(obs, err) }()
	if w.ctxInner != nil {
		return w.ctxInner.CreateContext(ctx, item, params...)
	}
	return w.inner.Create(item, params...)
}

func (w *instrumentedAdapter) Get(dest any, filter map[string]any, params ...map[string]any) error {
	return w.GetContext(context.Background(), dest, filter, params...)
}

func (w *instrumentedAdapter) GetContext(ctx context.Context, dest any, filter map[string]any, params ...map[string]any) (err error) {
	ctx, obs := w.begin(ctx, opGet, attribute.String("magic.storage.model", modelName(dest)))
	defer func() { w.end(obs, err) }()
	if w.ctxInner != nil {
		return w.ctxInner.GetContext(ctx, dest, filter, params...)
	}
	return w.inner.Get(dest, filter, params...)
}

func (w *instrumentedAdapter) Update(item any, filter map[string]any, params ...map[string]any) error {
	return w.UpdateContext(context.Background(), item, filter, params...)
}

func (w *instrumentedAdapter) UpdateContext(ctx context.Context, item any, filter map[string]any, params ...map[string]any) (err error) {
	ctx, obs := w.begin(ctx, opUpdate, attribute.String("magic.storage.model", modelName(item)))
	defer func() { w.end(obs, err) }()
	if w.ctxInner != nil {
		return w.ctxInner.UpdateContext(ctx, item, filter, params...)
	}
	return w.inner.Update(item, filter, params...)
}

func (w *instrumentedAdapter) Delete(item any, filter map[string]any, params ...map[string]any) error {
	return w.DeleteContext(context.Background(), item, filter, params...)
}

func (w *instrumentedAdapter) DeleteContext(ctx context.Context, item any, filter map[string]any, params ...map[string]any) (err error) {
	ctx, obs := w.begin(ctx, opDelete, attribute.String("magic.storage.model", modelName(item)))
	defer func() { w.end(obs, err) }()
	if w.ctxInner != nil {
		return w.ctxInner.DeleteContext(ctx, item, filter, params...)
	}
	return w.inner.Delete(item, filter, params...)
}

func (w *instrumentedAdapter) List(dest any, sortKey string, filter map[string]any, limit int, cursor string, params ...map[string]any) (string, error) {
	return w.ListContext(context.Background(), dest, sortKey, filter, limit, cursor, params...)
}

func (w *instrumentedAdapter) ListContext(ctx context.Context, dest any, sortKey string, filter map[string]any, limit int, cursor string, params ...map[string]any) (nextCursor string, err error) {
	ctx, obs := w.begin(ctx, opList,
		attribute.String("magic.storage.model", modelName(dest)),
		attribute.String("magic.storage.sort_field", sortKey),
		attribute.Int("magic.storage.limit", limit),
	)
	defer func() { w.end(obs, err) }()
	if w.ctxInner != nil {
		return w.ctxInner.ListContext(ctx, dest, sortKey, filter, limit, cursor, params...)
	}
	return w.inner.List(dest, sortKey, filter, limit, cursor, params...)
}

func (w *instrumentedAdapter) Search(dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (string, error) {
	return w.SearchContext(context.Background(), dest, sortKey, query, limit, cursor, params...)
}

func (w *instrumentedAdapter) SearchContext(ctx context.Context, dest any, sortKey string, query string, limit int, cursor string, params ...map[string]any) (nextCursor string, err error) {
	ctx, obs := w.begin(ctx, opSearch,
		attribute.String("magic.storage.model", modelName(dest)),
		attribute.String("magic.storage.sort_field", sortKey),
		attribute.Int("magic.storage.limit", limit),
	)
	defer func() { w.end(obs, err) }()
	if w.ctxInner != nil {
		return w.ctxInner.SearchContext(ctx, dest, sortKey, query, limit, cursor, params...)
	}
	return w.inner.Search(dest, sortKey, query, limit, cursor, params...)
}

func (w *instrumentedAdapter) Count(dest any, filter map[string]any, params ...map[string]any) (int64, error) {
	return w.CountContext(context.Background(), dest, filter, params...)
}

func (w *instrumentedAdapter) CountContext(ctx context.Context, dest any, filter map[string]any, params ...map[string]any) (total int64, err error) {
	ctx, obs := w.begin(ctx, opCount, attribute.String("magic.storage.model", modelName(dest)))
	defer func() { w.end(obs, err) }()
	if w.ctxInner != nil {
		return w.ctxInner.CountContext(ctx, dest, filter, params...)
	}
	return w.inner.Count(dest, filter, params...)
}

func (w *instrumentedAdapter) Query(dest any, statement string, limit int, cursor string, params ...map[string]any) (string, error) {
	return w.QueryContext(context.Background(), dest, statement, limit, cursor, params...)
}

func (w *instrumentedAdapter) QueryContext(ctx context.Context, dest any, statement string, limit int, cursor string, params ...map[string]any) (nextCursor string, err error) {
	ctx, obs := w.begin(ctx, opQuery,
		attribute.String("magic.storage.model", modelName(dest)),
		attribute.Int("magic.storage.limit", limit),
	)
	defer func() { w.end(obs, err) }()
	if w.ctxInner != nil {
		return w.ctxInner.QueryContext(ctx, dest, statement, limit, cursor, params...)
	}
	return w.inner.Query(dest, statement, limit, cursor, params...)
}
