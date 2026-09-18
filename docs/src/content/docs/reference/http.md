---
title: HTTP
description: Data-plane paths and the dashboard management API.
---

Listener: `internal/server`. Default `127.0.0.1:23100`.

## Data plane

| Method | Path | Owner |
| --- | --- | --- |
| POST | `/v1/responses` | `internal/server/responses.go`, stream via `internal/responses` |
| POST | `/v1/alpha/search` | Codex live search; `internal/server/alpha_search.go` |
| POST | `/v1/messages` | `internal/server/anthropic.go` |
| POST | `/v1/chat/completions` | `internal/server/chat.go` |
| POST | `/v1/images/generations` | Codex built-in image_gen relay; `internal/server/images.go` |
| POST | `/v1/images/edits` | Codex image edits (JSON or multipart); `internal/server/images.go` |
| GET | `/v1/models` | catalog |
| GET | `/v1/catalog` | catalog |
| GET | `/healthz` | process liveness |
| GET | `/readyz` | post-sync readiness JSON |

### `/v1/models` capability metadata

`GET /v1/models` keeps the OpenAI-compatible list shape and adds Benes capability metadata from the same effective catalog state used by routing and admission. Existing clients can ignore the additive fields.

Each model row can include top-level `api_types` when Benes has an audited runtime contract for the selected provider path. Values are the inbound shapes that the row can be routed from: `responses`, `chat_completions`, and `anthropic_messages`. If that contract is unknown, `api_types` is omitted instead of inferred from the model or provider name.

The row's `capabilities` object contains:

| Field | Meaning |
| --- | --- |
| `context_length` | Effective context window after configured overrides and caps. Live discovery may fill this from provider `/models` rows, including nested `capabilities.limits.max_context_window_tokens` (GitHub Copilot). `max_prompt_tokens` below that window becomes the standard input ceiling, not the total window. Invalid or missing values stay unknown and use the conservative 128k fallback unless an operator override or provider cap applies. Nested Copilot windows are not rewritten by the native GPT-5.6 1.05M lift. |
| `output_modalities` | Currently `["text"]` for ordinary routed catalog rows. Image generation uses the separate `/v1/images/*` routes. |
| `supports_tool_use` | Effective tool-use support when known. |
| `supports_streaming` | Effective streaming support when known. |
| `supports_reasoning` | Effective reasoning support when known. |
| `supports_vision` | Effective image-input support when known. |
| `input_modalities` | `["text", "image"]` when vision is known true, or `["text"]` when known false. |
| `reasoning_effort` | The effective supported reasoning ladder when reasoning is known true and the catalog has authoritative rungs. |

Capability booleans are tri-state internally. Unknown values are omitted from the JSON rather than serialized as `false`; `input_modalities` is likewise omitted when vision support is unknown, and `reasoning_effort` is omitted unless reasoning support is known true. Provider-discovered metadata may fill an unknown value, but an explicit stricter configured/operator value can narrow it. Combo rows use the conservative intersection of their executable targets, so they do not advertise a capability that a valid failover member cannot honor.

The user-facing logical OpenAI provider has one model identity per selected access lane. When API access is selected, an internal `openai-apikey/<model>` row is exposed as `openai/<model>` and the alternate OpenAI lane is omitted, preserving the selected lane's effective capabilities without publishing a duplicate model identity.

Codex's built-in `image_gen` extension POSTs `/v1/images/generations` and `/v1/images/edits` relative to the injected proxy base URL. Benes relays those requests to a proven OpenAI image-capable upstream (`api.openai.com` API-key or native ChatGPT Codex forward). Loopback or remote admission credentials are never sent as provider auth. Text-only providers and missing image upstreams return 400, not a retryable 5xx. Image bodies use a dedicated 64 MiB ceiling (50 MiB per multipart part) and a 5 minute upstream timeout; remote `image_url` values obey the outbound destination policy. Unrelated unknown `/v1/*` paths, including `/v1/images/variations`, stay 404.

`/healthz` answers `{"ok":true}`; `/readyz` answers `{"ok":true,"status":"ready"}`. Both are unauthenticated probes of the listener itself and publish nothing else — no version, uptime, pid, or port. Anything that wants to present the running build reads its own version.

## Management

`/api/*` backs the dashboard. A sample of owners:

| Area | File |
| --- | --- |
| Storage | `internal/server/storage_api.go` (loopback `GET /api/storage`; cleanup preview/execute, trash, restore, cleanup-policy). Contract: [Storage](../storage/). |
| Usage | `internal/server/usage_api.go` (`GET /api/usage`, loopback) |
| Request history | `internal/server/request_history_api.go` (loopback `GET /api/request-history/:id/route-decision` from the derived request-history index of the durable usage ledger) |
| Sessions | `internal/server/sessions_api.go` (`GET /api/sessions`, `GET /api/sessions/filters`, `GET /api/sessions/:id`, loopback) |
| Diagnostics | `internal/server/diagnostics_api.go` (loopback `GET /api/diagnostics/requests`, `GET /api/diagnostics/requests/:requestId`). Legacy `GET /api/logs` stays a compatibility projection of the same 2000-entry in-memory ring. |
| Dashboard API keys | `internal/server/keys_api.go` (loopback `GET`/`POST`/`DELETE`/`PATCH /api/keys`; same `apiKeys` config the CLI `benes access key` writes. GET never returns the secret. `authMatrix` is the live data-plane admission policy — Required / Accepted / Rejected per header for `/v1/*` — not GUI copy. Loopback accepts bearer, `x-benes-api-key`, and `x-api-key`; a remote bind with `BENES_API_AUTH_TOKEN` requires `x-benes-api-key`.) |
| Config | `internal/server/config_api.go` (`GET /api/config`, loopback `GET /api/config/mutations`) |
| Combos | `internal/server/combos_api.go` (`GET`/`PUT`/`DELETE /api/combos`, loopback) |
| Routing profiles | `internal/server/routing_profiles_api.go` (`GET`/`PUT`/`DELETE /api/routing-profiles`, dry-run, `/api/routing-analytics` from in-process `policy/<id>` walks; empty traffic stays empty) |
| Compatibility matrix | `internal/server/lab_api.go` (read-only `GET /api/lab/*`; merges derived live-route adapter+vision with protocol overlay from `~/.benes/lab/events.jsonl` after `benes lab rebuild`; `GET /api/lab/observations` and `GET /api/lab/events/<id>` serve ledger rows; community stays empty) |
| Context projection | `internal/server/context_projection_api.go` (loopback `GET`/`PUT /api/context-projection` `{mode}`; Control board) |
| Agent Fabric | `internal/server/fabric_api.go` (loopback `GET /api/fabric/status` `{enabled, kind:"lifecycle", capabilities:["lifecycle","execution","single_child_handoff"], schemaVersion}` when config is readable; corrupt/unreadable config is **500** `config_unreadable`, not disabled. Other `/api/fabric/tasks*` require `fabric.enabled` or return **409** `fabric_disabled`. Create/start/close/delete are kernel lifecycle marks, not model calls. Distinct `POST /api/fabric/tasks/:id/execute` runs a fenced data-plane turn under a server-owned worker context (metadata-only events; no Fabric fan-out/retries/shell). Opt-in `delegation.model` enables exactly one reserved `__benes_fabric_delegate_v1` child handoff (ChildRun* events; three `req_*` via three `runModelTurn` calls; structured tool-result continuation — never prompt paste). 202 includes `resultHandle`; `GET /api/fabric/tasks/:id/runs/:runId/result` returns bounded completed output from an in-memory store (TTL/evict; no restart survival). Close is terminal. Invalid transitions and operations on a removed task are **409** `invalid_transition`. Missing tasks are **404** `task_not_found`.) |
| Fabric settings | `internal/server/fabric_settings_api.go` (loopback `GET`/`PUT /api/fabric-settings` `{enabled}`; live, no restart; PUT writes only `fabric.enabled` and rejects trailing/malformed JSON) |
| Shadow calls | `internal/server/shadow_call_api.go` |
| Updates | `internal/server/update_api.go`. Self-update is retired: `GET /api/update/check`, `POST /api/update/run`, and `GET /api/update/status` answer **410** `self-update retired`. `GET /api/update/badge` stays live for the dashboard's update badge. Update through your package manager or rebuild from source. |
| Debug | `internal/server/debug_api.go` |
| OAuth accounts | `internal/server/oauth_accounts_api.go` |
| Codex auth pool | `internal/server/codex_auth_api.go` |
| Native integrations | `internal/server/native_integrations_api.go` |
| GitHub star | `internal/server/github_star_api.go` (`POST` → `403 consent_required` without a session) |
| Model presets | `internal/server/model_presets_api.go` (`GET`/`PUT /api/model-presets`, loopback) |
| Model discovery | `internal/server/model_discovery_api.go` (`GET`/`PUT`/`POST /api/model-discovery`, loopback). POST `{provider}` fetches the live list with stored credentials and writes `providers.<id>.models`, including nested Copilot context-window metadata when present. |
| Selected models | `internal/server/selected_models_api.go` (`GET`/`PUT /api/selected-models`, loopback) |
| Disabled models | `internal/server/models_api.go` (`PUT /api/disabled-models`, loopback; full replace of `disabledModels`) |

Loopback GET `/api/github/star` returns `{"starred":false,"unknown":true}`.

Loopback `GET|PUT /api/usage/retention` and `POST /api/usage/retention/preview` / `POST /api/usage/retention/run` manage the opt-in durable retention policy (`maxBytes`, `maxAgeMs`; both default disabled). Preview binds generation/sources/sizes/policy (stale → `stale_preview`). Run is ledger-first then request-history SQLite compaction under the writer lock. These routes are not under `/api/storage/cleanup`.

Loopback GET `/api/usage` aggregates the durable usage ledger (legacy `usage.jsonl` plus `$BENES_HOME/usage/` segments). Query: `range=today|yesterday|7d|30d|all`, `date=YYYY-MM-DD`, or custom `start`/`end`. Instants are date-only (`YYYY-MM-DD`), minute (`YYYY-MM-DDTHH:MM`), or RFC3339. The window is inclusive start, exclusive end, interpreted in `tz` (IANA) unless the value carries an offset. Optional `surface`, `provider`, `model`, and `account` filters compose with the window. Malformed, reversed (`end <= start`), one-sided custom, and windows longer than 366 days return **400** `invalid_range`. Missing files stay **200** with empty totals. A missing file is not an error; a read failure is **200** with `"error":"read_failed"`. Date-only custom `start=2026-08-20&end=2026-08-22` still means `[2026-08-20, 2026-08-22)` in the viewer timezone. Surface, provenance, cost confidence, unbounded segmented storage, the 64 MiB Usage read window, and privacy are in [Usage](../usage/). `surface=codex` matches explicit `codex` only; an empty historical surface is unattributed, not Codex. `account` filters the committed Codex log label, not client surface.

Loopback GET `/api/request-history/:id/route-decision` explains one admitted request from the derived request-history index of the durable usage ledger. The index schema is v3 (`ledger_offset` per request, `ledger_end_offset` / `retention_watermark` in meta). A v2 index is rebuilt once by the background indexer. The listener incrementally indexes newly committed ledger rows after a durable append on one serialized background worker. Listener start does not scan history. Lookup does not wait for that worker and does not synchronously rebuild history for an unknown id. A newly admitted request is explainable without `benes observe logs rebuild-index`. That command remains an explicit repair for a missing, corrupt, incompatible, or untrusted index. A healthy index that does not contain the id is **404** `not_found`. If the derived index is rebuild-required, corrupt, still catching up, or cannot establish freshness, the route is **503** `index_unavailable` and does not return stale SQLite rows or filesystem paths. An unknown request id does not trigger a synchronous full-history rebuild. The payload is `requestId`, `routeDecision`, `outcome.status`, and `summary.requestedModel` / `finalProvider` / `finalModel`. `benes observe logs explain <request-id>` is the CLI for this route. Denied traffic is not in the ledger and cannot be explained from it. A successful ledger append that races a catch-up snapshot is coalesced into a later fixed snapshot; do not treat explain as zero-lag. If a catch-up fails while the already-indexed prefix remains trustworthy, a lookup of an id already in that prefix still succeeds.

Loopback GET `/api/sessions` and `/api/sessions/:id` list durable session summaries and request history from `$BENES_HOME/sessions.sqlite`. This is not `/api/logs`. Only admitted model-request activity is persisted. Request `id` is a Benes-assigned `req_*` value; optional `correlationId` is inbound `X-Request-ID`. A healthy empty store is **200** with `sessions: []`. If persistence cannot be opened, list, detail, and `GET /api/sessions/filters` return **503** `store_unavailable` without filesystem paths. List summaries include `protocols` for the current page. List query `q` searches id/externalId/namespace. `protocol` is an enum (`responses`, `chat_completions`, `anthropic_messages`); unknown values are **400** `invalid_protocol`. Optional `namespace`, `provider`, `model`, `policy`, and `combo` filters compose with `q` and keyset `cursor` (repeat the same filters while paging). `GET /api/sessions/:id` includes whole-session `aggregates` independent of the request-page `limit`. Loopback GET `/api/logs?sessionId=ses_...` filters the in-memory Diagnostics ring to entries that still carry that durable session id; it does not read historical Sessions rows. Identity rules, retention, aggregates, and fields are in [Sessions](../sessions/). Structured request telemetry is additive loopback `GET /api/diagnostics/requests` and `GET /api/diagnostics/requests/:requestId` (opaque incremental `cursor`, exact filters including `sessionId`). It is the same bounded in-memory ring, not durable history. Contract, privacy boundary, timing, and provenance are in [Diagnostics](../diagnostics/).

`POST /api/codex-auth/reset-credits/consume` spends one ChatGPT/Codex banked reset. Body: `{ "accountId": "<id|__main__>", "redeemRequestId": "<uuid-v4>" }`. The listener forwards `redeemRequestId` as WHAM `redeem_request_id` and does not mint one. Dashboard confirm and `benes account reset-credits --consume --yes` each create that id once for the click or CLI invocation. A retry of the same operation must reuse it. Missing or non-v4 ids return 400 and do not call WHAM.

Admission / CORS: `internal/server/admission.go`, `internal/server/cors.go`.
