# Observability Design for `magic`

## Overview

This document defines a new observability module for the `magic` library that provides:

* Distributed tracing via OpenTelemetry (OTEL)
* Metrics via one of two modes:

  * Prometheus `/metrics` endpoint (default)
  * OTEL metrics over OTLP
* Automatic instrumentation of core `magic` packages:

  * HTTP / chi router
  * `storage` (operations that pass a `context.Context`)
  * `pubsub` (publish-only in v1)
* Custom metrics support for service authors
* Automatic trace/span correlation on `slog` `*Context` log calls (zero-touch once `logger.Init` runs)
* A backend-neutral metrics abstraction so services and `magic` packages do not need to care whether metrics are exported via Prometheus, OTEL, or both

The goal is to provide a near-zero-touch observability stack for services built on `magic`:

* initialize once
* add one chi middleware
* optionally expose `/metrics`
* automatically get tracing and metrics across HTTP, storage (contextual), and pubsub publish
* easily define additional business metrics

This design assumes most consumers of `magic` use `chi` as their HTTP router and want a simple way to enable tracing and metrics with minimal service-level code changes.

### Compatibility Constraint

Breaking changes to existing `magic` packages are off the table. The design therefore:

* Preserves every existing `StorageAdapter` and `Publisher` method signature.
* Adds new context-aware methods on an extension interface (`ContextualStorageAdapter`) and a new `PublishContext` method on `Publisher`, following the `database/sql` `…Context` naming convention.
* Falls back gracefully when an adapter has not yet been migrated to the contextual interface.

---

## Goals

* Provide easy observability enablement for services built on `magic`
* Support distributed tracing end-to-end across:

  * HTTP requests
  * storage operations (when called through `ContextualStorageAdapter`)
  * pubsub publish operations
* Support metrics for:

  * Go runtime
  * process
  * HTTP requests
  * storage operations
  * pubsub publish operations
  * custom business metrics
* Provide automatic instrumentation inside `magic` packages so consumers get value with minimal code changes
* Support Prometheus scrape and OTLP push as interchangeable metrics export modes without changing instrumentation code
* Automatically correlate application logs with the active trace/span by wrapping the `slog` handler in the `logger` package
* Provide first-class support for unit testing instrumented code

---

## Non-Goals (v1)

* OTEL logs integration
* PubSub consume / process / ack / nack instrumentation (no Consumer interface exists in the repo yet; this is deferred to a follow-up design)
* Auto-instrumentation of arbitrary third-party libraries not wrapped by `magic`
* Built-in dashboards or Grafana assets
* Advanced sampling controls beyond parent-based ratio sampling
* Tenant-level or user-level metric labels by default
* Automatic instrumentation of application business code

---

## Design Principles

### 1. Near-zero-touch for shared infrastructure

If a service uses `magic` packages like `storage` and `pubsub`, those packages should emit traces and metrics automatically once observability is enabled — provided callers use the contextual methods.

### 2. Explicit bootstrap, implicit package instrumentation

A service must explicitly initialize observability. Once initialized, `magic` packages instrument themselves automatically through package-level telemetry hooks.

### 3. Backend-neutral metrics instrumentation

Metrics instrumentation code should not depend on Prometheus or OTEL implementation details. Services and `magic` packages use a stable internal metrics abstraction. Tracing uses the OTEL API directly — there is only one viable tracing backend and wrapping OTEL's tracer adds surface without benefit.

### 4. Safe metric cardinality

Metric labels must be intentionally constrained. Route labels must use chi route patterns, not raw URL paths. Custom metrics require declared label keys, and runtime labels outside the declared set are rejected by default.

### 5. No hidden behavior change for non-migrated code

Services or adapters that have not adopted the contextual APIs must continue to work unchanged. They lose tracing (no parent context) but keep metrics wherever possible, and the system warns once about the missing coverage.

### 6. Keep the service developer experience simple

For most services, the happy path should look like this:

```go
obs, err := observability.Init(ctx, cfg)
if err != nil {
    log.Fatal(err)
}
defer obs.Shutdown(ctx)

r := chi.NewRouter()
r.Use(obs.ChiMiddleware())

r.Handle("/metrics", obs.MetricsHandler())
```

---

## High-Level Design

The observability module consists of:

* A bootstrap package, `observability`, responsible for initializing tracing and metrics backends
* A neutral package, `telemetry`, that hosts the abstractions shared by `magic` packages
* Package-level instrumentation in `magic` core packages
* Configurable export backends for metrics

Instrumentation happens at three levels:

1. **HTTP level**

   * request tracing
   * request metrics
   * propagation of trace context into handlers

2. **Shared infrastructure level**

   * storage tracing (contextual adapters) and metrics (all adapters)
   * pubsub publish tracing and metrics (contextual publishers)

3. **Application business level**

   * custom counters, histograms, gauges, and up-down counters defined by service authors

This gives consumers infrastructure telemetry by default and business telemetry when needed.

---

## Package Layering & Imports

To avoid an import cycle between `observability` and instrumented packages (`storage`, `pubsub`), and to keep `magic` core packages free of Prometheus and OTEL dependencies, the design introduces a neutral `telemetry` package.

### Dependency Direction

```text
observability ──▶ telemetry ◀── storage
                      ▲
                      └────── pubsub
```

* The `telemetry` package is **exported** as `github.com/<org>/magic/telemetry` (not `internal/`), so third-party storage/pubsub adapters built outside this repo can implement `ContextualStorageAdapter` and emit through the same pipeline as in-repo adapters. Its API is part of `magic`'s stability contract; new capabilities are added via extension interfaces rather than breaking changes.
* `telemetry` defines the interfaces (`MetricsBackend`, `Counter`, `Histogram`, `Gauge`, `UpDownCounter`, `MetricDefinition`, etc.) and a package-level `Global()` accessor.
* `storage` and `pubsub` import `telemetry` and use `telemetry.Global()` for their instrumentation.
* `observability` imports `telemetry`, installs concrete implementations via `telemetry.SetGlobal(...)` during `Init`, and owns the Prometheus/OTEL backend code.

### Dependency Discipline

To keep `go.mod` lean for services that use `storage`/`pubsub`/`logger` without enabling observability, the following rules apply to `magic` core packages (`storage`, `pubsub`, `logger`, and any future instrumented package):

* **Metrics must go through `telemetry`.** Core packages may not import `prometheus/client_golang`, `go.opentelemetry.io/otel/sdk/metric`, or any OTEL metric exporter package.
* **Tracing uses the OTEL trace API directly, not the SDK.** Core packages may import only:

  * `go.opentelemetry.io/otel/trace` — the interface-level trace API (used by `storage`, `pubsub`, and `logger` for span-context extraction)
  * `go.opentelemetry.io/otel/semconv/...` — semantic convention constants
  * `go.opentelemetry.io/otel/propagation` — only where a package needs to inject/extract context (currently `pubsub`)

  These packages are small, interface-heavy, and pull in no exporters or SDK.
* **Everything heavy lives in `observability`.** The OTEL SDK (`sdk/trace`, `sdk/metric`), all exporters (OTLP, Prom), `prometheus/client_golang`, and `sdk/resource` are imported only by `observability`. A service that never calls `observability.Init` pays only the cost of the trace API (tens of KB of interface code) when it uses `storage`, `pubsub`, or `logger`.

This is enforced by a CI check (`go list -deps ./storage/... ./pubsub/... ./logger/... ./telemetry/...` must not contain any of the forbidden import paths).

### Why a Neutral Package

* Removes the circular dependency between `observability` and `storage`/`pubsub`.
* Keeps the core packages' dependency graph lean — consumers who never enable observability don't pull in the OTEL SDK or Prometheus.
* Allows the `telemetry` interfaces to evolve independently of the bootstrap package, and lets third-party adapters implement them without importing `observability`.

### Global vs. Scoped Telemetry

`telemetry.Global()` returns a process-wide default that is a no-op until `observability.Init(...)` replaces it. This matches the doc's bootstrap-once-use-everywhere model.

Known limitations, documented for users:

* Tests that need isolated telemetry must use `telemetry.WithContext(ctx, t)` to scope a `Telemetry` to a context, or use the test harness in `observability/obstest` which resets global state between tests.
* Multiple calls to `observability.Init` replace the global. This is intentional but must be done carefully; concurrent `Init` calls are not supported.
* Adapters read the global on each operation, not at construction. Services that construct a storage adapter before calling `Init` still get instrumentation once `Init` runs.

---

## Proposed Package Structure

```text
telemetry/
  telemetry.go       // Telemetry struct + Global/SetGlobal
  tracer.go          // re-exports or aliases OTEL trace types used by magic
  metrics.go         // MetricsBackend, Counter, Histogram, Gauge, UpDownCounter, MetricDefinition
  labels.go          // Label, Labels helper
  noop.go            // no-op implementations (the zero-value default)
  context.go         // WithContext / FromContext for scoped telemetry

observability/
  config.go
  init.go
  shutdown.go
  tracing.go
  metrics_backend_prom.go
  metrics_backend_otel.go
  chi.go
  response_writer.go
  custom_metrics.go
  defaults.go
  logger.go          // LoggerFromContext escape-hatch helper for non-slog loggers
  obstest/
    observer.go      // NewTestObserver + assertions

storage/
  telemetry.go       // ContextualStorageAdapter interface + instrumented wrapper
  instrumented_adapter.go

pubsub/
  telemetry.go       // PublishContext extension + instrumented wrapper
  instrumented_publisher.go

logger/
  trace_handler.go   // slog handler wrap that auto-injects trace_id/span_id
```

Potential future additions (explicitly out of v1 scope):

```text
pubsub/
  consumer.go        // Consumer/Subscriber interface (future design)
  instrumented_consumer.go

health/
  telemetry.go
```

---

## Storage Context Migration

### Problem

The existing `storage.StorageAdapter` interface does not take `context.Context`. Without context, the instrumented wrapper cannot extract the parent span and storage operations would be orphan root spans, defeating end-to-end tracing.

### Solution: `ContextualStorageAdapter` Extension Interface

Add a new extension interface in `storage/telemetry.go`. Every method on `StorageAdapter` that performs I/O gets a `…Context` sibling.

```go
package storage

import "context"

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
```

Schema/migration methods (`CreateSchema`, `CreateMigrationTable`, `UpdateMigrationTable`, `GetLatestMigration`) deliberately do **not** gain `…Context` variants. They are one-shot startup operations that run before any request-scoped context exists, they are not part of any distributed trace, and their failures already surface as fatal startup errors. The instrumented wrapper passes them straight through with no span and no metric — adding context or instrumentation would be pure surface-area churn without operational value.

### Delegation Pattern

Every adapter implements the `Context` variants as the primary methods. The non-`Context` variants delegate with `context.Background()`:

```go
func (a *sqlAdapter) Get(dest any, filter map[string]any, params ...map[string]any) error {
    return a.GetContext(context.Background(), dest, filter, params...)
}

func (a *sqlAdapter) GetContext(ctx context.Context, dest any, filter map[string]any, params ...map[string]any) error {
    // real implementation lives here
}
```

This prevents drift: the non-ctx variants have no logic of their own.

### Instrumented Wrapper Capability Check

`StorageAdapterFactory.GetInstance(...)` returns an instrumented wrapper when observability is active. The wrapper checks whether the underlying adapter implements `ContextualStorageAdapter`:

```go
type instrumentedAdapter struct {
    inner           StorageAdapter
    ctxInner        ContextualStorageAdapter // nil if inner is legacy
    telemetry       telemetry.Telemetry
    providerLabel   string
}

func wrap(inner StorageAdapter, t telemetry.Telemetry) StorageAdapter {
    ctxInner, _ := inner.(ContextualStorageAdapter)
    if ctxInner == nil {
        telemetry.WarnOnce("storage adapter %T does not implement ContextualStorageAdapter; traces will not be linked", inner)
    }
    return &instrumentedAdapter{inner: inner, ctxInner: ctxInner, telemetry: t, ...}
}
```

### Fallback Behavior for Legacy Adapters (metrics-only)

When a caller uses the non-ctx method or the adapter is not contextual:

* **Spans are skipped.** No orphan root spans are created. This avoids polluting trace UIs with unparented storage spans.
* **Metrics are still recorded.** `magic_storage_operations_total`, `magic_storage_operation_duration_seconds`, and `magic_storage_operation_errors_total` are emitted normally.
* **A warn-once log** is issued at the first operation against a non-contextual adapter, naming the adapter type and recommending migration.

This preserves full metric coverage across the legacy path while keeping the trace UI clean.

### Migration Plan

All in-repo adapters must implement `ContextualStorageAdapter` during Phase 2 of the implementation plan:

* `storage/sql.go`
* `storage/dynamodb.go`
* `storage/cosmosdb.go`
* `storage/memory.go`
* Any Cassandra adapter that lands before Phase 2 completes

No adapter in the repository will remain legacy after Phase 2 ships. The legacy path exists for third-party adapters outside the `magic` repo and for binary-compatibility with pre-observability versions of `magic`.

### PubSub: Same Pattern

The `pubsub.Publisher` interface gets a `PublishContext` method via a sibling `ContextualPublisher` interface:

```go
package pubsub

import "context"

type ContextualPublisher interface {
    Publisher
    PublishContext(ctx context.Context, topic, message string, params map[string]any) error
}
```

`Publish` delegates to `PublishContext` with `context.Background()`. The instrumented wrapper applies the same capability check and fallback behavior (metrics only, no span, warn-once).

---

## Public API

### Package

```go
package observability
```

### Config

```go
type Config struct {
    ServiceName    string
    ServiceVersion string
    Environment    string

    TracingEnabled bool
    MetricsEnabled bool

    MetricsPath string // default: "/metrics"

    // Tracing
    OTLPTraceEndpoint string
    OTLPInsecure      bool

    // SamplingRatio is a pointer so that nil means "default" (1.0 when
    // TracingEnabled is true). A zero value (0.0) explicitly disables sampling
    // even when tracing is enabled. Ignored when Sampler is non-nil.
    SamplingRatio *float64

    // Sampler is an escape hatch for advanced sampling strategies. When non-nil
    // it overrides SamplingRatio and is wrapped with a parent-based sampler
    // internally, so downstream sampling decisions are still respected.
    // Tail sampling and per-operation sampling belong in the OTEL Collector,
    // not here.
    Sampler sdktrace.Sampler

    // Propagator defaults to W3C tracecontext + baggage via otel.GetTextMapPropagator().
    // Setting Propagator overrides the global default for magic-initiated work.
    Propagator propagation.TextMapPropagator

    // Metrics
    MetricsMode     MetricsMode
    MetricNamespace string // applied to custom metrics only; built-in metric names are not prefixed

    DisableGoMetrics      bool
    DisableProcessMetrics bool

    IgnoreRoutes []string

    // AllowUndeclaredLabels inverts the sense of the previous StrictMetrics field
    // so the zero value (false) gives strict behavior, which is the safer default.
    AllowUndeclaredLabels bool
}
```

### Metrics Mode

```go
type MetricsMode string

const (
    MetricsModePrometheus MetricsMode = "prometheus"
    MetricsModeOTLP       MetricsMode = "otlp"
)
```

### Observer

```go
type Observer struct {
    telemetry   telemetry.Telemetry
    shutdownFns []func(context.Context) error
}
```

### Initialization

```go
func Init(ctx context.Context, cfg Config) (*Observer, error)
```

Responsibilities:

* validate config
* apply defaults
* initialize tracing provider if enabled
* configure the global propagator
* initialize the metrics backend if enabled
* register default runtime/process metrics if applicable
* register built-in HTTP, storage, and pubsub instruments
* install the telemetry global so `storage` and `pubsub` package hooks become active
* return an `Observer`

### Middleware

```go
func (o *Observer) ChiMiddleware() func(http.Handler) http.Handler
```

Responsibilities:

* extract incoming trace context via the configured propagator
* start a server span when tracing is enabled
* wrap the response writer to capture status code
* **record metrics and finalize the span in a deferred block after `next.ServeHTTP`**, so the chi route pattern is populated
* skip ignored routes when configured
* on panic in the inner handler, record the panic on the span, re-raise, and still record metrics (see panic policy below)

### Metrics Handler

```go
func (o *Observer) MetricsHandler() http.Handler
```

Responsibilities:

* in `prometheus` mode, expose a standard `/metrics` endpoint backed by the Prometheus registry
* **in `otlp` mode, return an `http.Handler` that serves 404 with a short JSON body** (`{"error":"metrics are exported via OTLP"}`); never return `nil`. This makes `r.Handle("/metrics", obs.MetricsHandler())` safe in either mode.

### Shutdown

```go
func (o *Observer) Shutdown(ctx context.Context) error
```

Responsibilities:

* flush exporters
* stop tracer and meter providers cleanly
* execute any registered shutdown callbacks
* reset `telemetry.Global()` to the no-op implementation

---

## Default Behavior

Recommended defaults applied by `applyDefaults(cfg)`:

* `MetricsEnabled = true`
* `TracingEnabled = false`
* `MetricsPath = "/metrics"`
* `SamplingRatio = nil` → treated as `1.0` when `TracingEnabled` is true
* `Sampler = nil` → falls back to parent-based(ratio(SamplingRatio)); when set, wraps the caller's sampler in parent-based and ignores `SamplingRatio`
* `MetricsMode = MetricsModePrometheus`
* `IgnoreRoutes = []string{"/metrics", "/healthz"}`
* `AllowUndeclaredLabels = false` (strict)
* `Propagator = otel.GetTextMapPropagator()` (W3C tracecontext + baggage)

Reasoning:

* metrics are generally low-risk and high-value, so they default to enabled
* tracing usually depends on collector/exporter availability, so it is opt-in unless configured
* `/metrics` is the standard default path
* runtime and process metrics are enabled by default in scrape-based modes
* strict label validation is the safer default and prevents accidental cardinality blow-ups

---

## Internal Telemetry Abstraction

### Telemetry

Defined in the neutral `telemetry` package.

```go
package telemetry

import "go.opentelemetry.io/otel/trace"

type Telemetry struct {
    TracerProvider trace.TracerProvider // use OTEL directly; nil means no-op
    Metrics        MetricsBackend       // nil means no-op
}

func Global() Telemetry
func SetGlobal(t Telemetry)
```

### Tracing

Use OTEL trace APIs directly. Do **not** define custom `Tracer`, `Span`, or `SpanStartOption` interfaces. Callers in `magic` packages obtain a tracer as:

```go
tracer := telemetry.Global().TracerProvider.Tracer("magic/storage")
ctx, span := tracer.Start(ctx, "storage.get", trace.WithAttributes(...))
defer span.End()
```

Rationale: there is only one viable tracing backend (OTEL). Wrapping the OTEL `Tracer` / `Span` types means either mirroring their full surface (events, links, baggage, typed attributes, `RecordError`) or forcing escape hatches. The marginal benefit does not justify the surface area.

### Metrics Backend

```go
type MetricsBackend interface {
    NewCounter(def MetricDefinition) (Counter, error)
    NewHistogram(def MetricDefinition) (Histogram, error)
    NewGauge(def MetricDefinition) (Gauge, error)
    NewUpDownCounter(def MetricDefinition) (UpDownCounter, error)
}

type Counter interface {
    Add(ctx context.Context, value float64, labels ...Label)
}

type Histogram interface {
    Record(ctx context.Context, value float64, labels ...Label)
}

// Gauge represents an instantaneous value that is observed, not accumulated.
// Backed by a Prometheus Gauge or an OTEL async Gauge (Float64ObservableGauge).
type Gauge interface {
    Set(ctx context.Context, value float64, labels ...Label)
}

// UpDownCounter represents an additive value that can go up or down.
// Backed by a Prometheus Gauge or an OTEL Float64UpDownCounter.
type UpDownCounter interface {
    Add(ctx context.Context, value float64, labels ...Label)
}
```

### Why `Gauge` and `UpDownCounter` Are Split

In OTEL, `Set` maps to an async gauge (observed via callback) and `Add` maps to an `UpDownCounter`. Offering both operations on one type silently backs it by two unrelated instruments or makes one of them a no-op. Splitting keeps the mapping one-to-one and matches OTEL semantics, while Prometheus (which has a single `Gauge` supporting both) trivially implements both interfaces on top of one underlying `prometheus.Gauge`.

### Implementations

* `prometheusMetricsBackend` — uses `prometheus/client_golang`, served via the `/metrics` handler
* `otelMetricsBackend` — uses OTEL metrics SDK with an OTLP exporter, pushed to the configured collector

---

## HTTP Instrumentation

### Goal

Provide request tracing and metrics for chi-based services with one middleware.

### Middleware Ordering

The chi route pattern is populated during routing, which happens during `next.ServeHTTP`. A naive middleware that records metrics *before* calling `next` will see an empty route pattern and fall back to `"unknown"` for every request.

The middleware therefore records on the trailing edge:

```go
func (o *Observer) ChiMiddleware() func(http.Handler) http.Handler {
    return func(next http.Handler) http.Handler {
        return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
            start := time.Now()

            ctx := o.propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
            ctx, span := o.tracer.Start(ctx, "http.request", trace.WithSpanKind(trace.SpanKindServer))
            rw := newResponseWriter(w)

            defer func() {
                route := chi.RouteContext(r.Context()).RoutePattern()
                if route == "" {
                    route = "unknown"
                }
                if o.shouldIgnore(route) {
                    span.End()
                    return
                }
                o.recordRequest(ctx, r.Method, route, rw.status, time.Since(start))
                span.SetName(r.Method + " " + route)
                span.SetAttributes(
                    semconv.HTTPRequestMethodKey.String(r.Method),
                    semconv.HTTPRouteKey.String(route),
                    semconv.HTTPResponseStatusCodeKey.Int(rw.status),
                )
                if rw.status >= 500 {
                    span.SetStatus(codes.Error, http.StatusText(rw.status))
                }
                span.End()
            }()

            next.ServeHTTP(rw, r.WithContext(ctx))
        })
    }
}
```

### Panic Policy

The middleware does **not** itself recover panics. If a downstream handler panics and no upstream middleware recovers:

* the deferred block still runs, which records an `error` status on the span, `RecordError` with the recovered panic value (if reachable via `recover()` inside the deferred block, which it is), and emits metrics with status `"500"`
* the panic is then re-raised via `panic(rec)` so upstream middleware or the default `net/http` recovery can handle it

This means `ChiMiddleware()` is safe to place either before or after a user-supplied recover middleware. It never swallows panics.

### HTTP Tracing

Each incoming request creates a server span.

Span name format:

```text
GET /users/{id}
POST /orders
```

Attributes use OTEL semantic conventions (`semconv` package):

* `http.request.method`
* `http.route`
* `http.response.status_code`
* `network.peer.address` (optional)

Behavior:

* extract incoming trace context via the configured propagator (default W3C tracecontext + baggage)
* start a server span
* inject the updated context into the downstream request
* mark span status as error for 5xx responses
* record panics as span errors and re-raise

### HTTP Metrics

Built-in HTTP metrics:

* `http_requests_total` — counter
* `http_request_duration_seconds` — histogram, buckets: `{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}`

Labels:

* `method`
* `route`
* `status`

The HTTP histogram bucket set adds `0.001` and `0.005` below the Prometheus default so p50/p90 for well-tuned services don't pile up at the 5 ms boundary.

**These names are not prefixed by `MetricNamespace`.** Built-in metrics use stable, standard names so that off-the-shelf dashboards and alert rules work without modification.

### Cardinality Rule

Route labels use the chi route pattern. Raw paths are never used.

Correct:

```text
/users/{id}
/accounts/{accountId}/roles
```

Incorrect:

```text
/users/123
/accounts/999/roles
```

Fallback when the route pattern is empty (unmatched routes, 404s, direct handlers outside chi):

```text
unknown
```

### Response Writer Wrapper

Minimal wrapper to capture status:

```go
type responseWriter struct {
    http.ResponseWriter
    status      int
    wroteHeader bool
}

func (rw *responseWriter) WriteHeader(code int) {
    if rw.wroteHeader {
        return
    }
    rw.status = code
    rw.wroteHeader = true
    rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
    if !rw.wroteHeader {
        rw.status = http.StatusOK
        rw.wroteHeader = true
    }
    return rw.ResponseWriter.Write(b)
}
```

Must also expose `Flush`, `Hijack`, and `Push` via interface assertions on the inner writer, to preserve chi/SSE/HTTP-2 behavior.

---

## Storage Instrumentation

### Storage Goal

Automatically instrument `magic/storage` so every service using `storage` gets tracing (on contextual calls) and metrics (on all calls) without adding code beyond the `observability.Init` call.

### Storage Instrumentation Strategy

Instrumentation is implemented by wrapping the storage adapter internally in `StorageAdapterFactory.GetInstance(...)`. The external storage API is unchanged for legacy callers. Callers that want tracing use the `Context`-suffixed methods from the `ContextualStorageAdapter` interface.

### Storage Tracing

Each contextual storage operation creates a child span on the caller's context.

Span names:

* `storage.create`
* `storage.get`
* `storage.list`
* `storage.search`
* `storage.update`
* `storage.delete`
* `storage.count`
* `storage.query`
* `storage.execute`
* `storage.ping`

Schema/migration methods (`CreateSchema`, `CreateMigrationTable`, `UpdateMigrationTable`, `GetLatestMigration`) are not instrumented — see "Storage Context Propagation".

Attributes:

* `db.system` — e.g. `"postgresql"`, `"dynamodb"` (OTEL semconv)
* `magic.storage.provider` — the `StorageProviders` constant
* `magic.storage.operation` — the span name minus `storage.`
* `magic.storage.model` — the concrete type name of `item` / `dest`, reflected at the call site
* `magic.storage.limit` — for `list`/`search`/`query` only
* `magic.storage.sort_field` — for `list`/`search` only

Additional backend-specific attributes may be added if they are stable and low-cardinality.

Errors must:

* be recorded on the span via `span.RecordError(err)`
* set error status on the span via `span.SetStatus(codes.Error, err.Error())`

### Storage Metrics

Built-in storage metrics:

* `magic_storage_operations_total` — counter
* `magic_storage_operation_duration_seconds` — histogram, buckets: `{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}`
* `magic_storage_operation_errors_total` — counter

Labels:

* `provider`
* `operation`
* `status` — `"ok"` or `"error"`

Storage duration uses the same sub-10 ms extended bucket set as HTTP, since point reads and cache-backed operations frequently complete in single-digit milliseconds.

Examples:

```text
magic_storage_operations_total{provider="dynamodb",operation="get",status="ok"}
magic_storage_operation_duration_seconds{provider="postgresql",operation="search"}
magic_storage_operation_errors_total{provider="mysql",operation="update",status="error"}
```

### Legacy Adapter Fallback

When `GetInstance` wraps an adapter that does **not** implement `ContextualStorageAdapter`, or when the caller uses a non-context method on any adapter:

* No span is created (avoids orphan root spans).
* Metrics are still recorded, derived from wall-clock timing and the operation name.
* A `telemetry.WarnOnce` is issued at first use naming the adapter type and linking to the migration docs.

### Storage Design Constraints

* no API changes to the existing `StorageAdapter` interface
* new `ContextualStorageAdapter` is the extension point
* metric labels remain small and stable
* no labeling by raw query, record key, tenant ID, or user ID

---

## PubSub Instrumentation (Publish-Only in v1)

### PubSub Goal

Automatically instrument publish flows so that services emitting events participate in distributed tracing and emit useful metrics.

Consumer / subscribe / process / ack / nack instrumentation is explicitly deferred to a follow-up design once a `Consumer` interface exists in the `pubsub` package. The shape of this work is captured in Future Enhancements.

### PubSub Instrumentation Strategy

Introduce a `ContextualPublisher` extension interface:

```go
package pubsub

import "context"

type ContextualPublisher interface {
    Publisher
    PublishContext(ctx context.Context, topic, message string, params map[string]any) error
}
```

`Publish` delegates to `PublishContext(context.Background(), ...)`. Every in-repo publisher (`sns.go`) implements both and has its real logic in `PublishContext`.

`PublisherFactory.GetInstance(...)` returns an instrumented wrapper when observability is active. The wrapper applies the same capability check as the storage wrapper and the same fallback behavior.

### PubSub Tracing

Span name: `pubsub.publish`.

Behavior:

* start a client span on the caller's context
* inject trace context into outbound message metadata via the configured propagator
* record message publish errors on the span

Attributes (OTEL semconv):

* `messaging.system` — e.g. `"aws_sns"`
* `messaging.destination.name` — the topic
* `messaging.operation` — `"publish"`
* `magic.pubsub.provider` — the `PublisherType` constant

Optional attributes when safe:

* `messaging.message.body.size`

### Context Propagation: SNS Specifics

SNS message attributes are limited to 10 per message. The propagator (default W3C) adds `traceparent` and, when present, `tracestate` and `baggage`. This consumes up to 3 attributes.

* The wrapper reads and writes back the shared params-map key `pubsub.MessageAttributesParamKey` (string value `"MessageAttributes"`) whose value is a `map[string]string`. In-repo publishers (currently just SNS) translate that map into their native per-system representation.
* User-supplied keys are authoritative and are never overwritten.
* Propagator keys are inserted in priority order `traceparent` → `tracestate` → `baggage`. The wrapper stops as soon as the merged count hits 10 and emits a warn-once log event naming the dropped header. In the degenerate case where the caller already fills the cap with user attributes, every propagator key is dropped (traceparent included) rather than ever displacing a caller-supplied key.
* Teams using B3 or Jaeger propagation must set `cfg.Propagator` explicitly; the wrapper defers to whatever propagator is configured.

### PubSub Metrics

Built-in publish metrics:

* `magic_pubsub_messages_total` — counter
* `magic_pubsub_publish_duration_seconds` — histogram, buckets: `{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}` (the Prometheus default)
* `magic_pubsub_errors_total` — counter

Labels:

* `provider`
* `destination`
* `operation` — always `"publish"` in v1
* `status` — `"ok"` or `"error"`

Publish is network-bound and rarely faster than a few milliseconds, so the Prometheus default bucket set is sufficient.

Examples:

```text
magic_pubsub_messages_total{provider="sns",destination="orders",operation="publish",status="ok"}
magic_pubsub_publish_duration_seconds{provider="sns",destination="orders",operation="publish"}
magic_pubsub_errors_total{provider="sns",destination="orders",operation="publish",status="error"}
```

### Cardinality Note on `destination`

SNS destinations are topic ARNs. ARNs embed the AWS account ID, which is bounded per service but adds one label value per account. Teams operating with many accounts (multi-tenant) should be aware that `destination` label cardinality tracks the number of distinct topics across all accounts the service publishes to. The wrapper does not strip ARNs by default; if needed, this can be addressed in a future release with a normalization hook.

### PubSub Design Constraints

* no API changes to the existing `Publisher` interface
* `ContextualPublisher` is the extension point
* context propagation is automatic when using `PublishContext`
* no labeling by message ID, tenant ID, or user ID by default

---

## Custom Metrics

### Custom Metrics Goal

Allow service authors to define business metrics using the same observability pipeline as built-in metrics.

Custom metrics must work in both supported metrics modes:

* Prometheus
* OTLP

### Custom Metrics Design Principles

1. **One instrumentation API for consumers.** Service authors should not need to import Prometheus or OTEL metric SDKs directly for normal usage.
2. **Backend-neutral metric definition.** A custom counter, histogram, gauge, or up-down counter is defined once and emitted the same way regardless of export mode.
3. **Safe by default.** Stable names, declared labels, reusable instruments, startup registration.
4. **Built-in and custom metrics share the pipeline.** HTTP, storage, pubsub, runtime, and user-defined metrics all flow through the same backend abstraction.

### Metric Kind

```go
type MetricKind string

const (
    CounterKind       MetricKind = "counter"
    HistogramKind     MetricKind = "histogram"
    GaugeKind         MetricKind = "gauge"
    UpDownCounterKind MetricKind = "updowncounter"
)
```

### Metric Definition

```go
type MetricDefinition struct {
    Name        string
    Description string
    Unit        string
    Kind        MetricKind
    LabelKeys   []string
    Buckets     []float64 // histogram only
}
```

### Label

```go
type Label struct {
    Key   string
    Value string
}

func Labels(kv ...string) []Label
```

Usage:

```go
ordersCreated.Add(ctx, 1, telemetry.Labels(
    "status", "success",
    "channel", "web",
)...)
```

### Observer API

```go
func (o *Observer) Counter(def telemetry.MetricDefinition) (telemetry.Counter, error)
func (o *Observer) Histogram(def telemetry.MetricDefinition) (telemetry.Histogram, error)
func (o *Observer) Gauge(def telemetry.MetricDefinition) (telemetry.Gauge, error)
func (o *Observer) UpDownCounter(def telemetry.MetricDefinition) (telemetry.UpDownCounter, error)
```

### Example

```go
ordersCreated, err := obs.Counter(telemetry.MetricDefinition{
    Name:        "orders_created_total",
    Description: "Total number of orders created",
    Kind:        telemetry.CounterKind,
    LabelKeys:   []string{"status", "channel"},
})
if err != nil {
    log.Fatal(err)
}

checkoutLatency, err := obs.Histogram(telemetry.MetricDefinition{
    Name:        "checkout_duration_seconds",
    Description: "Checkout duration in seconds",
    Unit:        "seconds",
    Kind:        telemetry.HistogramKind,
    LabelKeys:   []string{"result"},
    Buckets:     []float64{0.05, 0.1, 0.25, 0.5, 1, 2, 5},
})
if err != nil {
    log.Fatal(err)
}

activeConnections, err := obs.UpDownCounter(telemetry.MetricDefinition{
    Name:      "active_connections",
    Kind:      telemetry.UpDownCounterKind,
    LabelKeys: []string{"protocol"},
})
```

Recording:

```go
ordersCreated.Add(ctx, 1,
    telemetry.Label{Key: "status", Value: "success"},
    telemetry.Label{Key: "channel", Value: "web"},
)

checkoutLatency.Record(ctx, duration.Seconds(),
    telemetry.Label{Key: "result", Value: "success"},
)
```

### Namespace Scope

`Config.MetricNamespace`, when non-empty, is applied **only to custom metrics registered through `Observer.Counter`/`Histogram`/`Gauge`/`UpDownCounter`**. Built-in HTTP, storage, pubsub, Go runtime, and process metrics keep their canonical names so that shared dashboards remain portable.

### Validation Rules

Metric registration validates:

#### Name

* non-empty
* matches `^[a-zA-Z_][a-zA-Z0-9_]*$`
* must not collide with a built-in metric name

#### Labels

* label keys declared up front in `LabelKeys`
* runtime labels must match declared keys exactly when `AllowUndeclaredLabels` is false (the default)
* label ordering is irrelevant; the backend sorts keys internally before lookup

#### Buckets

* only permitted when `Kind == HistogramKind`
* defaults apply if omitted — custom histograms without explicit `Buckets` get the Prometheus default: `{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}`
* built-in histograms (HTTP, storage, pubsub) ship with per-family bucket sets defined in their respective sections; these are not overridable in v1

#### Duplicate Registration

Two registrations are compatible when their canonical shape is equal:

* `Name` equal
* `Kind` equal
* sorted `LabelKeys` equal
* `Unit` equal
* for histograms, sorted `Buckets` equal (within float epsilon)

Compatible re-registrations return the existing instrument. `Description` differences are tolerated and logged at debug.

Incompatible re-registrations return an error.

### Best Practices

* register custom metrics at startup
* store metric handles and reuse them
* avoid registering metrics dynamically inside request handlers
* avoid high-cardinality label values

Good:

```go
var ordersCreated telemetry.Counter

func initMetrics(obs *observability.Observer) error {
    var err error
    ordersCreated, err = obs.Counter(telemetry.MetricDefinition{
        Name:      "orders_created_total",
        Kind:      telemetry.CounterKind,
        LabelKeys: []string{"status"},
    })
    return err
}
```

Bad:

```go
func handler(w http.ResponseWriter, r *http.Request) {
    c, _ := obs.Counter(...)
    c.Add(r.Context(), 1)
}
```

### Concurrency

All `Counter`, `Histogram`, `Gauge`, and `UpDownCounter` implementations are safe for concurrent use. Registration (`Observer.Counter` / `Observer.Histogram` / …) is also safe for concurrent use and is idempotent for compatible shapes.

---

## Metrics Export Modes

### 1. Prometheus Mode

Default. Simplest.

Behavior:

* tracing uses OTEL if enabled
* metrics are backed by native Prometheus collectors via `prometheus/client_golang`
* `/metrics` is exposed via `MetricsHandler()`
* Go runtime and process metrics are collected via the standard Prometheus collectors

Best for:

* teams already scraping Prometheus endpoints
* simplest adoption path
* lowest friction for `magic` consumers

### 2. OTLP Mode

Behavior:

* tracing uses OTEL and exports over OTLP
* metrics use OTEL meters and export over OTLP
* `MetricsHandler()` returns a 404 handler — never `nil` — so registering `/metrics` stays safe

Best for:

* teams with a central OTEL Collector pipeline
* environments where metrics are pushed, not scraped
* users who want OTEL-only for both tracing and metrics

### Migration Between Modes

Teams moving between Prometheus and OTLP should do so at the collector, not in the library. The recommended path is:

* stand up the OTEL Collector with a Prometheus **receiver** scraping the existing `/metrics` endpoint
* have the Collector emit OTLP (or anything else) downstream
* once all services and dashboards consume the Collector's output, flip the library to `MetricsModeOTLP`

This avoids in-process dual-export and its double-counting, allocation, and bucket-alignment problems.

---

## Runtime and Process Metrics

In scrape-based modes, the observability module registers:

* Go runtime metrics — `collectors.NewGoCollector(collectors.WithGoCollectorRuntimeMetrics(collectors.MetricsAll))`
* Process metrics — `collectors.NewProcessCollector(collectors.ProcessCollectorOpts{})`

Controlled by:

* `DisableGoMetrics`
* `DisableProcessMetrics`

In OTLP-only mode, Go runtime metrics are emitted via `go.opentelemetry.io/contrib/instrumentation/runtime`.

These metrics are part of the near-zero-touch value proposition and require no consumer code.

---

## Error Rate

The system does not define a separate stored metric called `error_rate`.

Error rate is derived from counters:

* `http_requests_total{status=~"5.."}`
* `magic_storage_operation_errors_total`
* `magic_pubsub_errors_total`

This keeps the metrics model simple and matches standard Prometheus / OTEL practice.

---

## Logger Correlation

### Scope

v1 modifies the existing `logger` package to automatically inject `trace_id` and `span_id` into every log line that is produced with an active span in its context. No call-site changes are required beyond using the `*Context` variants of `slog` (`slog.InfoContext`, `slog.ErrorContext`, etc.) that the `slog` API already encourages.

Enrichment is always on once `logger.Init` runs. When no span is active, the handler performs a single `SpanContext.IsValid()` check and delegates unchanged. There is no coupling to `observability.Init` — if tracing is never enabled, no span ever lives in context, and the handler simply passes through.

### Mechanism

`logger.Init` wraps the underlying `slog.Handler` with a trace-aware handler:

```go
package logger

import (
    "context"
    "log/slog"

    "go.opentelemetry.io/otel/trace"
)

type traceHandler struct{ slog.Handler }

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
    if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
        r.AddAttrs(
            slog.String("trace_id", sc.TraceID().String()),
            slog.String("span_id", sc.SpanID().String()),
        )
    }
    return h.Handler.Handle(ctx, r)
}

// WithAttrs and WithGroup delegate to the inner handler, preserving slog semantics.
func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
    return &traceHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
    return &traceHandler{Handler: h.Handler.WithGroup(name)}
}
```

`logger.Init` becomes:

```go
func Init(config *Config) {
    var handler slog.Handler
    if config.JSON {
        handler = slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: config.Level})
    } else {
        handler = slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: config.Level})
    }
    handler = &traceHandler{Handler: handler}
    slog.SetDefault(slog.New(handler))
}
```

### Usage

No new API is required for the common path. Any caller that already uses `slog.InfoContext(ctx, ...)` / `slog.ErrorContext(ctx, ...)` automatically gets correlated logs when a span is active:

```go
func getOrder(w http.ResponseWriter, r *http.Request) {
    slog.InfoContext(r.Context(), "fetching order", "id", chi.URLParam(r, "id"))
    // log line includes trace_id and span_id when the ChiMiddleware has started a span
}
```

### Non-Context Calls

`slog.Info(...)` without context passes `context.Background()` to the handler, so no span is found and no trace fields are emitted. This is expected and matches `slog`'s design. The implication is:

* Code that wants correlated logs must use `*Context` variants.
* Pre-existing `slog.Info`/`slog.Error` call sites keep working, they just won't carry trace IDs.
* This is a gentle forcing function toward context-aware logging, which is already idiomatic in Go 1.21+.

### Escape Hatch: `LoggerFromContext`

For call sites that cannot easily use `*Context` variants (for example, a non-`slog` logger passed through a third-party library), `observability.LoggerFromContext` returns an `*slog.Logger` pre-populated with trace fields:

```go
package observability

func LoggerFromContext(ctx context.Context, l *slog.Logger) *slog.Logger
```

If the context has no active span, the input logger is returned unchanged. This is a secondary helper; the primary mechanism is the auto-wrapping handler.

### Field Names

* `trace_id` — hex-encoded 16-byte ID
* `span_id` — hex-encoded 8-byte ID

Names chosen to match OTEL `logs` semantic conventions so that correlation works with downstream log pipelines that already understand OTEL.

### Non-Goals for v1

* OTEL logs export (deferred; this section wires trace IDs into the existing `slog` stdout path only)
* Sampling-decision propagation into log records (deferred)

---

## Testing Support

### `NewTestObserver`

In `observability/obstest`:

```go
package obstest

type TestObserver struct {
    *observability.Observer
}

func New(t *testing.T) *TestObserver

// Assertions
func (o *TestObserver) Metrics() []RecordedMetric
func (o *TestObserver) Spans() []RecordedSpan

// Assertion helpers
func (o *TestObserver) AssertCounter(t *testing.T, name string, labels map[string]string, expected float64)
func (o *TestObserver) AssertHistogramObserved(t *testing.T, name string, labels map[string]string)
func (o *TestObserver) AssertSpan(t *testing.T, name string) RecordedSpan
```

### Test Observer Behavior

* Installs an in-memory `MetricsBackend` (not Prometheus, not OTEL) that records every operation.
* Installs an OTEL `TracerProvider` backed by the in-memory SDK `tracetest.SpanRecorder`.
* Automatically registers cleanup via `t.Cleanup` to reset `telemetry.Global()`.
* Safe to use in `t.Parallel()` tests because each `TestObserver` scopes its telemetry to a context via `telemetry.WithContext`, and the in-repo instrumented adapters resolve telemetry from context first, then global. (This is the one place where context-scoped telemetry matters.)

---

## Initialization Flow

`Init(ctx, cfg)` performs:

1. apply defaults
2. validate required config
3. initialize the tracer provider if tracing is enabled; set the global propagator
4. initialize the metrics backend if metrics are enabled
5. register runtime and process metrics if applicable
6. register built-in HTTP, storage, and pubsub instruments
7. install `telemetry.SetGlobal(...)` so `storage` and `pubsub` wrappers see the telemetry
8. return an initialized `Observer`

Pseudo-flow:

```go
func Init(ctx context.Context, cfg Config) (*Observer, error) {
    cfg = applyDefaults(cfg)
    if err := validateConfig(cfg); err != nil {
        return nil, err
    }

    // buildTracerProvider resolves the sampler:
    //   if cfg.Sampler != nil: sdktrace.ParentBased(cfg.Sampler)
    //   else:                  sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))
    tp, tpShutdown, err := buildTracerProvider(ctx, cfg)
    if err != nil {
        return nil, err
    }
    otel.SetTracerProvider(tp)
    otel.SetTextMapPropagator(cfg.Propagator)

    backend, bShutdown, err := buildMetricsBackend(ctx, cfg)
    if err != nil {
        return nil, err
    }

    if cfg.MetricsEnabled {
        if err := registerBuiltinMetrics(backend, cfg); err != nil {
            return nil, err
        }
        if err := registerRuntimeMetrics(backend, cfg); err != nil {
            return nil, err
        }
    }

    t := telemetry.Telemetry{
        TracerProvider: tp,
        Metrics:        backend,
    }
    telemetry.SetGlobal(t)

    return &Observer{
        telemetry:   t,
        shutdownFns: []func(context.Context) error{tpShutdown, bShutdown},
    }, nil
}
```

---

## Package Integration Strategy

### Storage Integration

The `storage` package defines `ContextualStorageAdapter` and `storage/telemetry.go`. The instrumented wrapper is applied at `StorageAdapterFactory.GetInstance` time when `telemetry.Global().Metrics != nil`.

The wrapper resolves telemetry per-call via `telemetry.FromContextOrGlobal(ctx)` so that:

* test harnesses can scope telemetry to a context
* the wrapper picks up `telemetry.SetGlobal` even if the adapter was constructed before `Init` ran

### PubSub Integration

Same pattern: `ContextualPublisher`, `pubsub/telemetry.go`, wrapper applied at `PublisherFactory.GetInstance` time.

### Behavior When Observability Is Not Enabled

If a service does not initialize observability:

* `telemetry.Global()` returns the no-op implementation
* `storage` and `pubsub` wrappers short-circuit to the underlying adapter without extra work
* no tracing or metrics are emitted
* package behavior is byte-for-byte identical to today

This keeps observability opt-in and prevents any regression for existing consumers.

---

## Example Consumer Usage

### Default Prometheus Mode

```go
obs, err := observability.Init(ctx, observability.Config{
    ServiceName:       "orders-api",
    ServiceVersion:    "1.0.0",
    Environment:       "prod",
    TracingEnabled:    true,
    MetricsEnabled:    true,
    MetricsMode:       observability.MetricsModePrometheus,
    MetricsPath:       "/metrics",
    OTLPTraceEndpoint: "otel-collector:4317",
    OTLPInsecure:      true,
})
if err != nil {
    log.Fatal(err)
}
defer obs.Shutdown(ctx)

r := chi.NewRouter()
r.Use(obs.ChiMiddleware())

r.Get("/orders/{id}", getOrder)
r.Post("/orders", createOrder)

r.Handle("/metrics", obs.MetricsHandler())
```

### OTEL-Only Mode for Tracing and Metrics

```go
obs, err := observability.Init(ctx, observability.Config{
    ServiceName:       "orders-api",
    ServiceVersion:    "1.0.0",
    Environment:       "prod",
    TracingEnabled:    true,
    MetricsEnabled:    true,
    MetricsMode:       observability.MetricsModeOTLP,
    OTLPTraceEndpoint: "otel-collector:4317",
    OTLPInsecure:      true,
})
if err != nil {
    log.Fatal(err)
}
defer obs.Shutdown(ctx)

r := chi.NewRouter()
r.Use(obs.ChiMiddleware())
// /metrics is still safe to register; it serves a 404 in this mode.
r.Handle("/metrics", obs.MetricsHandler())
```

### Using the Contextual Storage API

```go
func getOrder(w http.ResponseWriter, r *http.Request) {
    var order Order
    err := storageAdapter.(storage.ContextualStorageAdapter).
        GetContext(r.Context(), &order, map[string]any{"id": chi.URLParam(r, "id")})
    if err != nil {
        // span is already marked error; metrics already recorded
        http.Error(w, err.Error(), http.StatusInternalServerError)
        return
    }
    // ...
}
```

Since every adapter shipped in the `magic` repo implements `ContextualStorageAdapter` starting in Phase 2, the type assertion always succeeds for in-repo adapters. For defensive code against third-party adapters, use the two-value assertion and fall back to the non-ctx method.

---

## Recommended Built-In Metric Names

### HTTP

* `http_requests_total` — counter
* `http_request_duration_seconds` — histogram, buckets `{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}`

### Storage

* `magic_storage_operations_total` — counter
* `magic_storage_operation_duration_seconds` — histogram, buckets `{0.001, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}`
* `magic_storage_operation_errors_total` — counter

### PubSub

* `magic_pubsub_messages_total` — counter
* `magic_pubsub_publish_duration_seconds` — histogram, buckets `{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}` (Prometheus default)
* `magic_pubsub_errors_total` — counter

### Runtime / Process

Standard collectors for Go runtime and process metrics in scrape-based modes; `go.opentelemetry.io/contrib/instrumentation/runtime` in OTLP-only mode.

---

## Implementation Plan

### Phase 1: Neutral Telemetry Package + Core Bootstrap

* create `telemetry` package with interfaces, no-op implementations, and `Global`/`SetGlobal`
* create `observability` package
* add config, init, shutdown
* add metrics mode support (Prometheus, OTLP)
* add tracer provider initialization and propagator configuration
* implement `prometheusMetricsBackend` and `otelMetricsBackend`
* add chi middleware (with trailing-edge recording)
* add metrics handler (with 404 fallback for OTLP mode)
* register runtime and process metrics

### Phase 2: Storage Instrumentation + Contextual Migration

* add `ContextualStorageAdapter` interface in `storage/telemetry.go`
* migrate all in-repo adapters (`sql.go`, `dynamodb.go`, `cosmosdb.go`, `memory.go`, and the pending Cassandra adapter) to implement both interfaces, with real logic in the `Context` variants and non-ctx methods as one-line delegates
* implement instrumented adapter wrapper with capability check and warn-once on legacy adapters
* wire wrapper into `StorageAdapterFactory.GetInstance`
* add built-in storage spans and metrics
* add unit tests against `NewTestObserver`

### Phase 3: PubSub Publish Instrumentation

* add `ContextualPublisher` interface in `pubsub/telemetry.go`
* migrate `sns.go` to implement both interfaces (rename file `publlisher.go` → `publisher.go` in the same PR — trivial cleanup)
* implement instrumented publisher wrapper with capability check and SNS attribute-limit handling
* wire wrapper into `PublisherFactory.GetInstance`
* add built-in pubsub publish spans and metrics
* expose `pubsub.MessageAttributesParamKey` so callers and other in-repo publishers agree on the trace-context carrier key
* add unit tests (span on success/error, propagator injection, user-key preservation, attribute-limit drop, legacy-publisher metrics-only)

### Phase 4: Custom Metrics + Logger Correlation

* add `MetricDefinition` validation and duplicate-registration normalization
* add `Counter` / `Histogram` / `Gauge` / `UpDownCounter` implementations across all backends
* add `telemetry.Labels` helper
* add the `traceHandler` wrap inside `logger.Init` so every log line with a valid span context carries `trace_id` and `span_id`
* add `observability.LoggerFromContext` escape hatch
* document custom metrics usage patterns

### Phase 5: Testing Harness + Hardening

* implement `observability/obstest.NewTestObserver` with in-memory backends
* add span recorder integration
* add assertion helpers
* add tests across both metrics modes
* add benchmarks for chi middleware and metric record hot paths (target: < 1 µs per op without tracing, < 5 µs with tracing, on a typical dev laptop)
* document best practices, cardinality rules, migration examples

---

## Testing Strategy

### Bootstrap

* defaults are applied correctly
* invalid config is rejected
* tracing-only, metrics-only, and both modes initialize correctly
* `SamplingRatio` pointer semantics are respected (nil vs 0 vs > 0)
* when `Sampler` is set, `SamplingRatio` is ignored and the sampler is wrapped in `ParentBased`
* `MetricsMode = OTLP` produces a non-nil 404-serving `MetricsHandler`

### HTTP Middleware

* chi route pattern is used instead of raw path
* unmatched routes emit `route="unknown"`
* status code is captured correctly
* spans are created when tracing is enabled
* ignored routes skip both span and metrics
* panics in downstream handlers are re-raised with error-tagged span and 500-labeled metrics

### Storage Instrumentation Tests

* all supported operations emit spans when called through the `Context` variants
* duration and error metrics are recorded for both `Context` and non-`Context` calls
* legacy (non-contextual) adapter emits metrics only, no spans, and logs a warn-once
* provider, operation, and status labels are applied correctly

### PubSub Instrumentation Tests

* `PublishContext` injects the configured propagator's fields
* SNS attribute-limit handling drops `baggage` first, then `tracestate`, never `traceparent`
* publish metrics are recorded with correct provider/destination/status
* legacy publisher gets metrics-only treatment

### Custom Metrics Tests

* registration succeeds for valid definitions
* duplicate compatible registrations return the same instrument
* conflicting registrations (kind, labels, or buckets) fail
* undeclared labels fail when `AllowUndeclaredLabels` is false
* namespace is applied only to custom metrics

### Metrics Modes

* Prometheus mode exposes scrape endpoint with built-in and custom metrics
* OTLP mode pushes to the configured collector and `MetricsHandler()` serves 404

### Logger Correlation Tests

* `slog.InfoContext(ctx, ...)` with an active span produces log lines containing `trace_id` and `span_id`
* `slog.InfoContext(ctx, ...)` with no active span produces log lines without those fields
* `slog.Info(...)` (no-context variant) produces log lines without those fields even when a span is active in a surrounding goroutine
* `WithAttrs` and `WithGroup` on the wrapped handler preserve trace injection after chaining
* JSON and text handlers both emit the fields correctly
* `observability.LoggerFromContext` returns a logger with the same fields pre-populated for non-`*Context` call sites

---

## Risks and Mitigations

### Risk: High cardinality metrics

Mitigation:

* chi route patterns, not raw paths
* declared label keys enforced by default
* no tenant IDs, user IDs, resource IDs, or message IDs

### Risk: Import cycle / heavy dependency footprint

Mitigation:

* neutral `telemetry` package hosts interfaces only
* `storage` and `pubsub` import `telemetry` only (no Prometheus or OTEL deps)
* consumers who never call `observability.Init` never pay the Prometheus/OTEL cost

### Risk: Adapters not migrated to `ContextualStorageAdapter`

Mitigation:

* all in-repo adapters are migrated in Phase 2
* legacy third-party adapters fall back to metrics-only with a warn-once
* documentation and the warn-once message link to the migration recipe

### Risk: Hidden behavior after `Init`

Mitigation:

* observability is explicitly initialized by the service
* package instrumentation activates only after `telemetry.SetGlobal`
* no-op behavior before `Init`, after `Shutdown`, and in tests that omit `Init`

### Risk: Breaking existing consumers

Mitigation:

* existing `StorageAdapter` and `Publisher` interfaces are unchanged
* `ContextualStorageAdapter` and `ContextualPublisher` are extension interfaces
* default behavior without `Init` is byte-for-byte identical to today

### Risk: SNS message-attribute limit

Mitigation:

* propagator fields merged without overwriting user keys
* drop order defined (`baggage`, then `tracestate`, never `traceparent`)
* warn-once when truncation occurs

### Risk: Middleware ordering and panic handling

Mitigation:

* metrics and span finalization happen in a deferred block after `next.ServeHTTP`, so chi's route pattern is populated
* middleware does not `recover()`; panics are recorded, re-raised, and safe to handle with any upstream recover middleware

---

## Future Enhancements

Potential additions after v1, in rough priority order:

* **PubSub consumer instrumentation.** Once a `Consumer`/`Subscriber` interface lands in the `pubsub` package, add `pubsub.consume` / `pubsub.process` / `pubsub.ack` spans, context extraction from inbound messages, and corresponding metrics.
* **Health-check instrumentation.** Spans and metrics for `health.Check` calls.
* **OTEL logs integration.** Once OTEL Go logs SDK is stable for the exporters we need.
* **Exemplars.** Link metrics to traces for backends that support it (Prometheus exemplars, OTEL exemplars over OTLP).
* **Route-level metric suppression helpers.** E.g. `obs.SuppressRoute("/internal/*")` at registration time rather than per-config.
* **Scoped `Meter("orders")` helpers** for optional metric prefix scoping.
* **ARN/topic normalization hook** for pubsub `destination` label cardinality control.

### Explicitly Deferred Metrics Export Modes

Two modes were considered for v1 and deliberately cut:

* **OTEL Prometheus exporter mode** (OTEL SDK emitting to `/metrics`). The same operational outcome is achievable by running the OTEL Collector with a Prometheus receiver in front of services configured in `MetricsModeOTLP`. Adding it in-process was not worth the second code path, testing surface, and subtle output-format differences from native `client_golang`.
* **Dual export mode** (simultaneous Prometheus + OTLP from the same process). Migration is better handled at the Collector: scrape the existing `/metrics` with a Prometheus receiver, emit OTLP downstream, then flip the library to `MetricsModeOTLP` once dashboards consume the Collector's output. In-process dual export's double-counting risk and per-record overhead were not worth supporting.

Both modes can be added later as new `MetricsMode` values without breaking existing consumers.

---

## Summary

This design introduces a first-class observability layer for `magic` that provides:

* OTEL-based distributed tracing with W3C context propagation
* two metrics export modes: Prometheus (scrape) and OTLP (push)
* automatic instrumentation for HTTP, storage (via `ContextualStorageAdapter`), and pubsub publish (via `ContextualPublisher`)
* near-zero-touch infrastructure telemetry without breaking existing consumers
* first-class custom business metrics with safe-by-default label validation
* a backend-neutral metrics API that works across Prometheus or OTEL
* automatic trace/span correlation in application logs via the `logger` package's `slog` handler wrap
* an in-memory testing harness for unit tests

The result is a complete observability foundation for services built on `magic`:

* easy to adopt
* safe by default
* non-breaking for current consumers
* powerful without requiring every service to reinvent instrumentation
* flexible enough to support both Prometheus-first and OTEL-first teams
