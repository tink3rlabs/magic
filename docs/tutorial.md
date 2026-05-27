# Build a microservice with magic

You will build **todo-service** — the canonical reference service for magic — and come away understanding it layer by layer. todo-service is a small but complete microservice: a CRUD API for todo items that exercises magic's features end to end. The source lives at [`tink3rlabs/todo-service`](https://github.com/tink3rlabs/todo-service).

Every snippet below is included live from [`tink3rlabs/todo-service`](https://github.com/tink3rlabs/todo-service) at commit `1483fdcb85a151625a3028ae296aa5dbd44e0a66` — pulled straight from the repo at build time, not copied into this page.

!!! note "Pinned to a commit, for now"
    The code blocks below are not copied — they're included live from `tink3rlabs/todo-service` at the pinned commit `1483fdcb85a151625a3028ae296aa5dbd44e0a66` via mkdocs `pymdownx.snippets` URL includes. `mkdocs build --strict` fetches and validates every include, so the tutorial can no longer silently drift from the real repo. todo-service hasn't cut a tagged release yet; once it does, this pin moves from the SHA to that release tag.

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
--8<-- "https://raw.githubusercontent.com/tink3rlabs/todo-service/1483fdcb85a151625a3028ae296aa5dbd44e0a66/config/migrations/postgresql/01__base.yaml:migration-postgres"
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
--8<-- "https://raw.githubusercontent.com/tink3rlabs/todo-service/1483fdcb85a151625a3028ae296aa5dbd44e0a66/main.go:main-imports"
```

At startup, `runServer` builds the storage adapter and then runs any pending migrations against it:

```go title="cmd/server.go"
--8<-- "https://raw.githubusercontent.com/tink3rlabs/todo-service/1483fdcb85a151625a3028ae296aa5dbd44e0a66/cmd/server.go:server-cmd-a"
```

The Server section later covers the full bootstrap; for now the point is just that migrations run automatically, before the first request is served. This holds even for the default in-memory adapter used in local dev — it runs the same migrations, so the service behaves identically whether it's backed by Postgres or an in-process store. For the adapter details, see [Storage Adapters](storage.md).

!!! note "Migrations run on every start"
    `Migrate()` applies only the migrations that haven't run yet and is safe to call on every boot. A fresh database gets the full schema; an up-to-date one is left untouched.

With the schema defined, the next layer is the **types** — the Go structs that model a todo and carry the OpenAPI annotations magic generates the spec from.

## Types

The second layer is the data shapes the rest of the service is built around. Every layer above this one — features, routes, the generated OpenAPI spec — refers back to these structs. todo-service defines all three in a single file:

```go title="pkg/types/todo.go"
--8<-- "https://raw.githubusercontent.com/tink3rlabs/todo-service/1483fdcb85a151625a3028ae296aa5dbd44e0a66/pkg/types/todo.go:types-todo"
```

### The `json` tags do double duty

The `json` struct tags are not just for serialization. magic's storage layer reads them too — they are the field and column names it uses, not the Go field names. The `Todo.Id` field is `json:"id"`, so storage knows it as `id`. That's the same `id` you saw as the primary key in the Migrations section, and it's the literal string passed as the sort key in the feature layer:

```go
--8<-- "https://raw.githubusercontent.com/tink3rlabs/todo-service/1483fdcb85a151625a3028ae296aa5dbd44e0a66/pkg/features/todo/todoService.go:feature-list-call"
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
--8<-- "https://raw.githubusercontent.com/tink3rlabs/todo-service/1483fdcb85a151625a3028ae296aa5dbd44e0a66/pkg/features/todo/todoService.go:feature-service"
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
--8<-- "https://raw.githubusercontent.com/tink3rlabs/todo-service/1483fdcb85a151625a3028ae296aa5dbd44e0a66/pkg/features/todo/todoService.go:feature-list-call"
```

The third argument is a `map[string]any` of field/value pairs ANDed together as exact matches — here it's empty, so every todo is returned, a page at a time.

`SearchTodos` is the **search path**. It calls `storage.Search` with a single Lucene `filter` string:

```go
--8<-- "https://raw.githubusercontent.com/tink3rlabs/todo-service/1483fdcb85a151625a3028ae296aa5dbd44e0a66/pkg/features/todo/todoService.go:feature-search-call"
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
--8<-- "https://raw.githubusercontent.com/tink3rlabs/todo-service/1483fdcb85a151625a3028ae296aa5dbd44e0a66/pkg/routes/todo.go:routes-a"
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
--8<-- "https://raw.githubusercontent.com/tink3rlabs/todo-service/1483fdcb85a151625a3028ae296aa5dbd44e0a66/pkg/routes/todo.go:routes-b"
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
--8<-- "https://raw.githubusercontent.com/tink3rlabs/todo-service/1483fdcb85a151625a3028ae296aa5dbd44e0a66/main.go:main-run"
```

`main.go` does almost nothing itself. The `//go:embed config` directive bakes the entire `config/` tree — `development.yaml`, the generated `openapi.json`, and crucially the `migrations/` directory — into the binary as an `embed.FS`. That filesystem is then handed to two places: `storage.ConfigFs`, which is where magic's storage package looks for the migration files at startup (this is what makes the Migrations section's SQL available at runtime, with no files to ship alongside the binary), and `cmd.ConfigFS`, which the `server` command reads `openapi.json` from to serve `/api-docs`. Then `cmd.Execute()` hands control to cobra.

### `cmd/root.go` — the cobra root and viper config

```go title="cmd/root.go"
--8<-- "https://raw.githubusercontent.com/tink3rlabs/todo-service/1483fdcb85a151625a3028ae296aa5dbd44e0a66/cmd/root.go:root-cmd"
```

`root.go` defines the cobra root command and registers the `server` subcommand. The work happens in `initConfig`, run by `cobra.OnInitialize` before any command executes: it points viper at the config file (the `--config` flag, or `~/.todo.yaml` by default), enables `TODO_`-prefixed environment overrides, and reads the file. Every `viper.GetString(...)` call you'll see in `server.go` resolves against the config loaded here. Finally it calls `logger.Init` with the level and format from the config, so magic's structured logger is ready before the server starts.

### `cmd/server.go` — the `server` command

`server.go` is the centrepiece. Its `runServer` function does the full bootstrap in wiring order. Here it is whole:

```go title="cmd/server.go"
--8<-- "https://raw.githubusercontent.com/tink3rlabs/todo-service/1483fdcb85a151625a3028ae296aa5dbd44e0a66/cmd/server.go:server-cmd-b"
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

## Run it

One build step first: the server fails fast if `config/openapi.json` is missing, so generate it from the type and route annotations:

```bash
go generate ./...
```

Then start the service. It defaults to the in-memory storage adapter with auth and pub/sub disabled — no database, no tokens, no AWS credentials:

```bash
go run . server --config config/development.yaml
```

In a second shell, exercise it with `curl`.

**Health probes** — both return `204 No Content`:

```bash
curl -i http://localhost:8080/health/liveness
curl -i http://localhost:8080/health/readiness
```

```text
HTTP/1.1 204 No Content
```

**Create two todos** — `POST /todos` returns `201` with the created record (note the server-minted UUIDv7 `id`):

```bash
curl -s -X POST http://localhost:8080/todos \
  -H 'Content-Type: application/json' \
  -d '{"summary":"buy milk"}'
curl -s -X POST http://localhost:8080/todos \
  -H 'Content-Type: application/json' \
  -d '{"summary":"walk dog","done":true}'
```

```json
{"id":"01909c42-cc90-75dc-a943-2d87a16e787d","summary":"buy milk","done":false}
```

!!! note "POST `/todos`, no trailing slash"
    `POST /todos/` 301-redirects to `/todos` (the `RedirectSlashes` middleware), and `curl` drops the request body across that redirect. Target `/todos` directly.

**List all todos** — `GET /todos` returns both, plus an empty `next` cursor (only one page):

```bash
curl -s http://localhost:8080/todos
```

```json
{"todos":[{"id":"01909c42-cc90-75dc-a943-2d87a16e787d","summary":"buy milk","done":false},{"id":"01909c42-d1f0-7a3b-bc77-9e2150f4c8a1","summary":"walk dog","done":true}],"next":""}
```

**Filter with Lucene** — `?filter=done:1` returns only the completed todo:

```bash
curl -s 'http://localhost:8080/todos?filter=done:1'
```

```json
{"todos":[{"id":"01909c42-d1f0-7a3b-bc77-9e2150f4c8a1","summary":"walk dog","done":true}],"next":""}
```

It's `done:1`, not `done:true` — the in-memory adapter is SQLite-backed, and its `done` column stores integers, so the boolean term has to be written as `1`. The Features section's note covers why; [Search (Lucene)](lucene.md) has the full filter syntax.

**Metrics** — `GET /metrics` serves the Prometheus exposition. The custom counter wired up in the Server section shows the two creates:

```bash
curl -s http://localhost:8080/metrics | grep todo_service_todos_created_total
```

```text
todo_service_todos_created_total 2
```

**OpenAPI spec** — `GET /api-docs` returns `200` with the generated OpenAPI JSON — the same `config/openapi.json` that `go generate` produced from the `@openapi` annotations:

```bash
curl -s http://localhost:8080/api-docs
```

That's the whole service: the schema migrated itself at startup, the types shaped every payload, the feature layer ran the CRUD and search, the routes mapped them onto HTTP, and the server wired it all together.

## Where to next

Finishing this tutorial means you've walked todo-service's full stack — migrations, types, features, routes, server — and the running process you just `curl`ed *is* that walk: the real reference service, end to end. You haven't read a toy; you've built and understood the canonical magic service.

The walk kept auth, pub/sub, and the background machinery in the default-off state to stay focused on the request path. todo-service ships all of it, config-gated and ready to turn on:

- **JWT auth** — `middlewares.EnsureValidToken` with multi-provider issuer support, gating the protected-writes group and the `RequireRole` check.
- **SNS pub/sub** — the `WithPublisher` seam, emitting `todo.created` / `todo.updated` events when `pubsub.enabled` is set.
- **Leadership election + scheduler** — leader election over the storage adapter, with the background `gocron` scheduler running only on the elected replica.
- **OpenAPI generation** — `build/generate.go` and `openapi-godoc` turning the `@openapi` annotations into the served spec.

Flip the relevant `config/development.yaml` switches and read the corresponding code in [`tink3rlabs/todo-service`](https://github.com/tink3rlabs/todo-service) to see each one in action.

For the cross-cutting concerns this tutorial pointed at along the way:

- [Search (Lucene)](lucene.md) — the full `?filter=` query syntax and the searchable-field rules.
- [Storage Adapters](storage.md) — the adapter factory and per-adapter configuration.
- [Observability](observability.md) — metrics, tracing, and the observability config surface.
- [Contributing](contributing.md) — how to work on magic itself.
