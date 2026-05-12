# Storage Adapters

magic ships adapters for the stores most Go services reach for.

> This is a stub. Per-adapter setup guides will land in follow-up PRs.

| Adapter    | Status        | Notes                                  |
|------------|---------------|----------------------------------------|
| Postgres   | Stable        | Reference adapter. JSONB supported.    |
| MySQL      | Stable        | JSON column support.                   |
| SQLite     | Stable        | Good for tests and small deployments.  |
| DynamoDB   | Stable        | AWS SDK v2.                            |
| Cassandra  | Experimental  | API may change.                        |
| In-memory  | Stable        | Tests and prototyping only.            |

See the [`storage/`](https://github.com/tink3rlabs/magic/tree/main/storage) package for source and per-adapter README files.
