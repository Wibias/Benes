---
title: Pull request contract
description: What "ready for review" means on Wibias/Benes, and what the checks enforce.
---

Open pull requests against **`dev`**. `main` only moves by maintainer promotion, so a change is not
shipped when it merges - it ships when `main` is promoted.

## What ready means

Marking a pull request ready for review is a claim about your own work: the change is complete, you
understand it, it was tested, and it can be merged. It is not a handover. The author keeps the branch,
so CI failures, missing tests, conflicts, and review findings stay with the author. A maintainer who
finds a problem names it; they do not adopt the branch to fix it.

## The draft gate

A pull request from an author without push permission opens as a draft and stays one until the
generated review-readiness checklist is complete:

- local CI was run and is green (the author's own report: a forked branch cannot start repository CI);
- the branch sits on the latest `dev` commit, or at most ten commits behind it;
- every correct Codex or CodeRabbit finding is resolved;
- the ready-for-review confirmation is there.

The gate binds completion to the commit the checklist was written against, and verifies the two claims
it can check itself: ancestry and review findings. A later push re-opens the checklist and returns the
pull request to draft, so it has to be re-ticked against the new head. Once all four hold, the gate
marks the pull request ready and notifies the maintainers listed in
[`MAINTAINERS.md`](https://github.com/Wibias/Benes/blob/main/MAINTAINERS.md), excluding the author.

Authors with push permission skip the ancestry heuristic only. Everything else still applies.

## What gets rejected

- A head that sits on the `main` tip while far behind `dev`.
- An empty, thin, or malformed description. Every section of
  [the template](https://github.com/Wibias/Benes/blob/main/.github/PULL_REQUEST_TEMPLATE.md) has to be
  filled in: Summary, Verification, Checklist.
- A pull request whose title or description mentions `gui` without a screenshot of the change, unless a
  maintainer applies `gui-screenshot-waived`.

## Evidence

Name the commands you ran and what they printed. The default set is `go test ./...`, `go vet ./...`,
and `npm run privacy:scan`, plus the GUI and docs builds when those trees changed. Hosted Actions on
this repository may be blocked by billing, so a green badge is not evidence on its own - paste the
local output.

Behavior changes under `cmd/` or `internal/` need a focused `*_test.go` beside the code.

`npm run ci:pr -- <PR_NUMBER>` is the canonical local run. It classifies the diff and executes only the
buckets that match it, so it has to run against the exact head you are asking to merge; any new commit
means running it again.

## Merge bar

- Another maintainer approves. Authors do not approve their own work.
- Authentication, credential handling, workflow files, release automation, and dependency installation
  need explicit security review, and the `maintainer-sponsored` label, before the surface they touch is
  merged.
- A pull request that stalls with unresolved findings may be closed, with the reason stated. It can be
  reopened once that reason is gone, or replaced with a cleaner one.
