# Observability

magic gives you OpenTelemetry traces and metrics with one `Init` call. HTTP requests, storage operations, and pubsub publishes are instrumented automatically. Go runtime and process metrics show up for free. You add custom metrics and spans where it matters.

## Setup (5 minutes)

main.go

```
import (
    "context"

    "github.com/tink3rlabs/magic/observability"
    "github.com/tink3rlabs/magic/middlewares"
)

cfg := observability.DefaultConfig()
cfg.ServiceName = "tasks-svc"
cfg.MetricsMode = observability.MetricsModePrometheus

obs, err := observability.Init(context.Background(), cfg)
if err != nil {
    log.Fatal(err)
}
defer obs.Shutdown(context.Background())

r := chi.NewRouter()
r.Use(middlewares.Observability(obs))
r.Handle("/metrics", obs.MetricsHandler())
```

That's it. You now get:

- A span per HTTP request, tagged with the chi route pattern (not raw URL — keeps cardinality bounded).
- A storage operation span and counter for every `ContextualStorageAdapter` call that receives a context.
- Default HTTP metrics (`http_requests_total`, `http_request_duration_seconds`).
- Go runtime and process metrics on `/metrics`.
- `trace_id` and `span_id` injected into every `slog.*Context` log line (if you initialize `logger.Init` too).

## Configure with env vars

`observability.Init` reads OTLP endpoints from environment variables when the corresponding `Config` fields are empty:

| Variable                              | Purpose                                        |
| ------------------------------------- | ---------------------------------------------- |
| `OTEL_EXPORTER_OTLP_ENDPOINT`         | Fallback endpoint for both traces and metrics. |
| `OTEL_EXPORTER_OTLP_TRACES_ENDPOINT`  | Trace-only override.                           |
| `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` | Metric-only override (OTLP mode).              |

For OTLP push mode, set `cfg.MetricsMode = observability.MetricsModeOTLP` and `cfg.EnableTracing = true`. OTLP mode also needs a metrics endpoint — set `cfg.MetricsOTLPEndpoint` or one of the `OTEL_EXPORTER_OTLP_[METRICS_]ENDPOINT` env vars above, or `Init` fails fast with `MetricsMode=otlp but no OTLP metrics endpoint configured`.

Prometheus vs OTLP — pick one

In Prometheus mode, `/metrics` returns the scrape format. In OTLP mode, `/metrics` returns 404 by design — metrics are pushed, not scraped.

## Custom metrics

Register metrics once on startup, then call `Add` or `Observe` on the returned instrument.

metrics.go

```
import "github.com/tink3rlabs/magic/telemetry"

ordersCreated, err := obs.Counter(telemetry.MetricDefinition{
    Name:   "orders_created_total",
    Help:   "Orders created, by channel and result.",
    Kind:   telemetry.KindCounter,
    Labels: []string{"channel", "result"},
})
if err != nil {
    log.Fatal(err)
}

orderLatency, err := obs.Histogram(telemetry.MetricDefinition{
    Name:    "order_processing_seconds",
    Help:    "Time to process an order.",
    Kind:    telemetry.KindHistogram,
    Labels:  []string{"channel"},
    Buckets: []float64{0.01, 0.05, 0.1, 0.5, 1, 5},
})
```

At observation time, use `telemetry.Labels` to build the label set:

```
ordersCreated.Add(1, telemetry.Labels("channel", "api", "result", "ok")...)
orderLatency.Observe(0.42, telemetry.Labels("channel", "api")...)
```

Strict labels by default

Observations with label keys not declared in `Labels` are dropped and a one-shot warning is logged. To loosen this (not recommended in production), set `cfg.AllowUndeclaredLabels = true`.

## Custom spans

Pull a tracer from the `Observer` and start spans inside your handlers.

routes/orders.go

```
tracer := obs.TracerProvider().Tracer("example.com/tasks-svc/orders")

func (h *OrdersHandler) Create(w http.ResponseWriter, r *http.Request) error {
    ctx, span := tracer.Start(r.Context(), "orders.create")
    defer span.End()
    span.SetAttributes(attribute.String("orders.channel", "api"))

    if err := h.store.Create(order); err != nil {
        span.RecordError(err)
        span.SetStatus(codes.Error, "storage_create_failed")
        return err
    }
    return nil
}
```

Pass `ctx` (not `r.Context()`) into anything downstream so it picks up the parent span.

## Troubleshooting

**No traces in your backend.** Check `OTEL_EXPORTER_OTLP_ENDPOINT` (or the trace-specific override) points at a reachable collector, and that `cfg.EnableTracing = true`. If you're using plaintext gRPC, set `cfg.TracesOTLPInsecure = true`.

**`/metrics` returns 404.** You're in OTLP mode. Either switch `cfg.MetricsMode` to `MetricsModePrometheus`, or scrape your collector's exposition endpoint instead of the app.

**Custom metric observations silently disappear.** You added a label key that wasn't declared in `MetricDefinition.Labels`. Either add the key to the declaration or relax with `cfg.AllowUndeclaredLabels = true`. Check the warning logs — magic logs once per unknown key.

**Storage spans missing.** Adapters only emit spans when called via `ContextualStorageAdapter` methods (`CreateContext`, `GetContext`, etc.). Pre-context call sites (`s.Create(item)`) work but won't link to the request trace.

**Init returns `invalid MetricsMode ""`.** `cfg.MetricsMode` was zero-valued. It's required — set it to `MetricsModePrometheus` or `MetricsModeOTLP`.

______________________________________________________________________

**Advanced:** the design rationale, package layout, and full instrumentation surface live in [observability-internals.md](https://tink3rlabs.github.io/magic/main/observability-internals/index.md).
