# Maintainers

Who maintains Benes, and the policy that decides when a change can merge.

## Current maintainers

| GitHub account | Project role | Responsibilities |
| --- | --- | --- |
| [@Wibias](https://github.com/Wibias) | Project owner | Direction, releases, repository administration, triage, `dev` integration, security review, provider and CI maintenance |

The table records project responsibility. Repository permissions are whatever GitHub is configured to grant.

[`.github/scripts/pr-maintainers.cjs`](./.github/scripts/pr-maintainers.cjs) parses the **`## Current maintainers`** heading and the table under it to decide who receives review-ready notifications. Renaming the heading silently breaks that. Retired accounts belong in the change log at the bottom, never in the table.

Everything lands on `dev` first; nothing else integrates.

## Review and merge policy

**Targets**

- Every pull request is opened against `dev`. `preview` carries prereleases, and `main` moves only when a maintainer promotes `dev` to it.
- `enforce-target` accepts `dev`, or a stacked child whose base is another open pull request.
- It also refuses a branch whose head still sits on `main`'s tip with `dev` far ahead of it, plus descriptions that are empty, thin, or malformed.
- A title or description that mentions `gui` has to carry a screenshot of the change, unless a maintainer waives it with `gui-screenshot-waived`.

**Draft gate for contributors**

- An author without push permission gets a draft, and stays in draft until four generated boxes are ticked: the author's local CI run passed; the branch is on the newest `dev` commit (or no more than ten behind); every correct Codex or CodeRabbit finding is addressed; and the ready-for-review confirmation is present.
- Four ticks move the pull request out of draft and notify the maintainers in this file, minus the author. That completion belongs to one commit: pushing again re-opens every box, puts the pull request back in draft, and drops the notification.
- The gate verifies the two claims it can check itself: ancestry (on the latest `dev` commit, or at most ten commits behind it) and review findings. The local-CI box stays an author attestation, because a forked branch cannot start repository CI. A claim the gate can disprove is unticked and the pull request goes back to draft.
- Authors with repository push permission skip the ancestry heuristic only.

**Approval and evidence**

- At least one other maintainer approves, and required checks pass. Authors do not approve their own work.
- Hosted GitHub Actions on this organization may be blocked by billing. When runners never start, local evidence is the evidence: `go test ./...`, `go vet ./...`, `npm run privacy:scan`, plus the GUI or docs build when those trees changed. A workflow that failed to start is not a green check.

**Security and high-risk surfaces**

- Authentication, credential handling, workflow files, release automation, dependency installation, and comparable boundary changes need explicit security review.
- A new or promoted provider preset is a credential-destination change, so it needs primary-source evidence before merge: documented OpenAI-compatible endpoints (including an authenticated `GET /v1/models` when the entry declares `liveModels`), terms of service and operating legal entity, resale or routing authorization for aggregators, a named maintenance owner, and a citable verification date. An affiliation with the service is disclosed and does not lower the bar. Incomplete evidence means no registry row.
- With more than one maintainer, security-sensitive and release-related changes should get both reviews.

**Direct pushes and promotion**

- Direct pushes are reserved for maintainer-owned integration work, urgent repairs, and incident recovery. Test and documentation requirements do not relax for them.
- Promotion from `dev` to `main`, and npm releases, remain maintainer-controlled.

## Maintainer changes

Adding or removing a maintainer needs all three of:

1. agreement from the project owner,
2. review by another current maintainer when one exists, and
3. updates to this file and [`.github/CODEOWNERS`](./.github/CODEOWNERS).

### Change log

- 2026-07-27 — [@Wibias](https://github.com/Wibias) becomes the project owner.

  CODEOWNERS requests reviews; it does not enforce them, because this repository has no branch protection rule configured. The approval requirement above rests on the same convention. Making either of them a gate is a separate decision.

## Security reports

Private vulnerability reports reach the maintainers and are handled under [`SECURITY.md`](./SECURITY.md). Secrets and exploit detail stay out of public issues.
