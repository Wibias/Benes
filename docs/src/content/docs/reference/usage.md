---
title: Usage
description: Durable request-usage aggregation for GET /api/usage and benes observe usage.
---

`GET /api/usage` (loopback) and `benes observe usage` summarize the durable observability ledger under `$BENES_HOME`. The same ledger is the durable authority for request-history (`benes observe logs index-status`, `explain <request-id>`, loopback `GET /api/request-history/:id/route-decision`, and `rebuild-index` as repair). `$BENES_HOME/routing-history.sqlite` is a derived index. It is reproducible from the ledger. A crash or SQLite failure must not make the ledger less durable. The ledger is metadata, routing outcome, and measurement only. It does not store prompts, responses, tool contents, request or response bodies, API keys, OAuth tokens, cookies, or Authorization headers.

Admitted model-request traffic (`POST /v1/responses`, `/v1/chat/completions`, `/v1/messages`) appends one JSONL row when the listener can write the file. That row is the canonical durable record for both Usage aggregation and request-history. Admission is an explicit request-lifecycle bit, not HTTP status. Denied traffic still appears in Diagnostics and does not create a Usage row, a request-history row, or a Session record. An admitted malformed request still counts as one Usage/request-history row. Historical files from before that writer still summarize. Historical request-history rows that already carry `requestId` still rebuild. A missing file is an empty report, not an error.

## What a row records

Authoritative fields on a stored row:

| Field | Meaning |
| --- | --- |
| `timestamp` | Request start, Unix milliseconds |
| `requestId` | Benes-owned `req_*` id for the admitted request. This is the request-history lookup key. It is not inbound `X-Request-ID` |
| `status` | Final HTTP status observed for that request |
| `durationMs` | Request duration in milliseconds |
| `requestedModel` | Selector the client asked for, when known |
| `provider` | Resolved provider id when known |
| `model` | Resolved model id when known |
| `account` | Safe Codex account log label bound to the credential that committed the upstream attempt (`main` for the app-login account). The label is taken from the same runtime/account snapshot that selected that credential and is not reconstructed from a later `config.json` read. Omitted when no account was committed. Not an email, token, alias, ChatGPT account id, or internal account UUID |
| `surface` | Declared client surface from allowlisted `X-Benes-Surface`. Omitted when unknown |
| `usageStatus` | `reported`, `estimated`, `unsupported`, or `unreported` |
| `usage` | Token counts when usage was present. Missing usage is not stored as zeros |
| `attempts` | Combo/policy hop metadata when present |
| `routeDecision` | Truthful routing outcome for explain. `routeKind` is `direct`, `combo`, or `policy` when routing ran. `selected` is the committed provider/model. `profile.id` is present only for a policy route. The object is present even when routing never ran (admitted malformed). The writer does not invent candidate eligibility, exclusions, selected reasons, or profile revisions the runtime does not have |

Untrusted strings are clipped before write. Malformed JSONL rows are skipped. Their contents are never copied into errors.

## Surface attribution

Known surfaces are `codex`, `claude`, `claude-desktop`, and `grok`. The runtime records a surface only from an explicit allowlisted `X-Benes-Surface` header. That header is caller-declared. Benes does not treat it as a cryptographic or transport-proven identity. It does not infer a client from model name, provider, protocol, User-Agent, or the absence of another tag. The header is not forwarded upstream.

Benes-managed client configuration stamps the header only where a supported extra-header mechanism exists:

- Codex `config.toml` `[model_providers.benes]` static `http_headers` (`X-Benes-Surface: codex`)
- Grok `config.toml` managed `extra_headers` (`X-Benes-Surface: grok`)

Claude Code / Claude Desktop currently have no Benes-managed extra-header path, so their traffic stays unattributed unless the client itself sends an allowlisted `X-Benes-Surface`. An arbitrary authenticated caller can also send that header; the stored value remains a declared surface.

Query `surface=codex|claude|grok` filters those explicit values. `surface=claude` includes `claude-desktop`. Empty, omitted, or unknown historical `surface` is **unattributed**. It is not Codex.

`surface=codex` therefore does not select unattributed rows, and it is not a proxy for “charged to a Codex account”. Client surface and provider/account attribution are separate. Per-account Usage joins on the committed account log label. Additive `surfaceAttribution` counts explicit `codex` / `claude` / `claudeDesktop` / `grok` plus `unattributed` inside the filtered window.

## Usage provenance

Token totals only include rows that carried a usage object.

| Status | Counted as measured? |
| --- | --- |
| `reported` | yes |
| `estimated` | yes |
| `unsupported` | no |
| `unreported` (including a missing status) | no |

Invariant: `measuredRequests == reportedRequests + estimatedRequests`. `coverageRatio` is `measuredRequests / requests` when `requests > 0`. Missing usage is not a measured zero.

## Cost confidence

Pricing uses the same table as Diagnostics/Sessions: built-in records plus one request-scoped read of operator `modelCosts` overlays. A Usage request loads that overlay at most once. The next request sees a `config.json` published by another process.

Legacy totals stay for compatibility:

| Field | Legacy meaning |
| --- | --- |
| `exactCostUsd` | Sum of exact-priced USD |
| `estimatedCostUsd` | Sum of exact **and** estimated USD (not a pure estimated-only figure) |
| `lowerBoundCostUsd` | Lower-bound USD |
| `staleCostUsd` | Stale-price USD |

Do not treat `estimatedCostUsd` as exact. Additive `cost` is the canonical structured summary: currency, exact / estimated / lower-bound / stale amount+count, priced / unpriced / unmetered request counts, and `displayTotalSafe`. Whole-query and nested `displayTotalSafe` is true only when priced requests are greater than zero, unpriced and unmetered are zero, and exactly one priced confidence class is present. A lower-bound-only or stale-only total is safe only because `status` names that class explicitly. Empty, all-unmetered, mixed, or gapped totals are not safe.

Model, provider, day, and account rows use the same cost classes as the query total. Mixed confidence is `mixed`. Account rows expose structured `cost` only; they do not emit `estimatedCostUsd`.

## Storage and read window

The logical durable ledger is one ordered stream of newline-committed JSONL records with **absolute** committed-byte offsets. Durable retention is opt-in (`maxBytes` / `maxAgeMs`, default disabled) via loopback `GET|PUT /api/usage/retention` and `POST /api/usage/retention/preview|run` (also `benes observe usage retention status|preview|run`). Retention deletes only whole prefix sources (legacy whole-file and sealed segments). `usage/active.jsonl` is never pruned. Crash order commits `$BENES_HOME/usage/retention.json` first, then deletes; leftover pending deletes are ignored by readers when that state proves they are retired. There is no automatic scheduler and no GUI for this policy.

Physical layout:

| Path | Role |
| --- | --- |
| `$BENES_HOME/usage.jsonl` | Legacy predecessor. Read-only for the writer. May be removed only as a whole source by opt-in retention; never rewritten |
| `$BENES_HOME/usage/active.jsonl` | Sole append target for new admitted rows |
| `$BENES_HOME/usage/segments/NNNNNNNN.jsonl` | Immutable sealed segments, ordered by a monotonic 8-digit sequence. Retention may delete a contiguous retired prefix; remaining sequences stay contiguous from `firstRetainedSequence` |
| `$BENES_HOME/usage/retention.json` | Versioned durable retention watermark (`retainedFromOffset`, `firstRetainedSequence`, `sequenceHighWater`, `legacyRetired`, `generation`, `pendingDeletes`). Symlink-safe atomic replace |

New writes go only to the segmented store. A listener process owns `$BENES_HOME`; concurrent request finalizers serialize on one in-process mutex. Multi-process writers are not supported. Usage aggregation and request-history catch-up may open a separate ledger handle; they take a filesystem-stable snapshot (list, open, re-list identities) so a rotation during discovery cannot omit a just-sealed segment. Directory and file reparse points / symlinks under the ledger path are refused on both read and write; Snapshot, Enumerate, EnumerateFromSnapshot, Status, and segment discovery do not follow them out of `$BENES_HOME`. A missing ordinary `usage/` directory is an empty ledger, not an error.

Sealed segments have a 64 MiB target. A complete record is never split across segments. A single record larger than the target is still stored, then sealed. Segment names are sequence numbers only. They do not contain account, provider, model, or request ids. Recognized sealed sequences must start at `00000001` and be contiguous. A gap is ledger corruption, not truncation and not retention. Usage then returns `"error":"read_failed"` rather than a complete snapshot. Request-history catch-up records the error and does not advance `indexedOffset`. `rebuild-index` remains the repair path after the ledger is readable again. The writer fails closed and does not extend a gapped sequence.

Before each append and before rotation, the writer truncates `active.jsonl` to its last newline. Uncommitted suffix bytes are discarded. They are never sealed as history. Complete newline-terminated rows are preserved exactly.

Request-history indexing is incremental. After a durable ledger append succeeds, the listener records a catch-up obligation and returns. One serialized background worker per listener indexer drains those obligations. Concurrent appends may coalesce. They must not disappear. Each catch-up still reads one filesystem-stable snapshot; bytes appended after that snapshot belong to the next catch-up. The model request does not wait for or fail on derived indexing. Listener start schedules an asynchronous catch-up and does not block on a historical scan. Catch-up writes and rebuild installation share one home-scoped exclusive writer lock (`routing-history.sqlite.write.lock`) so a CLI rebuild cannot replace a generation that a listener catch-up is still committing, and an older rebuild cannot overwrite a newer installed checkpoint. Listener background catch-up may wait for that lock; `Indexer.Close` cancels the wait so shutdown cannot hang. Cancellation is authoritative around lock acquisition: an already-canceled or mid-wait canceled acquire must not return a lock or run CatchUp, including when the lock became free in the same race; a raced grant is unlocked and the handle closed without releasing another process's lock or weakening cross-process serialization. Package `CatchUp` / `Rebuild` keep a non-cancelled acquire. The writer-lock leaf and its ancestors are confined: symlink / reparse / junction targets are refused before create, open, or chmod, and rejection does not mutate an external target. Lookup of an unknown id does not synchronously rebuild the index; while catch-up is still behind, explain is **503** `index_unavailable` rather than a blocking full-history build. Catch-up enumerates only committed logical bytes after the stored `indexedOffset`. It does not re-scan the already indexed prefix and does not `COUNT(*)` the requests table on the schema-v2 steady-state path. `indexedRows` is maintained in the same transaction as unique insert-or-replace of `request_id`. `IndexedOffset` means every committed logical byte strictly before that offset was processed by this index version; skipped lines still advance it. Live `sourceSize` is the current committed ledger size. If later committed bytes exist after the snapshot, `caughtUp` is false and `pendingBytes` is the remaining suffix. If `indexedOffset` is ahead of the ledger, metadata is malformed, or the SQLite file cannot prove a safe incremental boundary, catch-up stops and `rebuildRequired` is set. Lookups then return unavailable rather than stale rows. There is no retention-aware offset rebase in this release.

`benes observe logs rebuild-index` reconstructs a fresh derived index from the complete logical ledger: legacy file, then sealed segments in sequence order, then committed active rows. Rebuild writes a unique temporary SQLite file without holding the writer lock for the full history scan, checkpoints it closed, then under the writer lock performs a bounded catch-up/revalidation against the current durable ledger logical size and installs over `routing-history.sqlite` with a replace that does not delete the previous file first. Generation comparison is authoritative against that ledger size: an installed checkpoint with `indexedOffset` ahead of the ledger is invalid and never skips a rebuild candidate; only a strictly newer checkpoint that is valid against the same authority may suppress install. Equal offset does not suppress an explicit rebuild, so rebuild restores row contents from the ledger even when schema and checkpoint metadata look healthy. A failed ledger scan or a failed install leaves the previous derived DB in place, including destination recovery sidecars (`-wal` / `-shm` / `-journal`). Destination sidecars are cleared only after replace succeeds so a failed install remains recoverable and a successful install cannot keep a stale journal. A physically corrupt or incompatible SQLite file is disposable. Catch-up creates schema v2 only for a genuinely empty/new SQLite with no user-owned tables (SQLite internals are ignored). An unrelated user table alone, or canonical schema-v2 plus an unexpected persistent user table, is rebuild-required; Catch-up does not mutate a foreign DB as fresh. An existing checkpoint with missing or incompatible `requests` / `schema_meta` shape is rebuild-required; Catch-up does not CREATE missing tables under the old offset. Explicit `rebuild-index` may replace a foreign derived DB because the package owns `$BENES_HOME/routing-history.sqlite`. Schema v2 is the strict canonical shape: declared TEXT columns, sole `request_id` primary key with binary uniqueness, no unexpected CHECK / COLLATE / triggers / extra uniqueness. Catch-up inserts with `ON CONFLICT(request_id) DO NOTHING` (then last-wins update) so only duplicate `request_id` values are suppressed; incompatible constraints fail closed without advancing `indexedOffset`. `indexedRows == 0` if and only if `requests` is empty. Rebuild and catch-up share the same row parser. Duplicate `requestId` values replace the stored row; `indexedRows` is the number of unique request ids currently in the index.

If SQLite indexing fails after a successful ledger append, the model request still succeeds, the ledger row remains, and a later catch-up indexes the missed suffix.

`GET /api/usage` and `benes observe usage` still use a bounded **read window**, not bounded storage. The reader snapshots **committed** logical sizes first and does not chase a growing EOF. It reads at most the newest 64 MiB of that logical byte stream. An unterminated suffix in `active.jsonl` or the legacy file contributes zero logical bytes and does not consume the window; the read path does not rewrite those files. Writer-produced sealed segments stay immutable.

- If the logical snapshot is within the bound, the whole ledger is read.
- If it is larger, the reader starts near `logicalSize - bound`, discards only a partial leading record caused by that seek, and keeps complete rows through the snapshot end. Newest retained history is kept. The oldest prefix is omitted.
- Each physical file is scanned independently, so an incomplete tail in the legacy file or one sealed segment cannot merge into the next file.
- `LogicalBytes` and request-history `sourceSize` / `indexedOffset` use the same committed-byte definition. A newline-terminated malformed row still occupies logical bytes even when aggregation skips it.

Additive completeness fields:

| Field | Meaning |
| --- | --- |
| `historyTruncated` | True when older absolute logical bytes were excluded by the 64 MiB read window and/or by durable retention (`retainedFromOffset > 0`) |
| `truncatedPrefixBytes` | When truncated, the exact **absolute logical ledger** byte offset where the first potentially retained complete record begins (max of durable `retainedFromOffset` and the read-window start, including any discarded partial leading row through its delimiter). Uncommitted suffixes are not part of this offset. `0` when the snapshot is complete. It is not a path inside one physical file |
| `snapshotWindowStart` / `snapshotWindowEnd` | Min/max timestamps of **successfully parsed retained rows**, before range/surface filters. Null when no retained row parsed |

The API does not emit an `entriesDropped` count. That number is not knowable without scanning the skipped prefix. A bounded snapshot is not complete historical truth.

A persisted Usage record is committed only by a trailing newline. Complete JSON at a file end without that delimiter is an uncommitted tail and is ignored. Blank, malformed, and semantically invalid rows (`timestamp` missing, zero, or negative) are skipped. An oversized row is skipped without allocating from its untrusted length; later newline-terminated valid rows in the window remain visible. Admitted rows may have empty provider/model. Historical rows with a valid timestamp still count when `surface`, `usageStatus`, or `usage` are missing.

`range=today` (and other windows) still see current retained rows when old history pushed the ledger over the bound. Completeness metadata describes the read, not the query.

A read failure is **200** with `"error":"read_failed"`, empty totals, `historyTruncated: false`, and null snapshot timestamps. That is not a truncated-history claim. `range`, `since`, `until`, and day-window metadata still describe the validated requested query, including custom start/end instants and timezone.

## Snapshots

One Usage response is one read:

- usage ledger snapshot at time A (fixed logical size/end before parse)
- pricing overlay snapshot at time B (loaded at most once for that request)

They are not required to share a clock tick. Both are stable for the duration of that observability request.

`HEAD /api/usage` stays **200** with `Content-Type: application/json` and no body. Query validation matches GET.
