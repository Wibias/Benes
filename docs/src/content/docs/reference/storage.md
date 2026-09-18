---
title: Storage
description: CODEX_HOME report, archived cleanup, quarantine, restore, and cleanup policy.
---

Dashboard **Storage** has three views: Overview (`#storage`), Cleanup (`#storage/cleanup`), and Quarantine (`#storage/quarantine`). It reports files under Codex's home and can quarantine or permanently delete **archived sessions** only. Active `sessions/`, databases, attachments, and anything outside `CODEX_HOME` are never cleanup candidates. `benes observe storage` still only prints the report. Unknown `storage/*` hashes normalize to `#storage`.

Implementation: `internal/storage` plus loopback handlers in `internal/server/storage_api.go`, `storage_mutation_api.go`, and `storage_policy_api.go`.

## What the report measures

Loopback `GET /api/storage` walks `CODEX_HOME` once.

- `bytes` is **logical**: the sum of each path's size. Hardlinked names each count.
- `fileCount` is the number of paths.
- `total.physicalBytes` is unique physical identity across the **entire** report (volume/index). A hardlink with one path in `sessions/` and another in `archived_sessions/` is counted once in the total.
- Bucket `physicalBytes` is unique within that bucket only, and is omitted when it equals the bucket's logical `bytes`.

Largest-file lists are capped at 5 entries, ordered by size descending then `relPath` ascending.

The walk is live, not a filesystem snapshot. A file that disappears while scanning is skipped. Cleanup always revalidates identity before it mutates.

`.trash` is omitted from the report. Directory symlinks and Windows junctions are not followed. A symlink **file** inside `CODEX_HOME` may be counted as an object; its external target is not walked.

`truncated: true` means the walk hit the 250 000-file cap. Cleanup preview/execute refuse that tree rather than delete a percent of a partial set.

File contents are never returned.

## Archived

**Archived** means regular files (and symlink objects) under `archived_sessions/`. Report bucket `archived_sessions`, cleanup candidates, and policy `archivedBytesOver` all use that classification.

Active `sessions/` files are never candidates.

## Preview, digest, and stale plans

`POST /api/storage/cleanup/preview` `{percent}` (1–100) selects the oldest archived files using ceil(`count * percent / 100`), at least one file when the set is non-empty. Oldest means `mtime` ascending, then `relPath` ascending. Missing timestamps sort first.

The digest is SHA-256 of that selected set (relative path, size, mtime, mode, volume/index). It is a freshness token, not a signature and not path authorization. Execute reconstructs the plan server-side. Client candidate paths are informational.

`POST /api/storage/cleanup` `{percent, mode, digest}`:

| Result | When |
| --- | --- |
| `invalid_digest` / `invalid_mode` | request validation |
| `codex_busy` | `state.sqlite` is locked |
| `storage_mutation_busy` / `restore_pending_overlap` | another cleanup/restore holds the mutation lock, or selected paths overlap an incomplete restore |
| `stale_preview` | the reconstructed candidate set no longer matches the digest (add/remove/replace/size/order/link change) |
| `referenced_history` | the digest still matches the unfiltered set, but a selected file is now referenced in `state.sqlite` |

Referenced-history is evaluated at execute time, not only at preview. Permanent delete uses the same rule.

The JSON `candidates` array is capped at 256 rows (`truncated: true` if clipped). `count` / `bytes` still describe the full plan. Preview `bytes` counts unique physical identity in the selected set (hardlinked names count once); `count` is logical paths.

## Quarantine vs permanent delete

Default mode is `quarantine`. Files move to `CODEX_HOME/.trash/<id>/files/…` with `manifest.json`. `id`/`epoch` are `<unixMilli>-<hex>` and never overwrite an existing directory. `.trash` itself must be a real directory inside `CODEX_HOME`. A symlink or Windows junction at `.trash` is refused.

Cleanup writes a planned manifest (`status: moving`, every file `phase: planned`) **before** the first rename, then updates each file's phase as it succeeds. Progress write failures abort the job. Crash recovery inspects the planned sources and destinations; it does not depend on a best-effort journal.

`count` / `bytes` on the cleanup result are what actually moved (`bytes` / `removedArchivedBytes` are unique physical identity among moved archived paths). `freedBytes` is physical storage actually reclaimed:

- Quarantine only renames on the same filesystem, so `freedBytes` is `0`.
- Permanent delete counts a file's bytes in `freedBytes` only when the last hardlink is unlinked. A surviving link (including one outside `archived_sessions/`) keeps `freedBytes` from claiming that inode.

A mid-plan filesystem failure returns `ok: false`, `fs_failed`, `partial: true`, and a restorable trash entry for the files that did move.

Permanent delete moves to trash first, then unlinks those objects and removes the trash entry when every planned file is `deleted`. A failed permanent delete leaves a trash row with `mode: "permanent"` and `partial: true`. Remaining files in that entry can be restored. A successful permanent delete creates no restorable entry.

Moves use `os.Rename` on the same volume. That is not a multi-file transaction.

## Restore

`GET /api/storage/trash` lists up to 500 entries, newest first. Unreadable or incomplete trash directories are **not** omitted: they appear in additive `recoveryNeeded` rows (`id`, `status`, `error`). An empty `entries` array with empty `recoveryNeeded` means trash is genuinely empty. Reconciliation never deletes a corrupt entry.

`POST /api/storage/trash/restore` `{id}` is resumable:

- Manifest paths are re-validated. `..`, absolute, drive, UNC, and non-`archived_sessions/` paths are `invalid_trash`.
- Restore never follows a symlink or junction ancestor to an outside target.
- Files already recorded `phase: restored` with the destination present are skipped (`alreadyRestored`). Restore uses a no-replace move: a destination that exists, including one that appears after the preflight check, stays `dest_exists` and is not overwritten.
- Generic source absence without restore progress is `missing_trash`.
- Partial restore keeps the trash entry. Retry continues remaining files and can retry database reconciliation after every file has already moved.
- The trash directory is removed only after both filesystem restore and `state.sqlite` row restore succeed.
- Timeout (10 minutes) is `restore_worker_timeout`. Shutdown cancel is `restore_worker_aborted`. Neither reports `ok: true`.
- `count` / `bytes` are work done by **this** attempt. `alreadyRestored`, `totalCount`, and `totalBytes` describe the whole entry.

## CODEX_HOME boundary

Every mutate path is lexically normalized, then walked component-by-component with `Lstat`. Directory symlinks, Windows junctions, and other reparse points are never traversed. Missing parents are created as real directories. Immediately before rename, mkdir, or write, ancestors are checked again. Prefix-string checks are not used.

Manifest writes use an exclusive random temporary file in the real parent directory and write through that file handle. They do not follow a pre-existing `manifest.json.tmp` symlink. Manifest reads open the final object without following a leaf symlink or reparse point; an external target is never used as mutation authority.

No cleanup, quarantine, permanent delete, manifest write, reconciliation, or restore may mutate outside the canonical `CODEX_HOME` root.

Symlink and junction **file objects** inside the home may be renamed as objects; their external targets are not deleted.

## Referenced history

`state_5.sqlite` (or `sqlite_home` / `CODEX_SQLITE_HOME`) TEXT values that resolve to an `archived_sessions/` path protect that file. Benes `$BENES_HOME/sessions.sqlite` is a different store and is not a Codex file index.

Final cleanup authorization takes a SQLite `BEGIN IMMEDIATE` write lock, re-reads references under that lock, keeps the lock through filesystem mutation and thread-row reconciliation, then commits or rolls back. The planned trash manifest includes any thread rows that cleanup may delete **before** the first filesystem mutation or database delete. A path that becomes referenced after authorization starts is not moved, and its thread row is not deleted. If the database is locked, cleanup/restore return `codex_busy` rather than guessing.

## Concurrency

One storage mutation at a time (cleanup or restore), including scheduled vs manual. Preview is read-only; a racing mutation makes execute `stale_preview`.

## Cleanup policy

Stored as `storageCleanup` in `~/.benes/config.json`. Default is **disabled**, `schedule: "manual"`, `mode: "quarantine"`, threshold 5 GiB, target oldest 25%. Never enabled automatically.

| Schedule | When it runs |
| --- | --- |
| `manual` | only `POST /api/storage/cleanup-policy/run` |
| `startup` | once when the listener starts serving |
| `daily` | next local calendar midnight after enable or last job finish (DST-safe `AddDate`, not +24h) |
| `weekly` | seven local calendar midnights after enable or last job finish |

Timezone is the process local zone. Enabling or saving a daily/weekly policy stores one durable `nextRun`. Ordinary GET/read/scheduler polls do not move it. Process restart keeps the same due time. Once `now >= nextRun` the job is eligible once; after finish/skip/defer/fail the next due time is persisted from that completion. Manual policies have no automatic `nextRun`. Disabled policies do not run.

`lastRun.freedBytes` is physical storage reclaimed (zero for quarantine). `job.finishedAt` / `lastOutcome` record skip, defer, and failure too, so a skip does not tight-loop. If the final policy write after a job fails, GET does not keep reporting `running`; the last error is `config_write_failed` and restart recovery still clears a genuinely stale persisted `running` job.

`job.status=running` survives ordinary GET/PUT/run reads in the current process. A second manual run returns `already_running`. PUT while a job is running is `already_running`. Scheduled and manual runs share the same in-process generation; a stale finish cannot overwrite a newer job. Process-start recovery (when the listener starts serving) converts a persisted `running` job from a previous process into `idle` with `restore_worker_aborted`. `codex_busy` is deferred, not success.

Automatic storage work (startup cleanup and the scheduler) starts only after the data plane has bound and reached “listener starts serving”. Bind, runtime publication, or AfterListen failure does not start it. Parent-context cancel, `/api/stop`, and HTTP serve return all cancel the scheduler.

Target modes `reduceToBytes` and `removeOldestPercent` are mutually exclusive.

Skipped reasons: `disabled`, `under_threshold`, `nothing_selected`.

## Privacy

Responses may include relative paths needed to confirm a cleanup. They do not include file contents, secrets from files, or unnecessary absolute host paths. Error text is sanitized and does not dump raw manifests.
