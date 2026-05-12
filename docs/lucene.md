# Search (Lucene)

magic exposes a single `filter=` query parameter on every list endpoint. The value is a [Lucene query string](https://lucene.apache.org/core/2_9_4/queryparsersyntax.html) which magic translates to safe, parameterized SQL.

> This is a stub. A full reference (operators, JSON field paths, fuzzy matching, null handling) will land in a follow-up.

## Quick examples

```text
status:received
status:received AND counterparty_id:abc123
amount:[100 TO 500]
name:foo*
name:foo~2
metadata.tier:gold
```

## Supported operators (overview)

| Operator              | Example                          | SQL effect (Postgres)              |
|-----------------------|----------------------------------|------------------------------------|
| Equality              | `status:received`                | `status = $1`                      |
| Boolean               | `a:1 AND b:2`, `a:1 OR b:2`      | `AND` / `OR`                       |
| Range                 | `amount:[100 TO 500]`            | `BETWEEN`                          |
| Wildcard              | `name:foo*`                      | `ILIKE`                            |
| Fuzzy                 | `name:foo~2`                     | trigram / Levenshtein              |
| Null                  | `field:null`                     | `IS NULL`                          |
| Not null              | `field:*`                        | `IS NOT NULL`                      |
| JSON sub-field        | `metadata.tier:gold`             | `metadata->>'tier' = $1`           |

For the full grammar and edge cases, read the source under [`storage/search/lucene/`](https://github.com/tink3rlabs/magic/tree/main/storage/search/lucene).
