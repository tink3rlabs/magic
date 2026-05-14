# Build a microservice with magic

You will build **todo-service** — the canonical reference service for magic — and come away understanding it layer by layer. todo-service is a small but complete microservice: a CRUD API for todo items that exercises magic's features end to end. The source lives at [`tink3rlabs/todo-service`](https://github.com/tink3rlabs/todo-service).

Every snippet below is from [`tink3rlabs/todo-service`](https://github.com/tink3rlabs/todo-service) at commit `2c27181`.

!!! note "Pinned to a commit, for now"
    todo-service hasn't cut a tagged release yet, so this tutorial pins to commit `2c27181`. Once todo-service tags a release, this will be re-pinned to that tag.

todo-service is a properly layered service, and that layering is the spine of this tutorial. We walk it in the order the request flows — and the order you'd build it:

**migrations → types → features → routes → server**

- **migrations** — the database schema, applied at startup.
- **types** — the data types and their OpenAPI annotations.
- **features** — the business logic.
- **routes** — the HTTP routing that wires features to URLs.
- **server** — the bootstrap that assembles everything: storage, observability, health probes, auth, and the router.

When you're done you have a running service you can `curl`: health probes answer, full CRUD on `/todos` works, and a Lucene `?filter=` query returns matching todos.

## Get the code

```bash
git clone https://github.com/tink3rlabs/todo-service.git
cd todo-service
go mod download
```

Here's the map before the walk:

```text
todo-service/
  main.go              # entrypoint — embeds config, hands off to the cobra CLI
  cmd/                  # cobra commands — root.go wires viper; server.go runs the service
  pkg/
    types/              # data types (Todo) plus their OpenAPI annotations
    features/           # business logic — the todo feature package
    routes/             # HTTP routing — maps /todos verbs onto the feature
  config/
    development.yaml    # service configuration (storage, auth, observability, ...)
    migrations/         # startup migrations, one set per SQL provider
    openapi.json        # generated OpenAPI spec artifact
  build/
    generate.go         # generates config/openapi.json from the type annotations
```

!!! tip "It runs with no external services"
    todo-service defaults to the in-memory storage adapter, with auth and pub/sub disabled. `go run . server --config config/development.yaml` starts it with no database, no tokens, and no AWS credentials.

## Migrations

The first layer is the schema — the shape of the data the rest of the service sits on. magic's migrations describe that schema as ordered SQL, applied automatically at startup.

todo-service has exactly one table, created by one migration:

```yaml title="config/migrations/postgresql/01__base.yaml"
---
description: Create the todos table
migrations:
  - migrate: >
      CREATE TABLE IF NOT EXISTS todos (
        id TEXT PRIMARY KEY,
        summary TEXT NOT NULL,
        done BOOLEAN NOT NULL DEFAULT FALSE
      )
    rollback: DROP TABLE IF EXISTS todos
```

### The file format

Each migration file is YAML with two top-level keys:

- **`description`** — a human-readable summary of what the file does.
- **`migrations`** — an ordered list of migration steps. Each step has:
    - **`migrate`** — the SQL that applies the change (here, creating the `todos` table).
    - **`rollback`** — the SQL that undoes it (dropping the table).

Both statements use `IF NOT EXISTS` / `IF EXISTS` so they're idempotent: re-running a migration that's already applied — or rolling back one that's already gone — is a no-op rather than an error.

The filename prefix (`01__`) orders the files. Add a schema change later as `02__add_due_date.yaml` and it runs after `01__base.yaml`.

### One directory per provider

```text
config/migrations/
  postgresql/01__base.yaml
  mysql/01__base.yaml
  sqlite/01__base.yaml
```

SQL dialects differ — Postgres spells the id column `TEXT`, MySQL wants `VARCHAR(255)`, SQLite stores `done` as `INTEGER` — so each provider gets its own directory. magic picks the directory matching the configured storage provider. The three columns (`id`, `summary`, `done`) map directly onto the `Todo` type the next section covers.

### How the schema gets applied

The migration files ship inside the binary. `main.go` embeds the whole `config` tree and hands it to magic's storage package:

```go title="main.go"
//go:embed config
var configFS embed.FS

func main() {
	storage.ConfigFs = configFS
	// ...
}
```

At startup, `runServer` builds the storage adapter and then runs any pending migrations against it:

```go title="cmd/server.go"
storage.NewDatabaseMigration(storageAdapter).Migrate()
```

The Server section later covers the full bootstrap; for now the point is just that migrations run automatically, before the first request is served. This holds even for the default in-memory adapter used in local dev — it runs the same migrations, so the service behaves identically whether it's backed by Postgres or an in-process store. For the adapter details, see [Storage Adapters](storage.md).

!!! note "Migrations run on every start"
    `Migrate()` applies only the migrations that haven't run yet and is safe to call on every boot. A fresh database gets the full schema; an up-to-date one is left untouched.

With the schema defined, the next layer is the **types** — the Go structs that model a todo and carry the OpenAPI annotations magic generates the spec from.

## Types

The second layer is the data shapes the rest of the service is built around. Every layer above this one — features, routes, the generated OpenAPI spec — refers back to these structs. todo-service defines all three in a single file:

```go title="pkg/types/todo.go"
package types

// @openapi
// components:
//
//	schemas:
//	  Todo:
//	    type: object
//	    properties:
//	      id:
//	        type: string
//	        description: The Todo's identifier
//	        example: 01909c42-cc90-75dc-a943-2d87a16e787d
//	      summary:
//	        type: string
//	        description: The Todo's summary
//	        example: Pick up the groceries
//	      done:
//	        type: boolean
//	        description: An indicator that tells if the Todo item is complete
//	        example: false
type Todo struct {
	Id      string `json:"id"`
	Summary string `json:"summary"`
	Done    bool   `json:"done"`
}

// @openapi
// components:
//
//	schemas:
//	  TodoUpdate:
//	    type: object
//	    properties:
//	      summary:
//	        type: string
//	        description: The Todo's summary
//	        example: Pick up the groceries
//	      done:
//	        type: boolean
//	        description: An indicator that tells if the Todo item is complete
//	        example: false
type TodoUpdate struct {
	Summary string `json:"summary"`
	Done    bool   `json:"done"`
}

// @openapi
// components:
//
//	schemas:
//	  TodoList:
//	    type: object
//	    properties:
//	      todos:
//	        type: array
//	        items:
//	          $ref: '#/components/schemas/Todo'
//	      next:
//	        type: string
//	        description: An identifier to use when requesting the next set of todos
//	        example: MDE5MDlhOGUtNjcwNi03NWY1LWJjMjUtNWM0MjY0ZjUwZTQ1
type TodoList struct {
	Todos []Todo `json:"todos"`
	Next  string `json:"next"`
}
```

### The `json` tags do double duty

The `json` struct tags are not just for serialization. magic's storage layer reads them too — they are the field and column names it uses, not the Go field names. The `Todo.Id` field is `json:"id"`, so storage knows it as `id`. That's the same `id` you saw as the primary key in the Migrations section, and it's the literal string passed as the sort key in the feature layer:

```go
next, err := t.storage.List(&todos, "id", map[string]any{}, limit, cursor)
```

The same tags decide what's searchable. When a Lucene `?filter=` query names a field, it names the `json` tag — `summary:groceries`, not `Summary:groceries`. magic introspects the struct once and builds the set of searchable fields from the tagged fields and their Go types. The Features section puts this to work; for the exact rules — which types are implicitly searchable, how `json:"-"` excludes a field — see [Search (Lucene)](lucene.md).

### The `@openapi` annotation blocks

Each struct is preceded by an `@openapi` comment block holding a fragment of OpenAPI YAML. These are not documentation for humans — they're the source of the generated spec. `build/generate.go` runs [`openapi-godoc`](https://github.com/tink3rlabs/openapi-godoc), which scans the package for `@openapi` comments, then calls `types.MergeOpenAPIDefinitions` to fold in magic's shared definitions. The result is written to `config/openapi.json`.

The schema names declared in these blocks — `Todo`, `TodoList`, `TodoUpdate` — are the contract. The route handlers reference them by name in their own `@openapi` annotations (request bodies, responses), and those references only resolve because the names are defined here. The Routes section covers that side.

!!! note "Keep the struct and the annotation in sync"
    The `@openapi` block and the Go struct are maintained by hand, side by side. If you add a field to a struct, add it to the annotation too — nothing cross-checks them, and the generated spec is only as accurate as the comment.

### Three types, three roles

- **`Todo`** — the full record: `id`, `summary`, `done`. This is what storage persists and what list/get endpoints return.
- **`TodoUpdate`** — the create/replace request body. It's `Todo` without `id` — the server owns identity, so the client never sends it.
- **`TodoList`** — the list response shape: a `todos` array plus a `next` cursor for pagination.

With the shapes defined, the next layer is **features** — the business logic that creates, reads, updates, and searches todos.

## Step 2: Wire `main.go`

This is the whole bootstrap — storage, observability, health probes, auth, routes.

```go title="main.go"
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/render"

	magicerrors "github.com/tink3rlabs/magic/errors"
	"github.com/tink3rlabs/magic/health"
	magiclogger "github.com/tink3rlabs/magic/logger"
	"github.com/tink3rlabs/magic/middlewares"
	"github.com/tink3rlabs/magic/observability"
	"github.com/tink3rlabs/magic/storage"

	"example.com/tasks-svc/routes"
)

func main() {
	magiclogger.Init(&magiclogger.Config{Level: slog.LevelInfo})

	cfg := observability.DefaultConfig()
	cfg.ServiceName = "tasks-svc"
	cfg.MetricsMode = observability.MetricsModePrometheus
	obs, err := observability.Init(context.Background(), cfg)
	if err != nil {
		magiclogger.Fatal("observability init failed", slog.Any("error", err))
	}
	defer obs.Shutdown(context.Background())

	store, err := storage.StorageAdapterFactory{}.GetInstance(storage.MEMORY, nil)
	if err != nil {
		magiclogger.Fatal("storage init failed", slog.Any("error", err))
	}

	tasks, err := routes.NewTasksHandler(store)
	if err != nil {
		magiclogger.Fatal("tasks handler init failed", slog.Any("error", err))
	}

	r := chi.NewRouter()
	r.Use(
		render.SetContentType(render.ContentTypeJSON),
		middleware.Recoverer,
		middlewares.ObservabilityWithOptions(obs, middlewares.ObservabilityOptions{
			SkipPathPrefixes: []string{"/health/"},
		}),
	)

	// Health probes — unauthenticated.
	r.Get("/health/liveness", func(w http.ResponseWriter, r *http.Request) {
		render.Status(r, http.StatusNoContent)
		render.NoContent(w, r)
	})
	checker := health.NewHealthChecker(store)
	errHandler := middlewares.ErrorHandler{}
	r.Get("/health/readiness", errHandler.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		if err := checker.Check(true, nil); err != nil {
			return &magicerrors.ServiceUnavailable{Message: err.Error()}
		}
		render.Status(r, http.StatusNoContent)
		render.NoContent(w, r)
		return nil
	}))

	r.Handle("/metrics", obs.MetricsHandler())

	// Protected routes.
	r.Group(func(r chi.Router) {
		r.Use(middlewares.EnsureValidToken(middlewares.EnsureValidTokenConfig{
			Enabled:   os.Getenv("AUTH_ENABLED") == "true",
			IssuerURL: os.Getenv("OIDC_ISSUER"),
			Audience:  []string{os.Getenv("OIDC_AUDIENCE")},
		}))
		r.Get("/tasks", errHandler.Wrap(tasks.List))
		r.Post("/tasks", errHandler.Wrap(tasks.Create))
	})

	slog.Info("listening on :8080")
	if err := http.ListenAndServe(":8080", r); err != nil {
		magiclogger.Fatal("server stopped", slog.Any("error", err))
	}
}
```

!!! note "Do I need leadership election?"
    Only if your service runs background workers that must execute on **exactly one** replica — schedulers, reconcilers, cron-like jobs. For request-handling services like this one, skip it. When you do need it: `leadership.NewLeaderElection(leadership.LeaderElectionProps{StorageAdapter: store}).Start()` and read from `Results` to know when you've been elected.

## Step 3: Add a handler

The handler reads `?filter=<lucene>` straight off the query string and hands it to `storage.Search`. magic compiles the Lucene to safe parameterized SQL — no string concatenation, no injection.

```go title="routes/tasks.go"
package routes

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/render"
	"github.com/google/uuid"

	magicerrors "github.com/tink3rlabs/magic/errors"
	"github.com/tink3rlabs/magic/storage"
)

type Task struct {
	ID       string `json:"id"`
	Title    string `json:"title"`
	Status   string `json:"status"`
	Priority int    `json:"priority"`
}

type TasksHandler struct {
	store storage.StorageAdapter
}

func NewTasksHandler(store storage.StorageAdapter) (*TasksHandler, error) {
	return &TasksHandler{store: store}, nil
}

func (h *TasksHandler) List(w http.ResponseWriter, r *http.Request) error {
	filter := r.URL.Query().Get("filter")
	cursor := r.URL.Query().Get("cursor")

	var tasks []Task
	next, err := h.store.Search(&tasks, "id", filter, 50, cursor)
	if err != nil {
		return &magicerrors.BadRequest{Message: err.Error()}
	}
	render.JSON(w, r, map[string]any{"items": tasks, "cursor": next})
	return nil
}

func (h *TasksHandler) Create(w http.ResponseWriter, r *http.Request) error {
	var t Task
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		return &magicerrors.BadRequest{Message: "invalid json body"}
	}
	if t.Title == "" {
		return &magicerrors.BadRequest{Message: "title is required"}
	}
	t.ID = uuid.NewString()
	if t.Status == "" {
		t.Status = "open"
	}
	if err := h.store.Create(t); err != nil {
		return err
	}
	render.Status(r, http.StatusCreated)
	render.JSON(w, r, t)
	return nil
}
```

The typed errors map to HTTP status codes automatically. `&errors.BadRequest{...}` becomes a 400; `&errors.NotFound{...}` becomes a 404. The `ErrorHandler.Wrap` middleware does that translation — your handler just returns the error.

!!! tip "Empty filter is fine"
    `Search` with an empty filter string returns everything (subject to limit/cursor). You don't need to branch in the handler.

## Step 4: Run it

```bash
go run .
```

In another shell:

```bash
# liveness
curl -i http://localhost:8080/health/liveness

# create a task (auth disabled locally)
curl -s -X POST http://localhost:8080/tasks \
  -H 'Content-Type: application/json' \
  -d '{"title":"Pour foundation","status":"open","priority":2}'

# list with a Lucene filter
curl -s 'http://localhost:8080/tasks?filter=status:open%20AND%20priority:%5B1%20TO%203%5D'
```

That last URL decodes to `filter=status:open AND priority:[1 TO 3]`. Full syntax: [Lucene](lucene.md).

## Step 5: Swap memory for SQL

This is the payoff. Change exactly the storage factory call:

```go title="main.go (storage init only)"
store, err := storage.StorageAdapterFactory{}.GetInstance(storage.SQL, map[string]string{
	"provider": "postgresql",
	"host":     "localhost",
	"port":     "5432",
	"user":     "postgres",
	"password": "admin",
	"dbname":   "tasks",
	"schema":   "public",
})
```

Your handler code doesn't change. Lucene queries compile to parameterized Postgres SQL. Health, observability, and auth keep working.

!!! warning "Schema lifecycle"
    On a fresh database, call `store.CreateSchema()` and `storage.NewDatabaseMigration(store).Migrate()` once at startup — see the [examples](https://github.com/tink3rlabs/magic/tree/main/examples) for the full migration pattern.

## Where to next

- **Filter syntax** — every operator, every provider quirk: [Lucene](lucene.md).
- **Pick an adapter** — SQL vs DynamoDB vs CosmosDB tradeoffs: [Storage Adapters](storage.md).
- **Metrics and traces** — custom metrics, OTLP, troubleshooting: [Observability](observability.md).
- **Help us improve magic** — [Contributing](contributing.md).
