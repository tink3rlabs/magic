# Contributing

First, thank you for contributing! We love and encourage pull requests from everyone.

Before submitting major changes, here are a few guidelines to follow:

1. Check the [open issues](https://github.com/tink3rlabs/magic/issues) and [pull requests](https://github.com/tink3rlabs/magic/pulls) for existing discussions.
2. Open an [issue](https://github.com/tink3rlabs/magic/issues) first, to discuss a new feature or enhancement.
3. Write tests, and make sure the test suite passes locally and on CI.
4. Open a pull request, and reference the relevant issue(s).
5. After receiving feedback, [squash](https://gitready.com/advanced/2009/02/10/squashing-commits-with-rebase.html) your commits and add a [great commit message](https://www.freecodecamp.org/news/how-to-write-better-git-commit-messages/).

## Branching Strategy

We use **trunk-based development**. There is one long-lived branch — `main` — and short-lived topic branches that merge into it via squash-PR.

- **`main`** — always releasable. Every push runs CI, and `feat:` / `fix:` commits trigger an automated release tag.
- **Topic branches** — name them by intent: `feat/<short-desc>`, `fix/<short-desc>`, `chore/<short-desc>`, `docs/<short-desc>`, `ci/<short-desc>`. Keep them small and short-lived. Delete on merge.
- **`next`** — created **only when we are incubating breaking changes for a major release** (e.g. v1.0 prep). It is a temporary prerelease branch. Tags emitted from `next` are prereleases (`vX.Y.Z-beta.N`). Once the major ships, `next` merges back to `main` and is deleted.
- **`release-vN`** — created **only after v1.0**, and only if we need to patch a previous major while a newer one is in flight. Not in use today.

There is no `develop` branch and no permanent `beta` branch.

## Conventional Commits

Commit messages follow the [Conventional Commits](https://www.conventionalcommits.org/) spec. CI enforces this via `webiny/action-conventional-commits`.

Allowed types: `feat`, `fix`, `perf`, `refactor`, `test`, `docs`, `ci`, `cicd`, `chore`, `patch`, `release`, `dev`.

Release impact:

| Commit prefix                          | Release effect                                          |
|----------------------------------------|---------------------------------------------------------|
| `feat:`                                | minor version bump                                      |
| `fix:` or `perf:`                      | patch version bump                                      |
| `feat!:` or `BREAKING CHANGE:` footer  | major version bump (gated to `next` until v1.0)         |
| `chore:`, `docs:`, `ci:`, `test:`, `refactor:` | no release                                       |

Use the `!` suffix or a `BREAKING CHANGE:` footer to signal breaking changes. While we are on `v0.x`, breaking changes are still permitted on `main` per SemVer, but please call them out explicitly in the commit body.

## Releases

Releases are fully automated by [`go-semantic-release`](https://github.com/go-semantic-release/semantic-release).

- **Trigger**: every successful CI run on `main` checks the new commits and decides whether to cut a tag.
- **Versioning**: [Semantic Versioning](https://semver.org/). We are pre-v1.0 — the public API may change in minor releases.
- **Tags**: pushed in the form `vX.Y.Z`. Tags are immutable and produced only by the release workflow.
- **Changelog**: auto-generated and prepended to `CHANGELOG.md` on each release.
- **API reference**: published automatically on [pkg.go.dev](https://pkg.go.dev/github.com/tink3rlabs/magic) once a tag is visible to the Go module proxy.

### Prereleases

When we are preparing a breaking release, work happens on the `next` branch and emits prerelease tags (`vX.Y.Z-beta.N`, `vX.Y.Z-rc.N`). Install a prerelease explicitly:

```bash
go get github.com/tink3rlabs/magic@v1.0.0-beta.1
```

If `next` does not exist in the repo, there is no active prerelease line — install `@latest`.

## Local docs build

The docs site uses [mkdocs-material](https://squidfunk.github.io/mkdocs-material/) with the social-cards plugin, which requires Cairo and Pango natives.

On Debian/Ubuntu:

```bash
sudo apt-get install -y libcairo2-dev libfreetype6-dev libffi-dev libjpeg-dev libpng-dev zlib1g-dev pkg-config
```

On macOS:

```bash
brew install cairo freetype libffi libjpeg libpng zlib pkg-config
```

Then:

```bash
pip install -r requirements-docs.txt
mkdocs serve
```

The site is published from `gh-pages` by the `Docs` workflow. Versioned with [mike](https://github.com/jimporter/mike); see `.github/workflows/docs.yml`.
