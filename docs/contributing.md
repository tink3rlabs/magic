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

- `examples/main.go` — a runnable end-to-end example. Hit it with `make run-example`.
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

`docs/tutorial.md` is a build-along over the real [`tink3rlabs/todo-service`](https://github.com/tink3rlabs/todo-service) repo. Every code snippet in it is copied **verbatim** from todo-service at a **pinned git ref** — cited once near the top of `tutorial.md` (currently a commit SHA, to become a release tag once todo-service cuts one). MkDocs can't pull source across repos, so the snippets live inline and drift is controlled by process, not tooling.

When todo-service changes in a way that affects the tutorial, the *same* change set to `docs/tutorial.md` must:

- re-copy the affected snippets verbatim from the new ref,
- update the pinned ref reference in the tutorial's intro, and
- re-run `mkdocs build --strict`.

The tutorial must **never** contain code that doesn't exist verbatim in todo-service at the pinned ref — no hand-edited, paraphrased, or "cleaned-up" snippets.

## Branching and commits

Trunk-based, short-lived topic branches off `main`. Naming: `feat/<desc>`, `fix/<desc>`, `docs/<desc>`, `chore/<desc>`. CI enforces [Conventional Commits](https://www.conventionalcommits.org/): `feat:` cuts a minor release, `fix:` cuts a patch, `docs:`/`chore:`/`test:` cut nothing. Full table and release flow: [root CONTRIBUTING.md](https://github.com/tink3rlabs/magic/blob/main/CONTRIBUTING.md).

## How to add an example

Put new examples under `examples/<name>/` with a `main.go` and a top-of-file comment block explaining how to run it (env vars, expected output). Reference them from the relevant doc page rather than inlining 100 lines of setup.
