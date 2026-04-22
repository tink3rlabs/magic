# Examples: Storage + Observability

The sample app in `examples/main.go` demonstrates:

- storage CRUD using the `storage` package
- observability bootstrap via `observability.Init(...)`
- HTTP middleware instrumentation via `middlewares.Observability(obs)`
- custom metric registration (`orders_created_total`)
- logger bootstrap via `logger.Init(...)` and `slog` request logging

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
- Docker (optional, for local OTLP collector)

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
curl -i -X POST http://localhost:8080/orders
curl -i http://localhost:8080/orders/<id-from-post-response>
```

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
  --network=host \
  -v "/workspaces/magic/examples/prometheus.yml:/etc/prometheus/prometheus.yml:ro" \
  prom/prometheus
```

Then open <http://localhost:9090> and query:

- `http_requests_total`
- `magic_storage_operations_total`
- `orders_created_total`

---

## Run with OTLP Metrics + Traces

Start the local collector (config in `examples/otel-collector.yml`):

```bash
docker run --rm --name otelcol-magic-example \
  --network=host \
  -v "/workspaces/magic/examples/otel-collector.yml:/etc/otelcol-contrib/config.yaml:ro" \
  otel/opentelemetry-collector-contrib:latest
```

Run the app in OTLP mode:

```bash
METRICS_MODE=otlp ENABLE_TRACING=true OTLP_ENDPOINT=localhost:4317 LOGGER_LEVEL=debug LOGGER_JSON=true go run ./examples
```

Generate traffic:

```bash
curl -i http://localhost:8080/health/liveness
curl -i http://localhost:8080/health/readiness
curl -s http://localhost:8080/api-docs
curl -i -X POST http://localhost:8080/orders
curl -i http://localhost:8080/orders/<id-from-post-response>
```

### Verify metrics in OTLP mode

In OTLP mode, the app's `/metrics` endpoint intentionally returns 404.
Metrics are exported to the collector, which re-exposes them at `:9464`:

```bash
curl -s http://localhost:9464/metrics | grep -E "http_requests_total|magic_storage_operations_total|orders_created_total"
```

### Verify traces are working

The collector config exports traces to the `debug` exporter. Check collector logs:

```bash
docker logs otelcol-magic-example
```

You should see trace export entries after hitting `/orders` routes.

### Verify log correlation (`trace_id` / `span_id`)

When tracing is enabled and logs use `slog.*Context`, the logger wrapper injects `trace_id` and `span_id`.

In this example, the `/orders` handlers log via `slog.InfoContext` / `slog.ErrorContext`.

Run with JSON logs:

```bash
METRICS_MODE=otlp ENABLE_TRACING=true OTLP_ENDPOINT=localhost:4317 LOGGER_LEVEL=debug LOGGER_JSON=true go run ./examples
```

Then hit:

```bash
curl -i -X POST http://localhost:8080/orders
curl -i http://localhost:8080/orders/<id-from-post-response>
```

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
