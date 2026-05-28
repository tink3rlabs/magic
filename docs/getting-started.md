# Quick start

You'll have magic's in-memory adapter creating, fetching, listing, and searching records in under a minute. The same code runs against Postgres, MySQL, SQLite, DynamoDB, or CosmosDB by swapping one constant — see [Storage Adapters](storage.md).

Requires Go 1.25 or newer.

## Install

```bash
mkdir magic-quickstart && cd magic-quickstart
go mod init magic-quickstart
go get github.com/tink3rlabs/magic@latest
```

## Run this

```go title="main.go"
package main

import (
    "fmt"
    "log"

    "github.com/tink3rlabs/magic/storage"
    "github.com/tink3rlabs/magic/storage/search/lucene"
)

type Task struct {
    ID     string `json:"id"`
    Title  string `json:"title"`
    Status string `json:"status"`
}

func main() {
    // 1. Build an adapter. In-memory needs no config.
    adapter, err := storage.StorageAdapterFactory{}.GetInstance(storage.MEMORY, nil)
    if err != nil {
        log.Fatal(err)
    }

    // 1a. The in-memory adapter is a fresh SQLite database with no tables yet.
    // A real service creates tables via magic's migrations (see the Tutorial);
    // for this single-file demo we create the one table directly.
    if err := adapter.Execute(
        `CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY, title TEXT, status TEXT)`,
    ); err != nil {
        log.Fatal(err)
    }

    // 2. Create some rows.
    for _, t := range []Task{
        {ID: "01", Title: "Pour foundations", Status: "done"},
        {ID: "02", Title: "Frame walls", Status: "in_progress"},
        {ID: "03", Title: "Run electrical", Status: "pending"},
    } {
        if err := adapter.Create(t); err != nil {
            log.Fatal(err)
        }
    }

    // 3. Fetch one by exact filter.
    var task Task
    if err := adapter.Get(&task, map[string]any{"id": "02"}); err != nil {
        log.Fatal(err)
    }
    fmt.Println("got:", task)

    // 4. List a page of tasks, sorted by id.
    var page []Task
    cursor, err := adapter.List(&page, "id", nil, 10, "")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("listed:", len(page), "cursor:", cursor)

    // 5. Search with a Lucene filter.
    parser, err := lucene.NewParser(Task{})
    if err != nil {
        log.Fatal(err)
    }
    _ = parser // your HTTP handler would hold this across requests.

    var hits []Task
    cursor, err = adapter.Search(&hits, "id", "status:in_progress", 10, "")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("searched:", hits, "next:", cursor)
}
```

```bash
go mod tidy
go run main.go
```

You'll see:

```text
got: {02 Frame walls in_progress}
listed: 3 cursor:
searched: [{02 Frame walls in_progress}] next:
```

## What just happened

- **Factory** — `StorageAdapterFactory{}.GetInstance` returns a `storage.StorageAdapter`. The result is wrapped with telemetry instrumentation. If you need the concrete `*SQLAdapter` (for example, to register a GORM plugin), use [`storage.UnwrapAdapter`](https://pkg.go.dev/github.com/tink3rlabs/magic/storage#UnwrapAdapter).
- **`sortKey`** — pass the JSON/column name (`"id"`), not the Go struct field name (`"ID"`). magic validates it against `^[a-zA-Z_][a-zA-Z0-9_]*$` to block injection.
- **Cursor pagination** — `List` and `Search` return an opaque cursor. Pass it back to fetch the next page. An empty cursor means you're done.
- **Sort direction** — defaults to ascending. Flip with `map[string]any{storage.SortDirectionKey: "DESC"}` as the final variadic argument.
- **Lucene** — `NewParser` introspects your struct via `json` tags and figures out which fields are searchable. `Search` uses this when you pass a query string. Full syntax: [Lucene](lucene.md).

## Now what

You just tried magic. Two paths from here:

- **Want a real service?** Continue to the [tutorial](tutorial.md) — handlers, auth, health, observability, swap to SQL.
- **Want more depth?** [Storage Adapters](storage.md) covers each backend; [Lucene](lucene.md) covers every operator.
