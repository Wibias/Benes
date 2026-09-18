---
title: Sessions
description: Durable session-level observability for later dashboard history.
---

Benes **Sessions v1** is durable conversation-level observability. It is not the Diagnostics request log. The dashboard Sessions page (`#sessions`) is a master/detail workspace over loopback `GET /api/sessions`, `GET /api/sessions/:id`, and `GET /api/sessions/filters`. Request-level inspection stays on Diagnostics (`GET /api/logs?sessionId=`).

A session is a set of **admitted** inbound model requests that share a **proven** conversation identity. Admission-denied traffic can still appear in Diagnostics / request logs. It does not create a durable session, increment a request count, write a request row, create a response continuation link, or change activity timestamps. An admitted request with a proven identity is still recorded if it later fails during parse, routing, or provider open.

Benes never groups by time, model, provider, source IP, similar prompts, or request order.

## Identity

Canonical key: `{namespace, externalId}` plus a Benes-assigned `ses_*` id.

| Surface | Identity | Namespace |
| --- | --- | --- |
| Codex / Responses only | `x-codex-parent-thread-id` | `codex/parent-thread` |
| Codex / Responses only | `thread-id` | `codex/thread` |
| Any inbound protocol | `session-id` / `session_id` | `{protocol}/session` |
| Grok inbound (Chat Completions and Responses, not Anthropic) | `x-grok-conv-id` / `x-grok-session-id` (client-supplied inbound only) | `grok/conversation` |
| Chat Completions `metadata` only | `conversation_id`, then `session_id`, then `thread_id`. Responses and Anthropic `metadata` are not session identity. Anthropic `user_id` is never identity. | `chat_completions/metadata/{key}` |
| Responses continuation | known `previous_response_id` | same session via `response_links` |
| Responses unknown `previous_response_id` | seeds `responses/previous_response` with that id | `responses/previous_response` |
| Responses success, no inbound id, trustworthy outgoing response id | first-turn root | `responses/chain` |
| Kiro stream state | `conversationId` on provider state key `kiro` only | `kiro/conversation` |

Priority when several identifiers are present: parent thread, then `thread-id`, then session headers, then Grok, then metadata. Conflicting identifiers are not merged.

Not used: `user`, `prompt_cache_key`, first-user-text hashes, `x-codex-window-id`, installation ids, timestamps, model names.

Chat Completions and Anthropic Messages with none of the identifiers above stay **ungrouped**. They may still appear in the in-memory Diagnostics ring (`GET /api/logs`, `GET /api/diagnostics/requests`). They do not become Sessions. A Chat Completions or Anthropic caller sending `thread-id` does not join a Codex `codex/thread` session.

A `/v1/responses` request with no proven inbound identity, no resolvable `previous_response_id`, and no trustworthy outgoing response id does not create a session. Identified failed requests still record into that session.

Inbound paths that are not Responses, Chat Completions, or Anthropic Messages (models, images, compact, search, health) are not recorded as session requests.

Malformed identifiers (empty, control characters, longer than 256 runes) are treated as missing.

Dashboard Storage's Codex `sessions/` bucket is Codex's own files under `CODEX_HOME`. It is not `$BENES_HOME/sessions.sqlite`. Archived cleanup only touches `archived_sessions/`. See [Storage](../storage/).

## What is persisted

SQLite at `$BENES_HOME/sessions.sqlite` (WAL, transactions, schema version 1). Fresh databases initialize schema v1. Version 1 is used as-is. An unsupported future schema version fails store open without rewriting the file. Opening the store must not fail listener start; a persist error does not fail the model request. If the file cannot be opened, model serving continues and loopback `GET /api/sessions` returns **503** `store_unavailable` instead of an empty list. A missing Benes home (no store attached) still returns a healthy empty list. A malformed historical row is skipped on read and does not strand later rows across a page boundary. Ordering timestamps must be SQLite INTEGER Unix milliseconds; REAL and TEXT timestamps are excluded from list/detail queries and cleaned up with expired sessions. Other malformed numeric fields are skipped after the ordering key is advanced; they are not coerced into zeros or timestamps.

Each grouped request stores metadata only:

- durable request id (`req_*`, assigned by Benes), optional `correlationId` copied from inbound `X-Request-ID` when present, timestamps, protocol, method, path, HTTP status, duration, byte sizes when already counted. HTTP status is the final observed wire status: a panic before any response is stored as 500; a panic after a committed status keeps that committed status. Diagnostics uses the same rule for the same `req_*` row.
- original requested model, resolved model, selected provider, combo/policy ids, committed member
- failover attempts as events on that one request (not extra session requests)
- input / cached input / output / total tokens when the provider reported **non-estimated** usage for that request
- cost and `USD` only when the existing pricing table returns **exact** confidence for that provider/model and those tokens

Not persisted: prompts, responses, Authorization headers, API keys, OAuth tokens, provider secrets.

## Retention

Default retention is **30 days** from `lastActivityAt`. Expired sessions are pruned transactionally on store open, on writes, and opportunistically (bounded) on list/detail reads using the store clock. List and detail never return a session after `lastActivityAt + retention`, even if no later model request and without a process restart. Related request rows and response links are deleted with that session. In-flight request rows are kept. Follow-up work can expose this in Storage settings; v1 does not add a settings UI.

## APIs

Loopback only, like other management routes.

`GET /api/sessions?limit=&cursor=&q=&namespace=&protocol=&provider=&model=&policy=&combo=` — summaries ordered by last activity (newest first). Each summary includes `protocols`: the distinct known request protocols on that session (`responses`, `chat_completions`, `anthropic_messages`), independent of the current page of request details. Search `q` is case-insensitive over the durable `ses_*` id, `externalId`, and `namespace`. Whitespace-only `q` is ignored. `protocol` must be `responses`, `chat_completions`, or `anthropic_messages`; anything else is **400** `invalid_protocol`. `provider`, `model`, `policy`, and `combo` match committed/selected fields on **structurally trustworthy** request rows through indexed `EXISTS` queries (`model` is `resolved_model`, not a requested `policy/` or `combo/` alias). Cursors are stateless keyset values; the client must repeat the same `q` and filter parameters on later pages.

`GET /api/sessions/filters` — currently present durable values for those filters (`protocols`, `namespaces`, `providers`, `models`, `policyIds`, `comboIds`). Expired sessions and malformed historical request rows are omitted. `protocols` only lists values the list API accepts. Values are canonical ids, not display labels.

`GET /api/sessions/:id?limit=&cursor=` — session metadata, **whole-session aggregates**, and a paginated request page in chronological order. Each request `id` is the durable `req_*` key. `correlationId` is present only when the inbound `X-Request-ID` was stored. `limit` only changes the request page; aggregates always cover every retained request in that session.

Cursors are opaque `lastActivityAtMs:id` / `startedAtMs:id` keyset values.

### Aggregates

`aggregates` is computed in SQLite, not from the returned request page.

- `models` / `providers` — deterministic unique **resolved/committed** model and provider ids. Requested aliases such as `policy/foo` or `combo/bar` are not listed as models.
- `policyIds` / `comboIds` — every distinct policy or combo id observed on the session. A session may have none, one, or several.
- `protocols` — distinct known request protocols on trustworthy rows.
- `failoverRequestCount` — number of inbound session requests whose stored attempts include at least one `decision: "hop"` (combo/policy walker advanced past a failed/rejected candidate). A single successful `committed` attempt does not count. One request with several hops still counts once. `attempts_json` that is missing, empty, invalid JSON, not an array, or an array without an object `decision: "hop"` does not count and does not fail the session detail. `hadFailover` is `failoverRequestCount > 0`.
- `usage` — per-field coverage. Each of `inputTokens`, `cachedInputTokens`, `outputTokens`, `totalTokens`, and `cost` has `value` (omitted when nothing exact was stored), `attributedRequests`, `totalRequests`, and `complete`. Missing or estimated usage is not stored as zero. `complete` is true only when every request row contributed an exact value for that field. `cost` is currency-qualified: a numeric cost counts only with a valid non-empty currency, and that amount belongs only to that currency. Missing or corrupt currency does not inherit `USD` from other rows. If more than one attributed currency is present, `value` is omitted, `complete` is false, and `currencies` lists the distinct codes. Malformed historical rows are skipped as contributions and make `complete` false. Aggregates and list filters use the same structural trust rule as request-history decoding (`scanRequestRow`): a wrong-typed required field such as `started_at` or `status` excludes the whole row.

There is no persisted client/surface field. Namespace on the session summary is the trustworthy higher-level identity; the API does not invent Codex/Claude Code/Web UI labels.

### Sessions vs Diagnostics

Sessions is durable, proven conversation grouping, session-level aggregates, and 30-day history.

Diagnostics stays a 2000-entry in-process ring. It includes ungrouped and admission-denied traffic. When an admitted request resolved to a durable session, the matching ring entry carries `sessionId`. `GET /api/logs?sessionId=ses_...` and `GET /api/diagnostics/requests?sessionId=ses_...` return only those still-retained ring entries. A malformed id is **400** `invalid_session_id`. A valid id with no retained matches is **200** `[]` (legacy logs) or `{requests:[], ...}` (structured). See [Diagnostics](../diagnostics/).

A durable session can outlive its Diagnostics rows. `View requests in Diagnostics` means: show matching requests still in the current ring — not reconstruct the whole 30-day session history. Persist failure must not suppress Diagnostics; those entries simply have no `sessionId`. Diagnostics request telemetry is not made durable.

## Restart

The store is the source of truth on disk. Reopening the same `sessions.sqlite` after process exit returns the same sessions and request history.
