# Search (Lucene)

You expose a single `filter=` query parameter on every list endpoint. The value is a [Lucene query string](https://lucene.apache.org/core/2_9_4/queryparsersyntax.html) that magic compiles to safe, parameterized SQL or DynamoDB PartiQL. No string-concatenated values anywhere — wildcards and JSON path keys are validated and parameterized.

## The two-line setup

```go title="parser.go"
parser, err := lucene.NewParser(Task{})
if err != nil {
    return err
}
sql, params, err := parser.ParseToSQL("status:received AND amount:[100 TO 500]", "postgresql")
// sql:    "(\"status\" = ?) AND (\"amount\" BETWEEN ? AND ?)"
// params: []any{"received", "100", "500"}
```

`ParseToSQL` returns `?` placeholders for every provider; GORM's Postgres driver rewrites them to `$1, $2, …` when it executes the query.

The same parser drives the storage adapter's `Search` method, so most code never calls `ParseToSQL` directly — you just pass the user's filter string through.

`NewParser` introspects the model struct once. Only fields with a `json` tag are searchable; `json:"-"` and untagged fields are excluded. The field's Go type controls how you query it:

| Go type                            | `ImplicitSearch`? | Notes                                                          |
|------------------------------------|-------------------|----------------------------------------------------------------|
| `string`                           | yes               | Matched by bare terms like `foo` (across all string fields).   |
| `int`, `float64`, `time.Time`, `uuid.UUID` | no        | Must be referenced explicitly: `created_at:[X TO Y]`.          |
| Map / struct (JSONB)               | no                | Reachable via `field.subfield` syntax (see [JSON paths](#json-sub-fields)). |
| Slice / array (`text[]`, JSON array) | no              | Multi-valued; `field:value` means containment (see [Array fields](#array-multi-valued-fields)). |

Field names in the query are the JSON tag, not the Go field name.

## Operators

All operators below have been verified against `storage/search/lucene/sql_driver.go`. Behavior that differs between providers is called out explicitly.

### Equality

```text
status:received
```

Compiles to `"status" = ?`. Values may be unquoted (`foo`), quoted (`"foo bar"`), numeric (`42`), or boolean (`true`).

### Boolean composition

```text
status:received AND counterparty_id:abc123
status:received OR status:pending
status:received AND NOT status:cancelled
```

Operators are case-sensitive (`AND` / `OR` / `NOT`). Parentheses group: `(a OR b) AND c`. Within a single field, group with `field:(a OR b)` — magic re-renders the inner leaves with the outer field name correctly, so `tenant_id:(abc OR null)` becomes `("tenant_id" = ? OR "tenant_id" IS NULL)`, not the broken form some Lucene libraries produce.

### Range

```text
amount:[100 TO 500]      # inclusive
amount:{100 TO 500}      # exclusive
created_at:[2025-01-01 TO 2025-12-31]
```

Inclusive ranges compile to `BETWEEN ? AND ?`; exclusive ranges compile to `> ? AND < ?`.

### Wildcards

```text
name:foo*       # starts with foo
name:foo?bar    # exactly one char between foo and bar
```

`*` becomes SQL `%`; `?` becomes `_`.

Per-provider behavior:

| Provider   | Wildcard rendering                                  |
|------------|-----------------------------------------------------|
| Postgres   | `"col"::text ILIKE ?` (case-insensitive)            |
| MySQL      | `` LOWER(`col`) LIKE LOWER(?) `` (case-insensitive) |
| SQLite     | `"col" LIKE ?` (case-insensitive for ASCII)         |

JSON sub-field columns skip the `::text` cast because the JSON operator already returns text.

### Fuzzy

```text
name:foo~2
```

!!! warning "Fuzzy is not consistent across providers"
    Postgres requires the `pg_trgm` extension. MySQL falls back to SOUNDEX and ignores the distance hint. SQLite returns an error — use wildcards instead. Read the table below before promising fuzzy search to users.

| Provider   | Implementation                                                                 |
|------------|--------------------------------------------------------------------------------|
| Postgres   | `similarity("col"::text, ?) > 0.3` — **requires the `pg_trgm` extension**.     |
| MySQL      | `SOUNDEX("col") = SOUNDEX(?)` — phonetic match only, the `~N` distance is ignored. |
| SQLite     | **Returns an error.** Use wildcards instead: `name:foo*`.                      |

### Null and not-null

```text
deleted_at:null      # IS NULL
deleted_at:*         # any value (matches non-null)
```

`field:null` compiles to `"field" IS NULL`. The empty-wildcard `field:*` is a wildcard match against everything — it compiles to the same form as any other wildcard (`"field"::text ILIKE ?` on Postgres, `LIKE` elsewhere) with `%` as the bound parameter. Since `NULL` never matches `LIKE`/`ILIKE`, this effectively selects rows where the field has a value.

Comparison operators (`>`, `<`, `>=`, `<=`) with `null` return a parse error — they are meaningless.

### Comparison

```text
amount:>100
amount:>=100
amount:<=500
```

Compile to `"amount" > ?` etc. Combining with `null` is an error (see above).

### JSON sub-fields

If a field's Go type is a struct or map (a JSONB column in Postgres), use dot notation to reach inside it:

```text
metadata.tier:gold
labels.region:eu-west-1
```

| Provider   | Rendered                                              |
|------------|-------------------------------------------------------|
| Postgres   | `metadata->>'tier' = ?`                               |
| MySQL      | `JSON_UNQUOTE(JSON_EXTRACT(metadata, '$.tier')) = ?`  |
| SQLite     | `JSON_EXTRACT(metadata, '$.tier') = ?`                |

Subfield names must match `^[a-zA-Z0-9_.]+$`. Single quotes inside Postgres path keys are escaped. Whitespace and other special characters are rejected up-front to block injection.

### Array (multi-valued) fields

If a field's Go type is a slice or array (a Postgres `text[]`, or a JSON array
in MySQL/SQLite), `field:value` means **containment** — the row matches if any
element equals the value. This follows Lucene/Elasticsearch semantics, where a
multi-valued field is queried exactly like a single-valued one. `[]byte` is
excluded — it's treated as a scalar blob (bytea), not a collection.

```text
tags:golang        # rows whose tags contain "golang"
tags:*go*          # rows where any single element matches *go*
tags:(a OR b)      # contains a, or contains b
tags:(a* OR b)     # any element matches a*, or contains b
tags:*             # tags column is not null
NOT tags:golang    # does not contain golang (including rows with no tags)
-tags:golang       # same — both spellings of negation behave identically
NOT tags:null      # tags column is not null (identical to tags:*)
```

| Operator | Postgres | MySQL | SQLite |
|---|---|---|---|
| Containment | `COALESCE("tags" @> ?, false)` | `` COALESCE(JSON_CONTAINS(`tags`, ?), false) `` | `EXISTS (SELECT 1 FROM json_each("tags") WHERE value = ?)` |
| Wildcard | `EXISTS (SELECT 1 FROM unnest("tags") AS elem WHERE elem ILIKE ?)` | `` JSON_SEARCH(LOWER(CAST(`tags` AS CHAR)), 'one', LOWER(?)) IS NOT NULL `` | `EXISTS (SELECT 1 FROM json_each("tags") WHERE value LIKE ?)` |
| Has value | `"tags" IS NOT NULL` | `` `tags` IS NOT NULL `` | `"tags" IS NOT NULL` |

The rows above are the string-element form. Arrays of numbers or booleans
render differently on every provider — see [Non-string
elements](#non-string-elements) below.

`>`, `<`, `>=`, `<=` and range queries (`field:[a TO b]`) return a parse error
on array fields — they have no meaning against a collection. Fuzzy
(`tags:foo~2`) is **not** rejected: it still renders
`similarity("tags"::text, ?) > 0.3` on Postgres (comparing the whole array's
text form), which is unlikely to be useful but does not error.

**Index your array columns.** Containment uses `@>` rather than the more
familiar `= ANY(...)` specifically so Postgres can use a GIN index — measured
on a 200k-row table, `@>` produced a bitmap index scan while `= ANY` fell back
to a sequential scan. GORM does not create this index for you:

```sql
CREATE INDEX idx_articles_tags ON articles USING GIN (tags);
```

Wildcard matching unnests each row and cannot use the index — that is
inherent to substring matching, not a limitation of this layer.

**`field:*` means "column is not null"**, so it matches a row holding an empty
array. This is a deliberate, documented divergence from Elasticsearch's
`exists` query, which treats an empty array as having no value.

**Known limitations.**

- A slice type whose name contains `JSON`/`JSONB` keeps the nested-access
  path from [JSON sub-fields](#json-sub-fields) instead — it is not treated
  as an array.
- `Tags []string` tagged `gorm:"serializer:json"` is stored as JSON but is
  still detected as a native array by its Go type, so Postgres receives
  `@>` against a JSON column and errors. If you want a JSON-backed
  array, use an explicitly JSON-named type instead.
#### Non-string elements

Filter values arrive as strings. An array of numbers or booleans therefore
needs the element type reclaimed at render time, or the comparison comes down
to "string versus number" and silently matches nothing.

```text
nums:5        # rows whose nums contain the number 5
flags:true    # rows whose flags contain the boolean true
nums:abc      # parse error — not a valid element of an int array
nums:*5*      # parse error — see below
```

Each provider renders one form for every element type, and the difference
lives entirely in how the parameter is bound:

| Provider | SQL | Bound parameter |
|---|---|---|
| Postgres | `COALESCE("nums" @> ?, false)` | single-element array literal (a `driver.Valuer`) |
| MySQL | `` COALESCE(JSON_CONTAINS(`nums`, ?), false) `` | JSON scalar text: `5`, `1.5`, `true`, `"golang"` |
| SQLite | `EXISTS (SELECT 1 FROM json_each("nums") WHERE value = ?)` | the native Go value |
| DynamoDB | `contains(nums, ?)` | an `N`, `BOOL` or `S` attribute |

**No provider needs a type cast.** Postgres infers the array type from the
column because the whole array arrives as a single bound parameter. The
earlier `ARRAY[?]` form could not: Postgres resolves an array constructor to
`text[]` at parse time, before it knows the parameter's type, so that form had
to carry an explicit `::type[]` cast naming the column's exact element type —
`@>` requires exactly matching array types, and neither narrowing nor widening
rescues a mismatch.

**The parameter is a `driver.Valuer`, not a Go slice, and that is
deliberate.** GORM expands slice arguments for `IN (?)` clauses, which would
rewrite `col @> ?` into `col @> ($1)` and then fail to encode. GORM checks
`driver.Valuer` before that expansion, so a Valuer survives as one parameter.

`COALESCE(..., false)` makes a NULL column compare false rather than NULL, so
`NOT field:value` is a true complement instead of silently dropping those rows.

**Values are validated against the element type before rendering.** A filter
like `nums:abc` or `flags:maybe` returns a parse error instead of reaching the
database and failing there — the same reason array support exists at all.

**Wildcards are rejected on non-string arrays** (`nums:*5*` is a parse error).
Substring matching has no meaning for a number: Postgres errors on `ILIKE`
against an integer, and MySQL's `JSON_SEARCH` only inspects string scalars, so
it would silently match nothing. Use containment instead.

### Implicit (unfielded) terms

If your model has any string field with `ImplicitSearch=true`, you can search across all such fields with a bare term:

```text
foundation
```

This is rewritten to a contains-match across those fields — `(name:*foundation* OR description:*foundation*)` — before being parsed (each bare term is wrapped in `*…*` unless it is quoted or already contains wildcards). Non-string fields are never included in implicit search — the user must reference them explicitly.

## A full HTTP handler

In practice you almost never call `ParseToSQL` yourself — the storage adapter's `Search` method does it for you. A complete list-with-filter endpoint:

```go title="routes/tasks.go"
func (h *TasksHandler) List(w http.ResponseWriter, r *http.Request) error {
    filter := r.URL.Query().Get("filter")
    cursor := r.URL.Query().Get("next")

    var tasks []Task
    next, err := h.store.Search(&tasks, "id", filter, 50, cursor)
    if err != nil {
        var bad *magicerrors.BadRequest
        if errors.As(err, &bad) {
            return bad // client error: bad filter string — 400
        }
        return err // storage/runtime failure — let ErrorHandler map it to 500
    }
    render.JSON(w, r, map[string]any{"items": tasks, "next": next})
    return nil
}
```

`Search` already does the parse/validation-vs-runtime classification for you: it wraps a bad `filter` string — including an `*lucene.InvalidFieldError` — into a `*magicerrors.BadRequest` before returning, so checking for `InvalidFieldError` here would never match. Match `*magicerrors.BadRequest` instead. It maps to HTTP 400 via the [`ErrorHandler` middleware](tutorial.md#routes); any other returned error falls through to 500. (Since `BadRequest` already maps to 400, a plain `return err` is also correct — the explicit branch is here only to show where you'd attach handler-specific context.) You only match the raw `*lucene.InvalidFieldError` when you call `parser.ParseToSQL` yourself — see [Errors](#errors) below. A user sending `?filter=does_not_exist:foo` gets back the structured message with a list of valid fields.

## Safety limits

`NewParser` applies three limits to incoming queries. All are configurable via `lucene.ParserConfig`:

| Limit             | Default     | What it catches                                |
|-------------------|-------------|------------------------------------------------|
| `MaxQueryLength`  | 10000 bytes | Memory exhaustion via huge strings.            |
| `MaxDepth`        | 20          | Stack overflow from deeply nested parens.      |
| `MaxTerms`        | 100         | CPU exhaustion from many-term queries.         |

```go title="parser.go"
parser, err := lucene.NewParser(Task{}, &lucene.ParserConfig{
    MaxQueryLength: 2000,
    MaxDepth:       8,
    MaxTerms:       30,
})
if err != nil {
    return err
}
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

The DynamoDB driver is intentionally narrower than the SQL driver — PartiQL does not support fuzzy search, case-insensitive matching, or JSON path access the same way. Wildcards (rendered as PartiQL `begins_with`/`contains`) and equality are supported. See `storage/search/lucene/dynamodb_driver.go` for the exact mapping.

[Array fields](#array-multi-valued-fields) render as `contains(tags, ?)`, one per value, so `tags:(a OR b)` becomes `(contains(tags, ?) OR contains(tags, ?))` and `NOT tags:a` becomes `NOT (contains(tags, ?))`. Wildcards, ordering operators (`>`, `<`, `>=`, `<=`) and ranges on an array field are rejected with a parse error naming the field: PartiQL's `contains()` tests element membership rather than substrings, and a list attribute has no ordering.

## Full operator reference

| Operator              | Example                          | Postgres                                   | MySQL                                                 | SQLite                                |
|-----------------------|----------------------------------|--------------------------------------------|-------------------------------------------------------|---------------------------------------|
| Equality              | `status:received`                | `"status" = ?`                             | `"status" = ?`                                        | `"status" = ?`                        |
| Boolean               | `a:1 AND b:2`, `a:1 OR b:2`      | `(...) AND (...)`                          | same                                                  | same                                  |
| Negation              | `NOT status:cancelled`           | `NOT (...)`                                | same                                                  | same                                  |
| Inclusive range       | `amount:[100 TO 500]`            | `BETWEEN ? AND ?`                          | same                                                  | same                                  |
| Exclusive range       | `amount:{100 TO 500}`            | `> ? AND < ?`                              | same                                                  | same                                  |
| Comparison            | `amount:>100`                    | `"amount" > ?`                             | same                                                  | same                                  |
| Wildcard (scalar)     | `name:foo*`                      | `"name"::text ILIKE ?`                     | `` LOWER(`name`) LIKE LOWER(?) ``                     | `"name" LIKE ?`                       |
| Fuzzy                 | `name:foo~2`                     | `similarity("name"::text, ?) > 0.3`        | `` SOUNDEX(`name`) = SOUNDEX(?) ``                    | **error** — use wildcards             |
| Null                  | `field:null`                     | `"field" IS NULL`                          | same                                                  | same                                  |
| Has value (scalar)    | `field:*`                        | `"field"::text ILIKE ?` (param `%`)        | `` LOWER(`field`) LIKE LOWER(?) ``                    | `"field" LIKE ?`                      |
| JSON sub-field        | `metadata.tier:gold`             | `metadata->>'tier' = ?`                    | `JSON_UNQUOTE(JSON_EXTRACT(metadata, '$.tier')) = ?`  | `JSON_EXTRACT(metadata, '$.tier') = ?` |
| Array containment     | `tags:golang`                    | `COALESCE("tags" @> ?, false)`             | `` COALESCE(JSON_CONTAINS(`tags`, ?), false) `` | `EXISTS (SELECT 1 FROM json_each("tags") WHERE value = ?)` |
| Array wildcard        | `tags:*go*`                      | `EXISTS (SELECT 1 FROM unnest("tags") AS elem WHERE elem ILIKE ?)` | `` JSON_SEARCH(LOWER(CAST(`tags` AS CHAR)), 'one', LOWER(?)) IS NOT NULL `` | `EXISTS (SELECT 1 FROM json_each("tags") WHERE value LIKE ?)` |
| Array has value       | `tags:*`                         | `"tags" IS NOT NULL`                       | same                                                  | same                                  |
| Grouped field         | `tenant_id:(a OR null)`          | `("tenant_id" = ? OR "tenant_id" IS NULL)` | same                                                  | same                                  |
| Implicit (unfielded)  | `foo`                            | OR across all `ImplicitSearch=true` fields | same                                                  | same                                  |

## Adding a database

Every per-database difference lives behind the `Dialect` interface in
`storage/search/lucene/dialect.go`. Adding a database is three steps:

1. Create `dialect_<name>.go` with a type implementing `Dialect`.
2. Register it from that file's `init`:
   `func init() { registerDialect(myDialect{}) }`
3. Add the provider to the executed-test matrix in `sql_executed_test.go`.

There is no separate allowlist to update — the registry is the only place a
provider name is enumerated, so a name that resolves is a name that is fully
implemented.

The eight operations are the provider name, identifier quoting, JSON subfield
extraction, scalar `LIKE`, array wildcard, array containment, element
encoding, and fuzzy matching. The compiler names any you leave out, and
`TestEveryDialectImplementsEveryOperation` fails if an implementation returns
something that ignores its argument.

`Fuzzy` may return an error when the database has no equivalent; SQLite does
exactly that.

This structure replaced seventeen `switch provider` statements scattered
through the renderer. Two of them fell through *silently* rather than
erroring — identifier quoting defaulted to double quotes for any non-MySQL
provider, and JSON subfield extraction returned the bare column name — so a
half-added provider produced queries that ran and returned wrong rows. Both
are compile errors now.
