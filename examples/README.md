# Examples: Storage + Observability

The sample app in `examples/main.go` demonstrates:

- storage CRUD using the `storage` package
- observability bootstrap via `observability.Init(...)` (or `observability.New`, same behavior)
- HTTP middleware instrumentation via `middlewares.Observability(obs)`
- custom metric registration (`orders_created_total`)
- logger bootstrap via `logger.Init(...)` and `slog` request logging

Note: to get `trace_id` / `span_id` on request logs, keep request logging middleware
after `middlewares.Observability(obs)` in the chi middleware chain.

It exposes:

- `POST /orders` (creates an item in storage)
- `GET /orders/{id}` (reads item from storage)
- `GET /health/liveness` (always 204)
- `GET /health/readiness` (checks storage ping, 204/503)
- `GET /api-docs` (minimal OpenAPI JSON document)
- `/metrics` (Prometheus mode only)

---

## Prerequisites

- Go installed
- `grep` for quick metric filtering
- `jq` (optional, for parsing JSON in the curl examples below)
- Docker (optional, for local OTLP collector)

### Optional: trace outbound HTTP from your own code

Server-side spans come from `middlewares.Observability`. For **client** calls, wrap the transport (add module `go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp` to your app):

```go
import "go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

client := &http.Client{Transport: otelhttp.NewTransport(http.DefaultTransport)}
```

This is separate from the example’s chi server instrumentation.

---

## Run with Prometheus Metrics (quickest path)

From repo root:

```bash
METRICS_MODE=prometheus ENABLE_TRACING=false LOGGER_LEVEL=info LOGGER_JSON=false go run ./examples
```

In another terminal, generate traffic:

```bash
curl -i http://localhost:8080/health/liveness
curl -i http://localhost:8080/health/readiness
curl -s http://localhost:8080/api-docs
ORDER_ID=$(curl -sf -X POST http://localhost:8080/orders | jq -r '.id')
curl -i "http://localhost:8080/orders/${ORDER_ID}"
```

Without `jq`, create an order with `curl -i -X POST http://localhost:8080/orders` and copy the `id` field from the JSON body for the GET URL.

Verify metrics from the app directly:

```bash
curl -s http://localhost:8080/metrics | grep -E "http_requests_total|magic_storage_operations_total|orders_created_total"
```

You should see series for:

- `http_requests_total` (HTTP middleware)
- `magic_storage_operations_total` (storage instrumentation)
- `orders_created_total` (custom metric from app code)

### Optional: view with Prometheus UI

The repo includes `examples/prometheus.yml`:

```bash
docker run --rm --name prom-magic-example \
  -p "9090:9090" \
  -v "./examples/prometheus.yml:/etc/prometheus/prometheus.yml:ro" \
  prom/prometheus
```

Then open <http://localhost:9090> and query:

- `http_requests_total`
- `magic_storage_operations_total`
- `orders_created_total`

---

## Run with OTLP Metrics + Traces (with Jaeger UI)

Start Jaeger (OTLP gRPC receiver + UI):

```bash
docker run --rm --name jaeger-magic-example \
  -p "16686:16686" \
  -p "4318:4317" \
  -v "$(pwd)/examples/jaeger.json:/etc/jaeger/ui-config.json:ro" \
  jaegertracing/all-in-one:latest \
  --query.ui-config /etc/jaeger/ui-config.json
```

Open the trace UI at <http://localhost:16686>.

Start the local collector (config in `examples/otel-collector.yml`):

```bash
docker run --rm --name otelcol-magic-example \
  -p "4317:4317" \
  -p "9464:9464" \
  -v "./examples/otel-collector.yml:/etc/otelcol-contrib/config.yaml:ro" \
  otel/opentelemetry-collector-contrib:latest
```

This collector setup:

- receives app OTLP metrics + traces on `:4317`
- re-exposes metrics on `:9464` (Prometheus format)
- forwards traces to Jaeger OTLP endpoint on `host.docker.internal:4318`

Run the app in OTLP mode:

```bash
METRICS_MODE=otlp ENABLE_TRACING=true OTLP_ENDPOINT=host.docker.internal:4317 LOGGER_LEVEL=debug LOGGER_JSON=true go run ./examples
```

Generate traffic:

```bash
curl -i http://localhost:8080/health/liveness
curl -i http://localhost:8080/health/readiness
curl -s http://localhost:8080/api-docs
ORDER_ID=$(curl -sf -X POST http://localhost:8080/orders | jq -r '.id')
curl -i "http://localhost:8080/orders/${ORDER_ID}"
```

Without `jq`, create an order with `curl -i -X POST http://localhost:8080/orders` and copy the `id` field from the JSON body for the GET URL.

### Verify metrics in OTLP mode

In OTLP mode, the app's `/metrics` endpoint intentionally returns 404.
Metrics are exported to the collector, which re-exposes them at `:9464`:

```bash
curl -s http://localhost:9464/metrics | grep -E "http_requests_total|magic_storage_operations_total|orders_created_total"
```

### Verify traces are working

Collector still exports traces to `debug` too, so you can inspect raw output:

```bash
docker logs otelcol-magic-example
```

You should see trace export entries after hitting `/orders` routes.

To visualize traces in Jaeger:

1. Open <http://localhost:16686>
2. Select service `magic-storage-observability-example`
3. Click **Find Traces**

Expected span shape for a successful create request:

- `HTTP POST /orders` (server/root span from middleware)
- `orders.create` (example business span)
- `storage.create` (storage instrumentation span)

Expected span shape for a successful get request:

- `HTTP GET /orders/{id}`
- `orders.get`
- `storage.get`

### Verify log correlation (`trace_id` / `span_id`)

When tracing is enabled and logs use `slog.*Context`, the logger wrapper injects `trace_id` and `span_id`.

In this example, the `/orders` handlers log via `slog.InfoContext` / `slog.ErrorContext`.

Run with JSON logs:

```bash
METRICS_MODE=otlp ENABLE_TRACING=true OTLP_ENDPOINT=localhost:4317 LOGGER_LEVEL=debug LOGGER_JSON=true go run ./examples
```

Then hit:

```bash
ORDER_ID=$(curl -sf -X POST http://localhost:8080/orders | jq -r '.id')
curl -i "http://localhost:8080/orders/${ORDER_ID}"
```

Without `jq`, create an order with `curl -i -X POST http://localhost:8080/orders` and copy the `id` field from the JSON body for the GET URL.

You should see log lines containing `trace_id` and `span_id`.

---

## Notes

- `METRICS_MODE` supports:
  - `prometheus` (default)
  - `otlp`
- `ENABLE_TRACING=true` enables spans from HTTP + storage + pubsub instrumentation paths.
- Logger config (environment variables):
  - `LOGGER_LEVEL` -> `debug|info|warn|error` (default: `info`)
  - `LOGGER_JSON` -> `true|false` (default: `false`)
- For storage adapters outside this example, update the adapter config block in `examples/main.go`.
