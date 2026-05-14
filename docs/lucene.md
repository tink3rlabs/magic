# Search (Lucene)

You expose a single `filter=` query parameter on every list endpoint. The value is a [Lucene query string](https://lucene.apache.org/core/2_9_4/queryparsersyntax.html) that magic compiles to safe, parameterized SQL or DynamoDB PartiQL. No string-concatenated values anywhere — wildcards and JSON path keys are validated and parameterized.

## The two-line setup

```go title="parser.go"
parser, _ := lucene.NewParser(Task{})
sql, params, err := parser.ParseToSQL("status:received AND amount:[100 TO 500]", "postgresql")
// sql:    "(\"status\" = $1) AND (\"amount\" BETWEEN $2 AND $3)"
// params: []any{"received", 100, 500}
```

The same parser drives the storage adapter's `Search` method, so most code never calls `ParseToSQL` directly — you just pass the user's filter string through.

`NewParser` introspects the model struct once. Only fields with a `json` tag are searchable; `json:"-"` and untagged fields are excluded. The field's Go type controls how you query it:

| Go type                            | `ImplicitSearch`? | Notes                                                          |
|------------------------------------|-------------------|----------------------------------------------------------------|
| `string`                           | yes               | Matched by bare terms like `foo` (across all string fields).   |
| `int`, `float64`, `time.Time`, `uuid.UUID` | no        | Must be referenced explicitly: `created_at:[X TO Y]`.          |
| Map / slice / struct (JSONB)       | no                | Reachable via `field.subfield` syntax (see [JSON paths](#json-sub-fields)). |

Field names in the query are the JSON tag, not the Go field name.

## Operators

All operators below have been verified against `storage/search/lucene/sql_driver.go`. Behavior that differs between providers is called out explicitly.

### Equality

```text
status:received
```

Compiles to `"status" = $1`. Values may be unquoted (`foo`), quoted (`"foo bar"`), numeric (`42`), or boolean (`true`).

### Boolean composition

```text
status:received AND counterparty_id:abc123
status:received OR status:pending
status:received AND NOT status:cancelled
```

Operators are case-sensitive (`AND` / `OR` / `NOT`). Parentheses group: `(a OR b) AND c`. Within a single field, group with `field:(a OR b)` — magic re-renders the inner leaves with the outer field name correctly, so `tenant_id:(abc OR null)` becomes `("tenant_id" = $1) OR ("tenant_id" IS NULL)`, not the broken form some Lucene libraries produce.

### Range

```text
amount:[100 TO 500]      # inclusive
amount:{100 TO 500}      # exclusive
created_at:[2025-01-01 TO 2025-12-31]
```

Inclusive ranges compile to `BETWEEN $1 AND $2`; exclusive ranges compile to `> $1 AND < $2`.

### Wildcards

```text
name:foo*       # starts with foo
name:foo?bar    # exactly one char between foo and bar
```

`*` becomes SQL `%`; `?` becomes `_`.

Per-provider behavior:

| Provider   | Wildcard rendering                                  |
|------------|-----------------------------------------------------|
| Postgres   | `"col"::text ILIKE $1` (case-insensitive)           |
| MySQL      | `LOWER("col") LIKE LOWER($1)` (case-insensitive)    |
| SQLite     | `"col" LIKE $1` (case-insensitive for ASCII)        |

JSON sub-field columns skip the `::text` cast because the JSON operator already returns text.

### Fuzzy

```text
name:foo~2
```

!!! warning "Fuzzy is not consistent across providers"
    Postgres requires the `pg_trgm` extension. MySQL falls back to SOUNDEX and ignores the distance hint. SQLite returns an error — use wildcards instead. Read the table below before promising fuzzy search to users.

| Provider   | Implementation                                                                 |
|------------|--------------------------------------------------------------------------------|
| Postgres   | `similarity("col"::text, $1) > 0.3` — **requires the `pg_trgm` extension**.    |
| MySQL      | `SOUNDEX("col") = SOUNDEX($1)` — phonetic match only, the `~N` distance is ignored. |
| SQLite     | **Returns an error.** Use wildcards instead: `name:foo*`.                      |

### Null and not-null

```text
deleted_at:null      # IS NULL
deleted_at:*         # IS NOT NULL
```

`field:null` compiles to `"field" IS NULL`. The empty-wildcard `field:*` compiles to `"field" IS NOT NULL`.

Comparison operators (`>`, `<`, `>=`, `<=`) with `null` return a parse error — they are meaningless.

### Comparison

```text
amount:>100
amount:>=100
amount:<=500
```

Compile to `"amount" > $1` etc. Combining with `null` is an error (see above).

### JSON sub-fields

If a field's Go type is a struct, map, or slice (a JSONB column in Postgres), use dot notation to reach inside it:

```text
metadata.tier:gold
labels.region:eu-west-1
```

| Provider   | Rendered                                              |
|------------|-------------------------------------------------------|
| Postgres   | `metadata->>'tier' = $1`                              |
| MySQL      | `JSON_UNQUOTE(JSON_EXTRACT(metadata, '$.tier')) = $1` |
| SQLite     | `JSON_EXTRACT(metadata, '$.tier') = $1`               |

Subfield names must match `^[a-zA-Z0-9_.]+$`. Single quotes inside Postgres path keys are escaped. Whitespace and other special characters are rejected up-front to block injection.

### Implicit (unfielded) terms

If your model has any string field with `ImplicitSearch=true`, you can search across all such fields with a bare term:

```text
foundation
```

This is rewritten to `(string_field_1:foundation OR string_field_2:foundation OR ...)` before being parsed. Non-string fields are never included in implicit search — the user must reference them explicitly.

## A full HTTP handler

In practice you almost never call `ParseToSQL` yourself — the storage adapter's `Search` method does it for you. A complete list-with-filter endpoint:

```go title="routes/tasks.go"
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
```

`magicerrors.BadRequest` maps to HTTP 400 via the [`ErrorHandler` middleware](tutorial.md#routes). A user sending `?filter=does_not_exist:foo` gets back the structured `InvalidFieldError` message with a list of valid fields.

## Safety limits

`NewParser` applies three limits to incoming queries. All are configurable via `lucene.ParserConfig`:

| Limit             | Default     | What it catches                                |
|-------------------|-------------|------------------------------------------------|
| `MaxQueryLength`  | 10000 bytes | Memory exhaustion via huge strings.            |
| `MaxDepth`        | 20          | Stack overflow from deeply nested parens.      |
| `MaxTerms`        | 100         | CPU exhaustion from many-term queries.         |

```go title="parser.go"
parser, _ := lucene.NewParser(Task{}, &lucene.ParserConfig{
    MaxQueryLength: 2000,
    MaxDepth:       8,
    MaxTerms:       30,
})
```

Exceeding any limit produces a wrapped error from `parser.ParseToSQL` / `parser.ParseToDynamoDBPartiQL`. Callers should map these to HTTP 400.

## Errors

The parser produces structured errors for the common cases:

- **`*lucene.InvalidFieldError`** — the query references a field that doesn't exist on the model. Has `Field` (the bad name) and `ValidFields` (a slice of all searchable field names). Map this to HTTP 400 and surface the valid list to the user.
- **Length / depth / term errors** — wrapped `errors.Join` of one or more `errors.New(...)`. Map to HTTP 400.
- **Provider errors** — `unsupported SQL provider: xxx` from `ParseToSQL` if you pass anything other than `"postgresql"`, `"mysql"`, `"sqlite"`. Programmer error, not user input.
- **SQLite fuzzy** — `fuzzy search (field:term~N) is not supported with SQLite; use wildcards instead` — return as 400 with the suggestion.

```go title="handler.go"
sql, params, err := parser.ParseToSQL(userInput, "postgresql")
if err != nil {
    var invalid *lucene.InvalidFieldError
    if errors.As(err, &invalid) {
        return badRequest(fmt.Sprintf("unknown field %q; valid fields: %v", invalid.Field, invalid.ValidFields))
    }
    return badRequest(err.Error())
}
```

## DynamoDB

```go title="handler.go"
partiql, attrs, err := parser.ParseToDynamoDBPartiQL("status:received AND amount:>100")
```

The DynamoDB driver is intentionally narrower than the SQL driver — PartiQL does not support fuzzy search, case-insensitive matching, or JSON path access the same way. Wildcards (`LIKE`) and equality are supported. See `storage/search/lucene/dynamodb_driver.go` for the exact mapping.

## Full operator reference

| Operator              | Example                          | Postgres                                   | MySQL                                                 | SQLite                                |
|-----------------------|----------------------------------|--------------------------------------------|-------------------------------------------------------|---------------------------------------|
| Equality              | `status:received`                | `"status" = $1`                            | `"status" = $1`                                       | `"status" = $1`                       |
| Boolean               | `a:1 AND b:2`, `a:1 OR b:2`      | `(...) AND (...)`                          | same                                                  | same                                  |
| Negation              | `NOT status:cancelled`           | `NOT (...)`                                | same                                                  | same                                  |
| Inclusive range       | `amount:[100 TO 500]`            | `BETWEEN $1 AND $2`                        | same                                                  | same                                  |
| Exclusive range       | `amount:{100 TO 500}`            | `> $1 AND < $2`                            | same                                                  | same                                  |
| Comparison            | `amount:>100`                    | `"amount" > $1`                            | same                                                  | same                                  |
| Wildcard              | `name:foo*`                      | `"name"::text ILIKE $1`                    | `LOWER("name") LIKE LOWER($1)`                        | `"name" LIKE $1`                      |
| Fuzzy                 | `name:foo~2`                     | `similarity("name"::text, $1) > 0.3`       | `SOUNDEX("name") = SOUNDEX($1)`                       | **error** — use wildcards             |
| Null                  | `field:null`                     | `"field" IS NULL`                          | same                                                  | same                                  |
| Not null              | `field:*`                        | `"field" IS NOT NULL`                      | same                                                  | same                                  |
| JSON sub-field        | `metadata.tier:gold`             | `metadata->>'tier' = $1`                   | `JSON_UNQUOTE(JSON_EXTRACT(metadata, '$.tier')) = $1` | `JSON_EXTRACT(metadata, '$.tier') = $1` |
| Grouped field         | `tenant_id:(a OR null)`          | `("tenant_id" = $1) OR ("tenant_id" IS NULL)` | same                                               | same                                  |
| Implicit (unfielded)  | `foo`                            | OR across all `ImplicitSearch=true` fields | same                                                  | same                                  |
