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

## Features

The features layer is where the business logic lives. It sits between the types and the routes: it knows nothing about HTTP — no request parsing, no status codes — and everything about *what a todo operation means*. Every method here is a storage-backed operation expressed in terms of the structs from the Types section.

todo-service keeps the whole layer in one file:

```go title="pkg/features/todo/todoService.go"
package todo

import (
	"encoding/json"
	"log/slog"

	"github.com/google/uuid"
	"github.com/spf13/viper"

	"todo-service/pkg/types"

	"github.com/tink3rlabs/magic/logger"
	"github.com/tink3rlabs/magic/pubsub"
	"github.com/tink3rlabs/magic/storage"
	"github.com/tink3rlabs/magic/telemetry"
)

type TodoService struct {
	storage   storage.StorageAdapter
	created   telemetry.Counter
	publisher pubsub.Publisher
	topic     string
}

// WithPublisher attaches a pub/sub publisher; todo lifecycle events are published to topic.
func (t *TodoService) WithPublisher(p pubsub.Publisher, topic string) *TodoService {
	t.publisher = p
	t.topic = topic
	return t
}

// WithCreatedCounter attaches a metrics counter incremented on each successful create.
func (t *TodoService) WithCreatedCounter(c telemetry.Counter) *TodoService {
	t.created = c
	return t
}

func NewTodoService() *TodoService {
	storageAdapter, err := storage.StorageAdapterFactory{}.GetInstance(
		storage.StorageAdapterType(viper.GetString("storage.type")),
		viper.GetStringMapString("storage.config"),
	)

	if err != nil {
		logger.Fatal("failed to create TodoService instance", slog.Any("error", err.Error()))
	}
	t := TodoService{storage: storageAdapter}
	return &t
}

func (t *TodoService) ListTodos(limit int, cursor string) ([]types.Todo, string, error) {
	todos := []types.Todo{}
	next, err := t.storage.List(&todos, "id", map[string]any{}, limit, cursor)

	return todos, next, err
}

// SearchTodos returns todos matching a Lucene filter string, cursor-paginated.
// An empty filter returns everything (subject to limit/cursor).
func (t *TodoService) SearchTodos(filter string, limit int, cursor string) ([]types.Todo, string, error) {
	todos := []types.Todo{}
	next, err := t.storage.Search(&todos, "id", filter, limit, cursor)
	return todos, next, err
}

func (t *TodoService) GetTodo(id string) (types.Todo, error) {
	todo := types.Todo{}
	err := t.storage.Get(&todo, map[string]any{"id": id})
	return todo, err
}

func (t *TodoService) DeleteTodo(id string) error {
	return t.storage.Delete(&types.Todo{}, map[string]any{"id": id})
}

func (t *TodoService) UpdateTodo(todoToUpdate types.Todo) error {
	err := t.storage.Update(todoToUpdate, map[string]any{"id": todoToUpdate.Id})
	if err == nil {
		t.publishEvent("todo.updated", todoToUpdate)
	}
	return err
}

func (t *TodoService) CreateTodo(todoToCreate types.TodoUpdate) (types.Todo, error) {
	todo := types.Todo{}

	// Using UUIDv7 in order to easily support cursor based pagination without extra fields
	//
	// From the RFC (https://datatracker.ietf.org/doc/rfc9562/)
	//
	// UUIDv7 features a time-ordered value field derived from the widely
	// implemented and well-known Unix Epoch timestamp source, the number of
	// milliseconds since midnight 1 Jan 1970 UTC, leap seconds excluded.
	// Generally, UUIDv7 has improved entropy characteristics over UUIDv1
	// (Section 5.1) or UUIDv6 (Section 5.6).
	//
	// UUIDv7 values are created by allocating a Unix timestamp in
	// milliseconds in the most significant 48 bits and filling the
	// remaining 74 bits, excluding the required version and variant bits,
	// with random bits for each new UUIDv7 generated to provide uniqueness
	// as per Section 6.9.
	id, err := uuid.NewV7()
	if err != nil {
		return todo, err
	}

	todo.Id = id.String()
	todo.Summary = todoToCreate.Summary
	todo.Done = todoToCreate.Done

	err = t.storage.Create(todo)
	if err != nil {
		return todo, err
	}

	if t.created != nil {
		t.created.Add(1)
	}

	t.publishEvent("todo.created", todo)

	return todo, nil
}

func (t *TodoService) publishEvent(eventType string, todo types.Todo) {
	if t.publisher == nil {
		return
	}
	payload, err := json.Marshal(todo)
	if err != nil {
		slog.Error("failed to marshal todo event", slog.String("error", err.Error()))
		return
	}
	if err := t.publisher.Publish(t.topic, string(payload), map[string]any{"event_type": eventType}); err != nil {
		slog.Error("failed to publish todo event", slog.String("error", err.Error()), slog.String("event_type", eventType))
	}
}
```

### Constructing the service

`NewTodoService()` builds the one thing the layer can't work without: a `storage.StorageAdapter`. It doesn't pick an adapter itself — it reads `storage.type` and `storage.config` from viper and hands them to `storage.StorageAdapterFactory`, which returns the matching adapter (in-memory, SQL, DynamoDB, CosmosDB). The feature code is written against the `StorageAdapter` interface, so swapping `storage.type` from `memory` to `postgres` in config changes nothing in this file. See [Storage Adapters](storage.md) for the factory and the per-adapter config.

### The CRUD methods

`CreateTodo`, `GetTodo`, `UpdateTodo`, and `DeleteTodo` are thin — each one is a single storage call wrapped in just enough logic to be meaningful:

- **`CreateTodo`** mints an id, copies the `summary`/`done` from the `TodoUpdate` body, and persists the full `Todo`.
- **`GetTodo`** and **`DeleteTodo`** key off `map[string]any{"id": id}` — the same `id` json tag the storage layer reads from the struct.
- **`UpdateTodo`** replaces the record by id, and on success publishes a `todo.updated` event.

Identity is owned here, not by the client: `CreateTodo` generates the id with `uuid.NewV7()`. UUIDv7 is **time-ordered** — the most significant 48 bits are a Unix-millisecond timestamp — so rows sort chronologically by primary key. That's the property cursor pagination depends on, and it's why `ListTodos` and `SearchTodos` can paginate on `id` alone, with no separate `created_at` sort column.

### Two list paths: plain list vs. Lucene search

This service exposes **two** ways to read a collection, and the difference between them is the core lesson of this layer.

`ListTodos` is the **plain list**. It calls `storage.List` with a structured, exact-match filter map and cursor pagination:

```go
next, err := t.storage.List(&todos, "id", map[string]any{}, limit, cursor)
```

The third argument is a `map[string]any` of field/value pairs ANDed together as exact matches — here it's empty, so every todo is returned, a page at a time.

`SearchTodos` is the **search path**. It calls `storage.Search` with a single Lucene `filter` string:

```go
next, err := t.storage.Search(&todos, "id", filter, limit, cursor)
```

The caller passes one expressive query — `summary:groceries AND done:1` — and magic compiles it to safe, parameterized SQL. No string concatenation, no injection surface. An empty filter returns everything, just like `List`.

Both share the same shape: sort on `id`, page with `limit`/`next`. This mirrors the Blox-style convention — **one `filter` Lucene parameter, no typed query params** (`?done=true&summary=...`), and cursor pagination via `limit`/`next`. Callers compose their own filters instead of the service growing a query parameter per field.

!!! note "Boolean filters on the in-memory/SQLite adapter: use `done:1`"
    As of magic v0.17.1, the Lucene parser passes a filter term through as a string without coercing it to the struct field's Go type. Against the in-memory adapter (a SQLite-backed database), `done:true` becomes the SQL string param `"true"`, which never matches SQLite's `INTEGER` column — it stores `1`/`0`. Write `done:1` instead: SQLite's integer affinity coerces the `"1"` string and the match works. On Postgres, `done:true` works as written because the boolean column accepts it. todo-service's `todoService_test.go` asserts exactly this behaviour. See [Search (Lucene)](lucene.md) for the full filter syntax.

### Optional seams: counter and publisher

`TodoService` has two optional collaborators, attached through fluent setters:

- **`WithCreatedCounter`** plugs in a `telemetry.Counter`. `CreateTodo` increments it on each successful create — but only `if t.created != nil`, so leaving it unset is a no-op.
- **`WithPublisher`** plugs in a `pubsub.Publisher` and a topic. `publishEvent` is the helper that uses it: it marshals the todo to JSON and publishes a `todo.created` or `todo.updated` event with an `event_type` attribute — and returns immediately `if t.publisher == nil`.

Both are nil by default, so the service is fully functional with neither. They're the seams where observability and pub/sub plug in; the Server section covers how `cmd/server.go` actually wires them up.

With the business logic in place, the next layer is **routes** — the HTTP handlers that parse requests, call these methods, and shape the responses.

## Routes

The routes layer is the HTTP boundary. It turns the feature methods — which know nothing about HTTP — into a chi router: URLs and verbs map onto `TodoService` calls, request bodies are validated, and returned errors become status codes. todo-service keeps the router, its validation schemas, and all six handlers in one file.

The first half declares the JSON-schema validation maps, the two wiring structs, and `NewTodoRouter` — the constructor that assembles the router:

```go title="pkg/routes/todo.go"
package routes

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"todo-service/pkg/features/todo"
	"todo-service/pkg/types"

	"github.com/tink3rlabs/magic/errors"
	"github.com/tink3rlabs/magic/middlewares"
	"github.com/tink3rlabs/magic/pubsub"
	"github.com/tink3rlabs/magic/telemetry"
)

type TodoRouter struct {
	Router  *chi.Mux
	service *todo.TodoService
}

var createSchema = map[string]string{
	"body": `{
		"type": "object",
		"properties": {
			"summary": { "type": "string" },
			"done": { "type": "boolean" }
		},
		"required": ["summary"],
		"additionalProperties": false
	}`,
}

var replaceSchema = map[string]string{
	"body": `{
		"type": "object",
		"properties": {
			"summary": { "type": "string" },
			"done": { "type": "boolean" }
		},
		"required": ["summary", "done"],
		"additionalProperties": false
	}`,
	"params": `{
		"type": "object",
		"properties": {
			"id": { "type": "string" }
		},
		"required": ["id"]
	}`,
}

var idSchema = map[string]string{
	"params": `{
		"type": "object",
		"properties": {
			"id": { "type": "string" }
		},
		"required": ["id"]
	}`,
}

// AuthConfig carries the auth wiring for the todo routes.
type AuthConfig struct {
	Middleware func(http.Handler) http.Handler
	Enabled    bool
	WriteRole  string
}

// PubSubConfig carries the pub/sub wiring for the todo routes.
type PubSubConfig struct {
	Publisher pubsub.Publisher
	TopicARN  string
}

func NewTodoRouter(created telemetry.Counter, auth AuthConfig, pubSub PubSubConfig) *TodoRouter {
	t := TodoRouter{}
	h := middlewares.ErrorHandler{}
	v := middlewares.Validator{}

	router := chi.NewRouter()

	// Public reads.
	router.Get("/{id}", v.ValidateRequest(idSchema, h.Wrap(t.GetTodo)))
	router.Get("/", h.Wrap(t.ListTodos))

	// Protected writes — require a valid token (and the write role when auth is enabled).
	router.Group(func(r chi.Router) {
		r.Use(auth.Middleware)
		r.Use(middlewares.UserRequestContext)
		if auth.Enabled {
			r.Use(middlewares.RequireRole(auth.WriteRole))
		}
		r.Post("/", v.ValidateRequest(createSchema, h.Wrap(t.CreateTodo)))
		r.Put("/{id}", v.ValidateRequest(replaceSchema, h.Wrap(t.ReplaceTodo)))
		r.Patch("/{id}", v.ValidateRequest(idSchema, h.Wrap(t.UpdateTodo)))
		r.Delete("/{id}", v.ValidateRequest(idSchema, h.Wrap(t.DeleteTodo)))
	})

	t.Router = router
	service := todo.NewTodoService().WithCreatedCounter(created)
	if pubSub.Publisher != nil {
		service = service.WithPublisher(pubSub.Publisher, pubSub.TopicARN)
	}
	t.service = service

	return &t
}
```

### Public reads, protected writes

`NewTodoRouter` builds a single `chi.Mux` and splits it into two access tiers:

- **Public reads** — `GET /{id}` and `GET /` are registered straight on the router. Anyone can read a todo or list the collection; no token required.
- **Protected writes** — `POST`, `PUT`, `PATCH`, and `DELETE` live inside a `router.Group`, which applies middleware to just that subtree. Every write goes through `auth.Middleware` (magic's `EnsureValidToken` middleware, populated by the Server section) and `middlewares.UserRequestContext`, which lifts the caller's identity off the validated token into the request context. `middlewares.RequireRole(auth.WriteRole)` is added **only when `auth.Enabled` is true** — so local dev with auth disabled still accepts writes, while a deployed service enforces the write role.

`AuthConfig` and `PubSubConfig` are the wiring structs the constructor takes as input. The routes layer declares *what* it needs — an auth middleware, an optional publisher — and the Server section populates them from configuration.

### Schema validation before the handler

Each route is wrapped in `v.ValidateRequest`, where `v` is a `middlewares.Validator{}`. It takes a JSON-schema map and the handler, and validates the request *before* the handler runs:

- **`createSchema`** validates the `POST` body — `summary` required, `done` optional, no extra properties.
- **`replaceSchema`** validates both the `PUT` body (`summary` *and* `done` required) and the `id` path param.
- **`idSchema`** validates just the `id` path param, used by `GET /{id}`, `PATCH /{id}`, and `DELETE /{id}`.

A request that fails its schema never reaches the handler — the validator rejects it with a 400.

### Errors become status codes

Every handler has the signature `func(w http.ResponseWriter, r *http.Request) error` — it returns an `error` instead of writing a status code itself. `h.Wrap`, from `middlewares.ErrorHandler{}`, bridges that to a standard `http.HandlerFunc`: when a handler returns a typed error from magic's `errors` package, `Wrap` maps it to the matching HTTP status. `&errors.BadRequest{}` becomes a 400, `&errors.NotFound{}` becomes a 404. The handler just returns the error; the middleware owns the translation.

The handlers themselves make up the second half of the file:

```go title="pkg/routes/todo.go (continued)"
// @openapi
// paths:
//
//	/todos:
//	  get:
//	    tags:
//	      - todos
//	    summary: Get all Todos
//	    description: Returns all Todos
//	    operationId: listTodos
//	    parameters:
//	      - name: limit
//	        in: query
//	        description: The number of todo items to return (defaults to 10)
//	        required: false
//	        schema:
//	          type: number
//	      - name: next
//	        in: query
//	        description: The next page identifier
//	        required: false
//	        schema:
//	          type: string
//	      - name: filter
//	        in: query
//	        description: A Lucene query string to filter todos (e.g. done:1)
//	        required: false
//	        schema:
//	          type: string
//	    responses:
//	      '200':
//	        description: successful operation
//	        content:
//	          application/json:
//	            schema:
//	              $ref: '#/components/schemas/TodoList'
//	      '500':
//	         $ref: '#/components/responses/ServerError'
func (t *TodoRouter) ListTodos(w http.ResponseWriter, r *http.Request) error {
	cursor := r.URL.Query().Get("next")
	filter := r.URL.Query().Get("filter")

	limit, err := strconv.ParseInt(r.URL.Query().Get("limit"), 10, 64)
	if err != nil || limit <= 0 {
		limit = 10
	}

	var todos []types.Todo
	var next string
	if filter != "" {
		todos, next, err = t.service.SearchTodos(filter, int(limit), cursor)
	} else {
		todos, next, err = t.service.ListTodos(int(limit), cursor)
	}
	if err != nil {
		return &errors.BadRequest{Message: err.Error()}
	}
	render.JSON(w, r, types.TodoList{Todos: todos, Next: next})
	return nil
}

// @openapi
// paths:
//
//	/todos/{id}:
//	  get:
//	    tags:
//	      - todos
//	    summary: Get a single Todo
//	    description: Returns a Todos with the identifier {id} if exists
//	    operationId: getTodo
//	    parameters:
//	      - name: id
//	        in: path
//	        description: The identifier of the Todo
//	        required: true
//	        schema:
//	          type: string
//	    responses:
//	      '200':
//	        description: successful operation
//	        content:
//	          application/json:
//	            schema:
//	              $ref: '#/components/schemas/Todo'
//	      '404':
//	         $ref: '#/components/responses/NotFound'
//	      '500':
//	         $ref: '#/components/responses/ServerError'
func (t *TodoRouter) GetTodo(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	todo, err := t.service.GetTodo(id)
	if err != nil {
		return err
	}
	render.JSON(w, r, todo)
	return nil
}

// @openapi
// paths:
//
//	/todos/{id}:
//	  delete:
//	    tags:
//	      - todos
//	    summary: Delete a single Todo
//	    description: Deletes a Todos with the identifier {id} if exists
//	    operationId: deleteTodo
//	    parameters:
//	      - name: id
//	        in: path
//	        description: The identifier of the Todo
//	        required: true
//	        schema:
//	          type: string
//	    responses:
//	      '204':
//	        description: successful operation
//	      '500':
//	         $ref: '#/components/responses/ServerError'
func (t *TodoRouter) DeleteTodo(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	err := t.service.DeleteTodo(id)
	if err != nil {
		return err
	}
	render.NoContent(w, r)
	return nil
}

// @openapi
// paths:
//
//	/todos:
//	  post:
//	    tags:
//	      - todos
//	    summary: Create a Todo
//	    description: Create a new Todo
//	    operationId: createTodo
//	    requestBody:
//	      description: Create a new Todo
//	      content:
//	        application/json:
//	          schema:
//	            $ref: '#/components/schemas/TodoUpdate'
//	    responses:
//	      '201':
//	        description: successful operation
//	      '400':
//	         $ref: '#/components/responses/BadRequest'
//	      '500':
//	         $ref: '#/components/responses/ServerError'
func (t *TodoRouter) CreateTodo(w http.ResponseWriter, r *http.Request) error {
	var todoToCreate types.TodoUpdate

	decodeErr := json.NewDecoder(r.Body).Decode(&todoToCreate)
	if decodeErr != nil {
		return decodeErr
	}

	todo, err := t.service.CreateTodo(todoToCreate)
	if err != nil {
		return err
	}

	render.Status(r, http.StatusCreated)
	render.JSON(w, r, todo)
	return nil
}

// @openapi
// paths:
//
//	/todos/{id}:
//	  put:
//	    tags:
//	      - todos
//	    summary: Replace a Todo
//	    description: Replace a Todo
//	    operationId: replaceTodo
//	    parameters:
//	      - name: id
//	        in: path
//	        description: The identifier of the Todo
//	        required: true
//	        schema:
//	          type: string
//	    requestBody:
//	      description: Updated Todo
//	      content:
//	        application/json:
//	          schema:
//	            $ref: '#/components/schemas/TodoUpdate'
//	    responses:
//	      '204':
//	        description: successful operation
//	      '400':
//	         $ref: '#/components/responses/NotFound'
//	      '404':
//	         $ref: '#/components/responses/BadRequest'
//	      '500':
//	         $ref: '#/components/responses/ServerError'
func (t *TodoRouter) ReplaceTodo(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")
	var todoToUpdate types.TodoUpdate

	decoder := json.NewDecoder(r.Body)
	err := decoder.Decode(&todoToUpdate)
	if err != nil {
		return err
	}

	currentRecord, err := t.service.GetTodo(id)
	if err != nil {
		return &errors.NotFound{Message: "Todo not found"}
	}

	todo := types.Todo{Id: currentRecord.Id, Summary: todoToUpdate.Summary, Done: todoToUpdate.Done}
	err = t.service.UpdateTodo(todo)
	if err != nil {
		return err
	}

	render.NoContent(w, r)
	return nil
}

// @openapi
// paths:
//
//	/todos/{id}:
//	  patch:
//	    tags:
//	      - todos
//	    summary: Update a Todo
//	    description: Update a Todo using [JSON Patch](https://jsonpatch.com/)
//	    operationId: updateTodo
//	    parameters:
//	      - name: id
//	        in: path
//	        description: The identifier of the Todo
//	        required: true
//	        schema:
//	          type: string
//	    requestBody:
//	      description: JSON Patch operations to perform in order to update the Todo item
//	      content:
//	        application/json-patch+json:
//	          schema:
//	            type: array
//	            items:
//	              $ref: "#/components/schemas/PatchBody"
//	            example:
//	              - {"op": "replace", "path": "/summary", "value": "An updated TODO item summary"}
//	              - {"op": "replace", "path": "/done", "value": true}
//	    responses:
//	      '204':
//	        description: successful operation
//	      '400':
//	         $ref: '#/components/responses/NotFound'
//	      '404':
//	         $ref: '#/components/responses/BadRequest'
//	      '500':
//	         $ref: '#/components/responses/ServerError'
func (t *TodoRouter) UpdateTodo(w http.ResponseWriter, r *http.Request) error {
	id := chi.URLParam(r, "id")

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return err
	}

	patch, err := jsonpatch.DecodePatch(body)
	if err != nil {
		return &errors.BadRequest{Message: err.Error()}
	}

	currentRecord, err := t.service.GetTodo(id)
	if err != nil {
		return &errors.NotFound{Message: "Todo not found"}
	}

	currentBytes, err := json.Marshal(currentRecord)
	if err != nil {
		return err
	}

	modifiedBytes, err := patch.Apply(currentBytes)
	if err != nil {
		return &errors.BadRequest{Message: err.Error()}
	}

	var modified types.Todo
	err = json.Unmarshal(modifiedBytes, &modified)
	if err != nil {
		return err
	}

	if modified.Id != currentRecord.Id {
		return &errors.BadRequest{Message: "Id field can't be changed"}
	}

	err = t.service.UpdateTodo(modified)
	if err != nil {
		return err
	}

	render.NoContent(w, r)
	return nil
}
```

### The `GET /todos` query surface

`ListTodos` is where the two list paths from the Features section surface as one endpoint. It reads three query params:

- **`?filter=<lucene>`** — if present, the handler calls `t.service.SearchTodos`, the Lucene search path. If absent, it falls through to `t.service.ListTodos`, the structured list. One endpoint, one decision: `if filter != ""`.
- **`?limit=`** — the page size, defaulting to 10 when missing or invalid.
- **`?next=`** — the opaque cursor for the next page.

Together `limit` and `next` drive cursor pagination, and `?filter=` selects which underlying path produces the page. There are no typed query params — a caller filtering on `done` writes `?filter=done:1`, not `?done=true`. See [Search (Lucene)](lucene.md) for the full filter syntax.

### JSON Patch and the OpenAPI annotations

`UpdateTodo` is the one handler that doesn't take a plain JSON body. It expects a `application/json-patch+json` document — a list of JSON Patch operations — reads the current record, applies the patch, and persists the result. It rejects any patch that tries to change `id`.

Every handler is preceded by an `@openapi` annotation block describing its path, parameters, request body, and responses. Just like the type annotations from the Types section, these feed `build/generate.go` — the route blocks reference the `Todo`, `TodoUpdate`, and `TodoList` schemas by name, and those references resolve because the Types section defined them. The handlers and the type structs together produce the complete `config/openapi.json`.

With the router assembled, the last layer is the **server** — the bootstrap that builds storage, observability, health probes, and auth, populates the `AuthConfig`/`PubSubConfig` structs, and mounts this router.

## Server

The server is the final layer — the wiring that assembles everything below it into a running process. Migrations, types, features, and routes each do one job; the server is what builds the storage adapter, runs the migrations against it, initialises observability, populates the `AuthConfig`/`PubSubConfig` structs the Routes section declared, mounts the router, and serves HTTP. todo-service splits this across three small files plus one larger one: `main.go`, `cmd/root.go`, and `cmd/server.go`.

### `main.go` — embed the config, hand off to the CLI

```go title="main.go"
package main

import (
	"embed"

	"todo-service/cmd"

	"github.com/tink3rlabs/magic/storage"
)

//go:generate go run build/generate.go
//go:embed config
var configFS embed.FS

func main() {
	storage.ConfigFs = configFS
	cmd.ConfigFS = configFS
	cmd.Execute()
}
```

`main.go` does almost nothing itself. The `//go:embed config` directive bakes the entire `config/` tree — `development.yaml`, `openapi.json`, and crucially the `migrations/` directory — into the binary as an `embed.FS`. That filesystem is then handed to two places: `storage.ConfigFs`, which is where magic's storage package looks for the migration files at startup (this is what makes the Migrations section's SQL available at runtime, with no files to ship alongside the binary), and `cmd.ConfigFS`, which the `server` command reads `openapi.json` from to serve `/api-docs`. Then `cmd.Execute()` hands control to cobra.

### `cmd/root.go` — the cobra root and viper config

```go title="cmd/root.go"
package cmd

import (
	"embed"
	"fmt"
	"os"
	"strings"

	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"

	"github.com/tink3rlabs/magic/logger"
)

var ConfigFS embed.FS
var cfgFile string
var rootCmd = &cobra.Command{
	Use:   "",
	Short: "ToDo is a reference implementaion of a common service architecture",
	Long: `ToDo is a reference implementaion of a common service architecture brought to you with love by tink3rlabs.
Complete documentation is available at https://github.com/tink3rlabs/todo-service`,
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.todo.yaml)")
	if viperBindFlagsErr := viper.BindPFlags(rootCmd.Flags()); viperBindFlagsErr != nil {
		fmt.Println(viperBindFlagsErr)
		os.Exit(1)
	}
	rootCmd.AddCommand(serverCommand)
}

func initConfig() {
	// Don't forget to read config either from cfgFile or from home directory!
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}

		// Search config in home directory with name ".cobra" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".todo")
	}

	viper.SetEnvPrefix("TODO")
	viper.SetEnvKeyReplacer(strings.NewReplacer("_", "."))
	viper.AutomaticEnv()
	if err := viper.ReadInConfig(); err != nil {
		fmt.Println("Can't read config:", err)
		os.Exit(1)
	}

	config := loggerConfig()
	logger.Init(config)
}

func loggerConfig() *logger.Config {
	// Fetch the log level and format from the config file
	levelStr := viper.GetString("logger.level")
	json := viper.GetBool("logger.json")

	return &logger.Config{
		Level: logger.MapLogLevel(levelStr),
		JSON:  json,
	}
}
```

`root.go` defines the cobra root command and registers the `server` subcommand. The work happens in `initConfig`, run by `cobra.OnInitialize` before any command executes: it points viper at the config file (the `--config` flag, or `~/.todo.yaml` by default), enables `TODO_`-prefixed environment overrides, and reads the file. Every `viper.GetString(...)` call you'll see in `server.go` resolves against the config loaded here. Finally it calls `logger.Init` with the level and format from the config, so magic's structured logger is ready before the server starts.

### `cmd/server.go` — the `server` command

`server.go` is the centrepiece. Its `runServer` function does the full bootstrap in wiring order. Here it is whole:

```go title="cmd/server.go"
package cmd

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/go-chi/render"
	"github.com/go-co-op/gocron/v2"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/tink3rlabs/magic/errors"
	"github.com/tink3rlabs/magic/health"
	"github.com/tink3rlabs/magic/leadership"
	"github.com/tink3rlabs/magic/logger"
	"github.com/tink3rlabs/magic/middlewares"
	"github.com/tink3rlabs/magic/observability"
	"github.com/tink3rlabs/magic/pubsub"
	"github.com/tink3rlabs/magic/storage"
	"github.com/tink3rlabs/magic/telemetry"

	"todo-service/pkg/routes"
)

var serverCommand = &cobra.Command{
	Use:   "server",
	Short: "Run the ToDo server",
	RunE:  runServer,
}

func init() {
	serverCommand.Flags().StringP("port", "p", "8080", "The port on which the Todo server will listen on")
}

func initRoutes(obs *observability.Observer, todosCreated telemetry.Counter, auth routes.AuthConfig, pubSub routes.PubSubConfig) *chi.Mux {
	router := chi.NewRouter()
	router.Use(
		render.SetContentType(render.ContentTypeJSON), // Set content-Type headers as application/json
		middleware.Logger,          // Log API request calls
		middleware.RedirectSlashes, // Redirect slashes to no slash URL versions
		middleware.Recoverer,       // Recover from panics without crashing server
		middlewares.ObservabilityWithOptions(obs, middlewares.ObservabilityOptions{
			SkipPaths:        []string{"/metrics"},
			SkipPathPrefixes: []string{"/health/"},
		}),
		cors.Handler(cors.Options{
			AllowedOrigins:   []string{"https://*", "http://*"},
			AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
			AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-CSRF-Token"},
			ExposedHeaders:   []string{"Link"},
			AllowCredentials: false,
			MaxAge:           300, // Maximum value not ignored by any of major browsers
		}),
	)

	t := routes.NewTodoRouter(todosCreated, auth, pubSub)
	router.Route("/", func(r chi.Router) {
		r.Mount("/todos", t.Router)
	})

	return router
}

func createScheduler() {
	slog.Info("strating scheduler")
	// create a scheduler
	s, err := gocron.NewScheduler()
	if err != nil {
		logger.Fatal("failed to create scheduler", slog.Any("error", err))
	}
	// add a job to the scheduler
	_, err = s.NewJob(
		gocron.DurationJob(30*time.Second),
		gocron.NewTask(
			func(param string) {
				slog.Info("scheduled job says", slog.String("param", param))
			},
			"hello",
		),
	)
	if err != nil {
		logger.Fatal("failed to create scheduled job", slog.Any("error", err))
	}

	// start the scheduler
	s.Start()
}

func runServer(cmd *cobra.Command, args []string) error {
	openApiSpec, err := ConfigFS.ReadFile("config/openapi.json")

	if err != nil {
		return fmt.Errorf("failed to load OpenAPI definition, did you forget to run go generate?: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	obsCfg := observability.DefaultConfig()
	obsCfg.ServiceName = viper.GetString("observability.service_name")
	if obsCfg.ServiceName == "" {
		obsCfg.ServiceName = "todo-service"
	}
	switch viper.GetString("observability.metrics_mode") {
	case "otlp":
		obsCfg.MetricsMode = observability.MetricsModeOTLP
		obsCfg.MetricsOTLPEndpoint = viper.GetString("observability.metrics_otlp_endpoint")
	default:
		obsCfg.MetricsMode = observability.MetricsModePrometheus
	}
	obsCfg.EnableTracing = viper.GetBool("observability.enable_tracing")
	obsCfg.TracesOTLPEndpoint = viper.GetString("observability.traces_otlp_endpoint")

	obs, err := observability.Init(ctx, obsCfg)
	if err != nil {
		logger.Fatal("failed to initialise observability", slog.String("error", err.Error()))
	}
	defer func() { _ = obs.Shutdown(context.Background()) }()

	todosCreated, err := obs.Counter(telemetry.MetricDefinition{
		Name: "todo_service_todos_created_total",
		Help: "Total number of todo items created.",
		Kind: telemetry.KindCounter,
	})
	if err != nil {
		logger.Fatal("failed to register todos_created counter", slog.String("error", err.Error()))
	}

	storageAdapter, err := storage.StorageAdapterFactory{}.GetInstance(
		storage.StorageAdapterType(viper.GetString("storage.type")),
		viper.GetStringMapString("storage.config"),
	)

	if err != nil {
		panic("failed to get storage adapter instance")
	}

	storage.NewDatabaseMigration(storageAdapter).Migrate()

	var publisher pubsub.Publisher
	if viper.GetBool("pubsub.enabled") {
		publisher, err = pubsub.PublisherFactory{}.GetInstance(pubsub.SNS, map[string]string{
			"region": viper.GetString("pubsub.region"),
		})
		if err != nil {
			logger.Fatal("failed to create pub/sub publisher", slog.String("error", err.Error()))
		}
	}

	electionProps := leadership.LeaderElectionProps{
		HeartbeatInterval: viper.GetDuration("leadership.heartbeat"),
		StorageAdapter:    storageAdapter,
		AdditionalProps: map[string]any{
			"global": viper.GetBool("storage.config.global"),
			"region": viper.GetString("storage.config.region"),
			"regios": viper.GetStringSlice("storage.config.regions"),
		},
	}
	election := leadership.NewLeaderElection(electionProps)
	election.Start()

	go func() {
		for result := range election.Results {
			if result == leadership.RESULT_ELECTED {
				createScheduler()
			}
		}
	}()

	authEnabled := viper.GetBool("auth.enabled")
	authMiddleware := middlewares.EnsureValidToken(middlewares.EnsureValidTokenConfig{
		Enabled:   authEnabled,
		IssuerURL: viper.GetString("auth.issuer_url"),
		Audience:  viper.GetStringSlice("auth.audience"),
	})
	authCfg := routes.AuthConfig{
		Middleware: authMiddleware,
		Enabled:    authEnabled,
		WriteRole:  viper.GetString("auth.write_role"),
	}

	pubSubCfg := routes.PubSubConfig{
		Publisher: publisher,
		TopicARN:  viper.GetString("pubsub.topic_arn"),
	}

	router := initRoutes(obs, todosCreated, authCfg, pubSubCfg)

	router.Handle("/metrics", obs.MetricsHandler())

	router.Get("/api-docs", func(w http.ResponseWriter, r *http.Request) {
		if _, responseFailed := w.Write(openApiSpec); responseFailed != nil {
			slog.Error("failed responding to /api-docs:", slog.Any("error", responseFailed))
		}
	})

	//health check - liveness
	router.Get("/health/liveness", func(w http.ResponseWriter, r *http.Request) {
		render.Status(r, http.StatusNoContent)
		render.NoContent(w, r)
	})

	//health check - readiness
	healthChecker := health.NewHealthChecker(storageAdapter)
	h := middlewares.ErrorHandler{}
	router.Get("/health/readiness", h.Wrap(func(w http.ResponseWriter, r *http.Request) error {
		err := healthChecker.Check(viper.GetBool("health.storage"), viper.GetStringSlice("health.dependencies"))
		if err != nil {
			slog.Error("health check readiness failed", slog.Any("error", err.Error()))
			return &errors.ServiceUnavailable{Message: err.Error()}
		} else {
			render.Status(r, http.StatusNoContent)
			render.NoContent(w, r)
			return nil
		}
	}))

	port := viper.GetString("service.port")
	listenAddress := fmt.Sprintf(":%s", port)

	srv := &http.Server{Addr: listenAddress, Handler: router}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("server failed", slog.Any("error", err))
		}
	}()
	slog.Info("todo-service listening", slog.String("address", listenAddress))

	<-ctx.Done()
	slog.Info("shutdown signal received, stopping server")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("graceful shutdown failed", slog.Any("error", err))
	}
	return nil
}
```

### The `runServer` wiring walk

`runServer` builds the service from the bottom up. In order:

1. **Load the OpenAPI spec.** `ConfigFS.ReadFile("config/openapi.json")` reads the generated spec out of the embedded filesystem — later served at `/api-docs`. If it's missing, the command fails fast with a reminder to run `go generate`.
2. **Signal-aware context.** `signal.NotifyContext(..., os.Interrupt, syscall.SIGTERM)` produces a `ctx` that's cancelled on `Ctrl-C` or a `SIGTERM` from the orchestrator. Everything downstream hangs off this context, and the function blocks on it at the end for graceful shutdown.
3. **Observability.** `observability.Init(ctx, obsCfg)` brings up metrics (Prometheus or OTLP, per config) and optional tracing. Its shutdown is deferred immediately, so traces and metrics flush cleanly on exit. See [Observability](observability.md) for the configuration surface.
4. **The custom counter.** `obs.Counter(...)` registers `todo_service_todos_created_total` — the metric the Features section's `WithCreatedCounter` seam expects.
5. **Storage adapter.** `storage.StorageAdapterFactory{}.GetInstance(...)` builds the adapter from `storage.type` and `storage.config` — the same factory call the feature layer makes. See [Storage Adapters](storage.md).
6. **Migrations.** `storage.NewDatabaseMigration(storageAdapter).Migrate()` runs every pending migration against that adapter — the Migrations section's SQL, applied here, before the first request.
7. **Optional pub/sub publisher.** When `pubsub.enabled` is true, an SNS publisher is built; otherwise `publisher` stays nil. Either way it goes into `PubSubConfig` for the routes.
8. **Leadership election and scheduler.** `leadership.NewLeaderElection(...).Start()` runs leader election against the storage adapter; a goroutine watches `election.Results` and starts the background `createScheduler()` only on the replica that's elected leader.
9. **Auth middleware and `AuthConfig`.** `middlewares.EnsureValidToken(...)` is built from the `auth.*` config and wrapped into a `routes.AuthConfig` along with `Enabled` and `WriteRole`. This is the config-gated middleware the Routes section's protected-writes group consumes.
10. **`PubSubConfig`.** The publisher from step 7 and `pubsub.topic_arn` are packed into a `routes.PubSubConfig`.
11. **Build the router.** `initRoutes(obs, todosCreated, authCfg, pubSubCfg)` constructs the chi router — base middleware (logging, panic recovery, CORS, observability), then `NewTodoRouter` mounted at `/todos`.
12. **Mount the operational endpoints.** `/metrics` serves the Prometheus handler, `/api-docs` writes the embedded OpenAPI spec, and the two health probes — `/health/liveness` (always 204) and `/health/readiness` (runs `health.NewHealthChecker` against storage and configured dependencies) — are registered directly on the router.
13. **Serve and block.** The `http.Server` runs in a goroutine; `runServer` blocks on `<-ctx.Done()`. When the signal arrives it calls `srv.Shutdown` with a 10-second timeout for an in-flight-safe graceful stop.

### Health probes and observability skips

The health probes are registered **outside** the route group that carries the auth middleware — they're unauthenticated by design, so an orchestrator can probe `/health/liveness` and `/health/readiness` without a token. The observability middleware is configured to skip them too: `ObservabilityOptions` sets `SkipPathPrefixes: []string{"/health/"}` and `SkipPaths: []string{"/metrics"}`, keeping probe traffic and the metrics scrape itself out of the request metrics and traces.

### Where the seams get populated

This is where the loose ends from the earlier layers get tied off. The `AuthConfig` and `PubSubConfig` structs the Routes section declared but left empty are filled in here from configuration. The `WithCreatedCounter` and `WithPublisher` seams the Features section exposed get their real collaborators — the `todos_created` counter and, when enabled, the SNS publisher — passed down through `initRoutes` and `NewTodoRouter`. The layers are independent; the server is what composes them.

With every layer assembled, the only thing left is to start the service and watch it answer.
