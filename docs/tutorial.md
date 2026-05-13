# Build a microservice in 15 minutes

You will build a small **tasks** service: create a task, list tasks with a Lucene filter, behind health probes and JWT auth — all using magic. When you're done, this works:

```bash
curl -s 'http://localhost:8080/tasks?filter=status:open%20AND%20priority:%5B1%20TO%203%5D' \
  -H "Authorization: Bearer $TOKEN"
```

Total moving parts: one `main.go`, one `routes/tasks.go`, and a `go.mod`. Then we swap memory for SQL with one line.

## Step 1: Project setup

```bash
mkdir tasks-svc && cd tasks-svc
go mod init example.com/tasks-svc
go get github.com/tink3rlabs/magic@latest
go get github.com/go-chi/chi/v5 github.com/go-chi/render github.com/google/uuid
```

Layout:

```text
tasks-svc/
  go.mod
  main.go
  routes/
    tasks.go
```

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
