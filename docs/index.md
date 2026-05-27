# magic

Every Go microservice rebuilds the same plumbing: CRUD over a database, list-with-search endpoints, observability, typed errors, auth middleware, health probes. magic gives you those as one coherent library so your service code can stay about your domain.

One storage interface for memory, SQL, DynamoDB, and CosmosDB. One Lucene-backed `?filter=` syntax that compiles to safe parameterized queries on every backend. OpenTelemetry traces and metrics with one `Init` call. Typed HTTP errors that map themselves to status codes.

## See it

```go title="main.go"
package main

import (
	"errors"
	"fmt"

	"github.com/tink3rlabs/magic/storage"
	magicerrors "github.com/tink3rlabs/magic/errors"
)

type Task struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
}

func main() {
	// setup errors elided for brevity; the Search below shows real error handling
	store, _ := storage.StorageAdapterFactory{}.GetInstance(storage.MEMORY, nil)
	// in-memory is SQLite with no tables yet — a real service migrates; here we create one
	_ = store.Execute(`CREATE TABLE IF NOT EXISTS tasks (id TEXT PRIMARY KEY, title TEXT, status TEXT)`)
	_ = store.Create(Task{ID: "1", Title: "Pour foundation", Status: "open"})

	var hits []Task
	if _, err := store.Search(&hits, "id", "status:open", 10, ""); err != nil {
		var bad *magicerrors.BadRequest
		if errors.As(err, &bad) {
			fmt.Println("bad filter:", bad.Message)
		}
		return
	}
	fmt.Printf("matched %d tasks\n", len(hits))
}
```

Swap `storage.MEMORY` for `storage.SQL` (with a config map) and the same code runs against Postgres.

## Pick your path

- **[Quick start](getting-started.md)** — install, run a script, see Lucene search work in under a minute.
- **[Tutorial](tutorial.md)** — build a real microservice in 15 minutes: handlers, auth, health, observability.
- **[Reference](https://pkg.go.dev/github.com/tink3rlabs/magic)** — full API on pkg.go.dev.

## What's in the box

`errors`, `health`, `leadership`, `logger`, `middlewares`, `mql`, `observability`, `pubsub`, `storage`, `telemetry`, `types`, `utils`. The headline packages have dedicated guides above; the rest are documented on [pkg.go.dev](https://pkg.go.dev/github.com/tink3rlabs/magic).

Requires Go 1.25 or newer. Pre-v1.0: the API may change in minor releases.
