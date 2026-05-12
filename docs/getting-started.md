# Getting Started

By the end of this page you will have built a Go program that uses magic's in-memory storage adapter to create, fetch, list, and search records. The same code works against Postgres, MySQL, SQLite, DynamoDB, or CosmosDB by swapping one constant — see [Storage Adapters](./storage.md).

## Requirements

- Go 1.25 or newer.

## Install

```bash
go get github.com/tink3rlabs/magic@latest
```

## A first program

Create a file called `main.go`:

```go
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

    // 5. Search with a Lucene filter (status field, exact match).
    parser, err := lucene.NewParser(Task{})
    if err != nil {
        log.Fatal(err)
    }
    _ = parser // parser is what your HTTP handler would hold across requests.

    var hits []Task
    cursor, err = adapter.Search(&hits, "id", "status:in_progress", 10, "")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println("searched:", hits, "next:", cursor)
}
```

Run it:

```bash
go run main.go
```

Expected output:

```text
got: {02 Frame walls in_progress}
listed: 3 cursor:
searched: [{02 Frame walls in_progress}] next:
```

## What just happened

- **Factory** — `StorageAdapterFactory{}.GetInstance` returns an adapter that conforms to `storage.StorageAdapter`. The result is wrapped with telemetry instrumentation; if you need the concrete `*SQLAdapter` for things like registering GORM plugins, use [`storage.UnwrapAdapter`](https://pkg.go.dev/github.com/tink3rlabs/magic/storage#UnwrapAdapter).
- **`sortKey`** — pass the JSON/column name (`"id"`), not the Go struct field name (`"ID"`). `magic` validates it against `^[a-zA-Z_][a-zA-Z0-9_]*$` to prevent injection.
- **Cursor pagination** — `List` and `Search` return an opaque cursor string. Pass it back as the `cursor` argument to fetch the next page. An empty cursor (`""`) means there are no more pages.
- **Sort direction** — defaults to ascending. Pass `map[string]any{storage.SortDirectionKey: "DESC"}` as the final variadic argument to flip it.
- **Lucene** — `lucene.NewParser` introspects your struct via the `json` tags and figures out which fields are searchable. The adapter's `Search` method uses this when you pass a query string. See [Search (Lucene)](./lucene.md) for the full syntax.

## Next steps

- Swap the in-memory adapter for SQL or DynamoDB → [Storage Adapters](./storage.md).
- Learn the full Lucene filter syntax → [Search (Lucene)](./lucene.md).
- Wire up observability → [Observability](./observability.md).
- See the [API reference on pkg.go.dev](https://pkg.go.dev/github.com/tink3rlabs/magic).
