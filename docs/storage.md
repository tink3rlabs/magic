# Storage Adapters

Every adapter implements the same `storage.StorageAdapter` interface, so swapping one for another is a one-line change to the factory call. You get CRUD, pagination, Lucene-backed search, count, and raw query escape hatches — plus schema and migration helpers.

## Picking an adapter

| Adapter type           | Constant            | Backing store                                     | Use it when                                                                 |
|------------------------|---------------------|---------------------------------------------------|-----------------------------------------------------------------------------|
| In-memory              | `storage.MEMORY`    | An in-process SQLite database (no file on disk)   | Tests, demos, prototypes. **All data is lost on process exit.**             |
| SQL                    | `storage.SQL`       | Postgres / MySQL / SQLite (via GORM)              | Anything production-ish that wants relational queries and JSON columns.     |
| DynamoDB               | `storage.DYNAMODB`  | Amazon DynamoDB                                   | AWS-native services that prefer single-table design.                        |
| CosmosDB               | `storage.COSMOSDB`  | Azure CosmosDB                                    | Azure-native services.                                                      |

Cassandra was supported historically but is currently disabled in the factory (see `storage/storage.go`); the source file is preserved as `storage/cassandra.go.backup`.

## Building an adapter

```go title="main.go"
import "github.com/tink3rlabs/magic/storage"

adapter, err := storage.StorageAdapterFactory{}.GetInstance(
    storage.SQL,
    map[string]string{
        "provider": "postgresql",
        "host":     "localhost",
        "port":     "5432",
        "user":     "blox",
        "password": "secret",
        "dbname":   "blox",
        "schema":   "public",
    },
)
if err != nil {
    log.Fatal(err)
}
```

The returned adapter is always wrapped with a telemetry-instrumented adapter. If you need the concrete `*SQLAdapter` — for example, to register a custom GORM plugin — call `storage.UnwrapAdapter(adapter)`. See [`UnwrapAdapter`](https://pkg.go.dev/github.com/tink3rlabs/magic/storage#UnwrapAdapter) on pkg.go.dev.

!!! tip "Same code, different backend"
    The whole point: swap `storage.MEMORY` for `storage.SQL` (and pass a config map) and your handler code keeps working. Lucene filters, cursor pagination, typed errors — all unchanged.

## Configuration by adapter

### In-memory

```go
adapter, _ := storage.StorageAdapterFactory{}.GetInstance(storage.MEMORY, nil)
```

No config keys. Use only for tests and prototypes. Internally it's an in-memory SQLite database — so SQLite quirks apply, e.g. boolean Lucene filters must be written `done:1`, not `done:true` (see [Search](lucene.md)).

### SQL (Postgres / MySQL / SQLite)

```go
// Postgres
config := map[string]string{
    "provider": "postgresql",
    "host":     "localhost",
    "port":     "5432",
    "user":     "blox",
    "password": "secret",
    "dbname":   "blox",
    "schema":   "public",
}

// MySQL
config := map[string]string{
    "provider": "mysql",
    "host":     "localhost",
    "port":     "3306",
    "user":     "blox",
    "password": "secret",
    "dbname":   "blox",
}

// SQLite
config := map[string]string{
    "provider": "sqlite",
    "path":     "/path/to/database.db",
}

adapter, _ := storage.StorageAdapterFactory{}.GetInstance(storage.SQL, config)
```

The SQL adapter uses [GORM](https://gorm.io) internally. Connection pooling, migrations, and schema creation are handled for you via `CreateSchema()`, `CreateMigrationTable()`, etc. The `schema` key is Postgres-specific and becomes a `TablePrefix` on the GORM config.

### DynamoDB

```go
config := map[string]string{
    "provider":   "dynamodb",
    "region":     "eu-west-1",
    "endpoint":   "http://localhost:8000",   // optional, for DynamoDB Local or LocalStack
    "access_key": "...",                     // optional — falls back to the default AWS credential chain
    "secret_key": "...",                     // optional — same as above
}
adapter, _ := storage.StorageAdapterFactory{}.GetInstance(storage.DYNAMODB, config)
```

When `access_key` / `secret_key` are empty, the adapter uses the standard AWS credential provider chain (env vars, IRSA, instance role).

### CosmosDB

```go
// Endpoint + key
config := map[string]string{
    "provider": "cosmosdb",
    "endpoint": "https://your-account.documents.azure.com:443/",
    "key":      "...",
    "database": "blox",
}

// Or use a single connection string
config := map[string]string{
    "provider":          "cosmosdb",
    "connection_string": "AccountEndpoint=https://...;AccountKey=...;",
    "database":          "blox",
}

// Local emulator — disable TLS verification
config := map[string]string{
    "provider":        "cosmosdb",
    "endpoint":        "https://localhost:8081/",
    "key":             "...",
    "database":        "blox",
    "skip_tls_verify": "true",   // local testing only
}

adapter, _ := storage.StorageAdapterFactory{}.GetInstance(storage.COSMOSDB, config)
```

!!! warning "CosmosDB partition key is per-call, not global"
    Azure CosmosDB requires you to pass the partition key on every operation, not just at adapter construction. The adapter exposes this via the variadic `params ...map[string]any` argument. If you forget, queries either fail or cross-partition-fan-out (slow and expensive). See the table and example below.

CosmosDB takes per-call params for the partition key. Pass them in the variadic `params ...map[string]any` argument on `Create` / `Get` / `Update` / `Delete` / `List` / `Search`:

| Param key      | Meaning                                                                |
|----------------|------------------------------------------------------------------------|
| `pk_field`     | Field name to use as the partition key in your document (default `"pk"`). |
| `pk_value`     | Value for that partition key.                                          |
| `sort_direction` | `"ASC"` (default) or `"DESC"` for `List` / `Search`. Same key as `storage.SortDirectionKey`. |

```go title="storage.go"
params := map[string]any{
    "pk_field": "tenant",
    "pk_value": "acme-corp",
}
err := adapter.Create(user, params)
err  = adapter.Get(&user, map[string]any{"id": "user-123"}, params)
```

## Common patterns

### Cursor pagination

`List` and `Search` return a cursor string. Pass `""` on the first call; pass whatever the previous call returned for each subsequent page. An empty cursor on the response means there are no more pages.

```go title="paginate.go"
var page []Task
cursor := ""
for {
    var err error
    cursor, err = adapter.List(&page, "created_at", nil, 100, cursor)
    if err != nil {
        return err
    }
    process(page)
    if cursor == "" {
        break
    }
}
```

### Sort direction

```go title="sort.go"
import "github.com/tink3rlabs/magic/storage"

_, err = adapter.List(
    &page,
    "created_at",
    nil,
    100,
    "",
    map[string]any{storage.SortDirectionKey: "DESC"},
)
```

The value is case-insensitive (`"asc"` and `"ASC"` are equivalent). Anything other than asc/desc returns an error.

### Filter vs search

- **`List(dest, sortKey, filter, limit, cursor, params...)`** — `filter` is a `map[string]any` of exact equalities. Multiple keys are ANDed.
- **`Search(dest, sortKey, query, limit, cursor, params...)`** — `query` is a Lucene query string. See [Search (Lucene)](./lucene.md).

For an HTTP `GET /tasks?filter=...` endpoint, pass the raw `filter` query string straight to `Search`. magic handles validation, error reporting, and parameterization.

### Count

```go
n, err := adapter.Count(&Task{}, map[string]any{"status": "in_progress"})
```

### Not-found

```go
err := adapter.Get(&task, map[string]any{"id": "missing"})
if errors.Is(err, storage.ErrNotFound) {
    // 404
}
```

## Migrations

Each adapter implements `CreateMigrationTable`, `GetLatestMigration`, and `UpdateMigrationTable`. magic ships an `embed.FS` (`storage.ConfigFs`) that adapters use to load SQL migration files; populate it from your own `embed`'d migrations directory at startup.

A typical bootstrap:

```go
if err := adapter.CreateSchema(); err != nil {
    log.Fatal(err)
}
if err := adapter.CreateMigrationTable(); err != nil {
    log.Fatal(err)
}
latest, err := adapter.GetLatestMigration()
// run any newer migrations in order...
```

The SQL adapter wraps each migration in a transaction. DynamoDB and CosmosDB do not have schema migrations in the relational sense; the helpers are no-ops on those adapters.

## Escape hatches

When you need a raw query that doesn't fit the interface, use:

- `adapter.Execute(statement)` — fire-and-forget DDL/DML.
- `adapter.Query(dest, statement, limit, cursor, params...)` — **not implemented on the SQL adapter** (returns a "not implemented yet" error today).

These bypass the Lucene layer entirely. **You are responsible for parameter binding.** Prefer `List` / `Search` whenever possible.

## Adapter-specific limitations

- **Memory** — an in-memory SQLite database: data lost on restart, single process only, and SQLite's limitations (below) apply.
- **SQLite** — no `pg_trgm`, so fuzzy Lucene search is unsupported (returns an explanatory error). Use wildcards instead.
- **MySQL** — fuzzy search uses `SOUNDEX`, which ignores the distance hint (`~2`) and works only on ASCII pronunciations.
- **DynamoDB** — the Lucene compiler targets PartiQL; fuzzy search and JSON path access are intentionally not implemented. Equality, range, wildcards (rendered as `begins_with`/`contains`), and boolean composition work.
- **CosmosDB** — see [`storage/cosmosdb.go`](https://github.com/tink3rlabs/magic/blob/main/storage/cosmosdb.go) for the supported subset and partition-key handling.
