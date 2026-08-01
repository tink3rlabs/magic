# Contributing

Thanks for wanting to help. This page covers what you need to hack on magic itself — code or docs. The root [CONTRIBUTING.md](https://github.com/tink3rlabs/magic/blob/main/CONTRIBUTING.md) has the full branching, release, and Conventional Commits reference.

## Local Go development

```bash
git clone https://github.com/tink3rlabs/magic.git
cd magic
go test ./...
```

Unit tests don't need a database — adapters that need one (Postgres, MySQL, DynamoDB, CosmosDB) use testcontainers and skip themselves when Docker isn't available. The in-memory adapter and Lucene parser have full coverage you can iterate against without any infra.

Useful entry points:

- `examples/main.go` — a runnable end-to-end example. Run it with `go run ./examples` (see `examples/README.md` for the env vars it expects).
- `storage/storage_test.go` — adapter contract tests, parameterized by backend.
- `storage/search/lucene/` — the Lucene parser and per-provider SQL drivers.

Run the full check before opening a PR:

```bash
go vet ./...
go test ./...
```

## Local docs build

The docs site is MkDocs (Material) with versioning via `mike`. Build dependencies live in `requirements-docs.txt`.

```bash
python3 -m venv .venv-docs
.venv-docs/bin/pip install -r requirements-docs.txt
.venv-docs/bin/mkdocs serve
```

Open http://localhost:8000. Pages live under `docs/` and the nav is set in `mkdocs.yml`. Build strictly before committing doc changes:

```bash
.venv-docs/bin/mkdocs build --strict
```

`--strict` turns warnings (broken links, unrecognized references) into errors.

### Keeping the tutorial in sync

`docs/tutorial.md` is a build-along over the real [`tink3rlabs/todo-service`](https://github.com/tink3rlabs/todo-service) repo. Its code blocks are not copied — they're **included live** from todo-service via `pymdownx.snippets` URL includes, each pinned to a todo-service **release tag** (currently `v0.9.0`). The included regions are delimited in todo-service's source by `--8<-- [start:name]` / `--8<-- [end:name]` marker comments.

To re-sync after a todo-service release, bump the tag in the snippet URLs in `tutorial.md` (find/replace the old tag with the new one). If a quoted region's shape changed, update the marker comments in todo-service in the same change so the included slice still lines up with the surrounding prose.

`mkdocs build --strict` fetches every include and fails on a stale or broken pin — a missing file, a renamed marker, an unreachable tag. Drift is caught by CI, not left to process: the tutorial can't quote code that doesn't exist in todo-service at the pinned ref.

Note that this couples the docs build to the network: it fetches these URLs from GitHub's raw CDN at build time, so the docs deploy depends on GitHub raw being reachable and the pinned tag staying live. If a docs build fails on a snippet fetch for a change unrelated to the tutorial, suspect a transient CDN issue or a deleted/moved pinned tag before anything else.

## Branching and commits

Trunk-based, short-lived topic branches off `main`. Naming: `feat/<desc>`, `fix/<desc>`, `docs/<desc>`, `chore/<desc>`. CI enforces [Conventional Commits](https://www.conventionalcommits.org/): `feat:` cuts a minor release, `fix:` cuts a patch, `docs:`/`chore:`/`test:` cut nothing. Full table and release flow: [root CONTRIBUTING.md](https://github.com/tink3rlabs/magic/blob/main/CONTRIBUTING.md).

## How to add an example

Put new examples under `examples/<name>/` with a `main.go` and a top-of-file comment block explaining how to run it (env vars, expected output). Reference them from the relevant doc page rather than inlining 100 lines of setup.
