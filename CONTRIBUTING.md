# Contributing

Thanks for helping with benes.

- Public contributing page: [Contributing](./docs/src/content/docs/contributing.md)
- Pull-request quality contract: [PR contract](./docs/src/content/docs/contributing/pr-quality.md)
- Public user docs: [`docs/`](./docs)
- Maintainer roles and merge policy: [`MAINTAINERS.md`](./MAINTAINERS.md)
- Diagnostic notes (optional originals): create `notes/` when a long investigation needs a written record.
- Agent-facing repository rules: [`AGENTS.md`](./AGENTS.md)

## Branches

- `dev` — where pull requests land. It is the only integration target.
- `main` — releases only. A maintainer promotes `dev` into it; nothing else moves it.
- `preview` — the prerelease train.

Rebasing onto the current head is normal maintenance here, not a rewrite. Open it as an ordinary pull request and name the source commits in the description.

## Local checks

Production `benes` is the Go CLI.

```bash
git clone https://github.com/Wibias/Benes.git
cd Benes
go test ./...
go vet ./...
go run ./cmd/benes help
```

Dashboard:

```bash
cd gui && npm ci && npm run lint && npm run build
```

Privacy scan (repository root):

```bash
npm run privacy:scan
```

Docs site:

```bash
cd docs && npm ci && npm run build
```

Request bodies, API keys, and account identifiers never reach a log. Security write-ups are drafted in `.tmp/` or a `mktemp` directory, and stay out of `docs/` and `notes/` until the fix has shipped.

## Layout

| Path | Role |
| --- | --- |
| `cmd/benes`, `internal/` | Go CLI and listener |
| `gui/` | Dashboard |
| `docs/` | Public Starlight site (English, `base: /Benes/`) |
| `notes/` | Optional original notes; created on demand |
| `scripts/release.ts` | Release authority |

Nested agent rules: `gui/AGENTS.md`, `docs/AGENTS.md`, `scripts/AGENTS.md`, `.github/AGENTS.md`.

## Pull request contract

Marking a pull request ready for review is a claim about your own work: complete, understood, tested, mergeable. It is not a handover — the author keeps the branch, so CI failures, missing tests, conflicts and review findings stay with the author. Maintainers name problems; they do not take the branch over.

- **Fixing a bug you hit never needs permission.** A drive-by pull request is welcome. For design-shaped work an issue first helps, although it is not a gate on contributing.
- Behavior changes in `cmd/` or `internal/` ship with a focused `*_test.go` next to the code. "Tested" or "CI" without named commands and results is not evidence.
- Name the commands you ran and what they printed. Hosted Actions on this repository may be blocked by billing; local output still counts.
- Authentication, workflow, release-automation and dependency-installation surfaces need a maintainer to sponsor the change (`maintainer-sponsored`) before merge. Those are the places where a bad merge is expensive to unwind.
- A dashboard diff needs a screenshot unless a maintainer applies `gui-screenshot-waived`.
- Authors do not approve their own pull requests. Another maintainer plus the required checks is the merge bar ([`MAINTAINERS.md`](./MAINTAINERS.md)).
- A pull request that stalls with unresolved review feedback may be closed, with the reason stated. It can be reopened once that reason is gone, or replaced with a clean one.

The full contract, including the draft checklist contributor pull requests have to complete, is the [PR contract](./docs/src/content/docs/contributing/pr-quality.md). Template: [`.github/PULL_REQUEST_TEMPLATE.md`](./.github/PULL_REQUEST_TEMPLATE.md).

## Pre-push hook

```sh
npm run setup:hooks
```

One run after cloning installs a local `pre-push` hook. It points `core.hooksPath` at the relative path `scripts/hooks` while that key is unset, so linked worktrees share one value and each checkout resolves its own copy; a previously configured absolute Benes path is migrated to the relative form. If you already run a custom `core.hooksPath`, the command refuses and leaves it alone.

`pre-push` runs `npm run prepush` — `lint:gui:if-changed`, `test`, `privacy:scan` — before every push. `lint:gui:if-changed` runs oxlint (including `oxlint-plugin-react-doctor`) only when the push touches `gui/`. `post-merge` rebuilds the packaged dashboard when its sources changed.

The same checks run on ubuntu-latest, macos-latest and windows-latest in CI, where the CLI is also smoke-tested and the dashboard is built. Hosted runners may currently be blocked by billing. `git push --no-verify` is the emergency exit, not the habit.
