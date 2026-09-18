# `.github/`

Parent: `/AGENTS.md`. This tree is GitHub automation. Edits are a security boundary under `MAINTAINERS.md`.

## Actions

- Workflow-level `permissions:` default to `{}`. Each job grants only the scopes it uses.
- Pin third-party `uses:` to a 40-character commit SHA. Put the marketplace version in an end-of-line comment.
- A `pull_request` from a fork is untrusted. Do not give that job secrets or write tokens.
- `pull_request_target`, `workflow_dispatch` inputs, reusable workflows, artifacts, caches, and generated shell are the same class of trust boundary.
- Do not widen triggers, write access, token exposure, release eligibility, or publish rights unless the change requires it.
- Do not log secrets, tokens, account ids, or request bodies.

## Intake and review automation

Issue and PR bots must be deterministic: same payload, same verdict. Cover them with `.github/scripts/*.test.cjs`.

Public issues go through `ISSUE_TEMPLATE/` only (`blank_issues_enabled: false`). GitHub renders each field `label` as a `###` heading. `issue-intake-catalog.cjs` is the alias list `enforce-issue-quality` reads. Change a live label and that catalog in the same commit.

Pull requests target `dev`. Release and `main` promotion stay maintainer-controlled.

## Reviewing a workflow

Read the whole file: `on:`, `permissions:`, `if:`, interpolations, and every shell step. Hosted runners on this org may be billing-blocked; do not claim Actions passed unless Actions reports success for that commit.
