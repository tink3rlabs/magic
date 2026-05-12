# magic

> Backend microservice scaffolding for Go — opinionated storage adapters, Lucene-based search, observability, and release tooling.

## What's in the box

- **Storage adapters** — DynamoDB, Postgres, MySQL, SQLite, Cassandra, in-memory.
- **Lucene search** — a single `filter=` query string per list endpoint; translates to safe parameterized SQL.
- **Observability** — structured logging, traces, metrics conventions.
- **Utils** — reverse-sorted UUID generation and friends.

## Install

```bash
go get github.com/tink3rlabs/magic@latest
```

Pre-v1.0: the API may change in minor releases. See [Versioning & Releases](https://github.com/tink3rlabs/magic#versioning--releases).

## Where to go next

- [Getting Started](./getting-started.md) — a 5-minute tour.
- [Search (Lucene)](./lucene.md) — query syntax reference.
- [Storage Adapters](./storage.md) — which backends are supported and how to wire them.
- [API reference on pkg.go.dev](https://pkg.go.dev/github.com/tink3rlabs/magic) — auto-generated, always current.
