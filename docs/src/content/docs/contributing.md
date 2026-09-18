---
title: Contributing
description: How to change the Go CLI, dashboard, and docs on Wibias/Benes.
---

Integration branch is **`dev`**. `main` is release-only. `preview` is the prerelease train.

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

Privacy scan (repo root):

```bash
npm run privacy:scan
```

This docs app:

```bash
cd docs && npm ci && npm run build
```

Do not log request bodies, API keys, or account identifiers. Security write-ups belong in scratch (`.tmp/` or `mktemp`), never in `docs/` or `notes/`.

## Layout

| Path | Role |
| --- | --- |
| `cmd/benes`, `internal/` | Go CLI and listener |
| `gui/` | Dashboard |
| `docs/` | This Starlight site |
| `notes/` | Optional original notes; created on demand |
| `scripts/release.ts` | Release authority |

Nested agent rules: `gui/AGENTS.md`, `docs/AGENTS.md`, `scripts/AGENTS.md`, `.github/AGENTS.md`.

## Pull requests

Open PRs against **`dev`**. A ready PR is the author's claim that the change is finished and tested. Maintainers name problems; they do not take over the branch.

- You do not need permission to fix a bug you hit. An issue first helps for design-sized work.
- Name the commands you ran and their results. Hosted Actions on this repo may be billing-blocked. "CI is green" with no local evidence is not a plan.
- Canonical local proof is `npm run ci:pr -- <PR_NUMBER>`. It classifies the PR diff and runs only matching buckets (docs, GUI, Go `./...`, packaging, Linux/WSL, Darwin compile). Unknown paths still run every bucket. Edits to the local-CI orchestrators (`scripts/ci-pr-scope.ts`, `scripts/local-pr.ps1`, `ci-local.*`, `.github/workflows/ci.yml` / `go-core.yml`) enable Go, automation, and privacy - not GUI, packaging, or keyring unless those trees also changed. Force the full suite with `-Full` or `BENES_CI_PR_FULL=1`. Hosted `CI` uses the same classifier; jobs that do not match the diff are skipped, and the aggregate `ci` check still passes.
- Behavior changes in `cmd/` or `internal/` need a focused `*_test.go` next to the code.
- Cross-provider compile/stream contracts live in `go test ./internal/conformance`. That package is test-only and must not be imported from production code.
- Authors do not approve their own PRs. Another maintainer plus required checks is the merge bar.
- Workflow, release, and credential paths need explicit security review (`maintainer-sponsored` before merge).
- GUI diffs need a screenshot unless a maintainer applies `gui-screenshot-waived`.
- A PR that stalls with unresolved review feedback may be closed, with the reason stated.
- The full contract, including the draft checklist that contributor PRs have to complete, is the [PR contract](./pr-quality/).

## Provider presets

Adding or promoting a preset decides where credentials get sent, which is why it is treated as a
policy change rather than a data edit. The evidence bar, the disclosure rule for contributor
affiliations, and the fallback when evidence is incomplete are part of the review policy in
[`MAINTAINERS.md`](https://github.com/Wibias/Benes/blob/main/MAINTAINERS.md).

## Hooks

```sh
npm run setup:hooks
```

Installs `pre-push` → `npm run prepush` (`lint:gui:if-changed`, `go test ./...`, `privacy:scan`). Skip with `git push --no-verify` only in an emergency.

Review policy: [`MAINTAINERS.md`](https://github.com/Wibias/Benes/blob/main/MAINTAINERS.md). PR bar: [PR contract](./pr-quality/). Full local notes: [`CONTRIBUTING.md`](https://github.com/Wibias/Benes/blob/main/CONTRIBUTING.md).
