---
template: home.html
hide:
  - navigation
  - toc
---

## Install

```bash
go get github.com/tink3rlabs/magic@latest
```

Requires Go 1.25 or newer. Pre-v1.0: the API may change in minor releases — see [Versioning & Releases](https://github.com/tink3rlabs/magic#versioning--releases).

## Start here

- **[Getting Started](./getting-started.md)** — install, build an adapter, do CRUD, paginate, search.
- **[Search (Lucene)](./lucene.md)** — full filter syntax reference with per-provider behavior.
- **[Storage Adapters](./storage.md)** — SQL / DynamoDB / CosmosDB / memory: when to use which, how to configure.
- **[Observability](./observability.md)** — logs, traces, metrics.
- **[API reference on pkg.go.dev](https://pkg.go.dev/github.com/tink3rlabs/magic)** — auto-generated, always current.

## Other packages

These are documented in the [repository README](https://github.com/tink3rlabs/magic#usage) and on [pkg.go.dev](https://pkg.go.dev/github.com/tink3rlabs/magic):

- `errors` — typed `NotFound`, `BadRequest`, `Unauthorized` with HTTP status mapping.
- `health` — readiness/liveness probe handlers.
- `leadership` — single-leader election for background workers.
- `logger` — structured logging conventions.
- `middlewares` — authentication, validation, error-handler middleware for `chi` / `net/http`.
- `mql` — a simpler query-string parser (distinct from the Lucene system above).
- `pubsub` — SNS publisher and friends.
- `telemetry` — OpenTelemetry plumbing used by the observability layer.
- `types` — shared response/request envelopes.
- `utils` — reverse-sorted UUID and other small helpers.
