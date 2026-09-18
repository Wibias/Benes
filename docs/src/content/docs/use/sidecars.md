---
title: Sidecars
description: Optional web-search and vision helpers for models that lack those tools.
---

Sidecars are opt-in packages:

- `internal/sidecar/websearch`
- `internal/sidecar/vision`
- `internal/sidecar/fabric` (task kernel; off unless `fabric.enabled` is true)

They run as extra work next to a routed completion — for example describing an image before a text-only upstream sees it, or answering a `web_search` tool call through a configured search backend.

Cache-preserving context projection is not a sidecar. Set `contextProjection.mode` in `config.json` (`off` by default) or from Control (`#startup`) via loopback `GET`/`PUT /api/context-projection`. `shadow` records metrics only. `duplicate` collapses repeated tool text. `recovery` (also `on`) adds exact head/tail receipts and a hidden `__benes_context_v1` recovery loop that never failovers to another combo member. Projection applies to Responses only. `BENES_CONTEXT_PROJECTION_EMERGENCY_DISABLE=1` skips it.

```bash
benes agent sidecar
```

Config objects: `webSearchSidecar` and `visionSidecar` in `~/.benes/config.json`. The dashboard `/api/sidecar-settings` route edits the same fields. Those two root objects stay the single authority for sidecar configuration — model and backend selection, streaming, timeouts, per-turn limits, and each modality's default activation. A per-Harness choice changes activation only and never duplicates or overrides them. The nested owners `claudeCode.webSearchSidecar` and `claudeCode.visionSidecar` are retired: Benes does not read, accept, or project them, the Claude Settings surface has no sidecar fields, and a config that still carries those blocks keeps them on disk untouched and unused.

`webSearch.enabled` and `vision.enabled` default to on when the key is omitted. PUT `true` deletes the key; PUT `false` stores `enabled: false`. A partial PUT of another field must not drop that flag. The same `webSearch.enabled` value gates hosted search on completions **and** Codex `POST /v1/alpha/search`. There is no second alpha-search toggle.

Codex `web_search = "live"` sends `POST /v1/alpha/search` to the listener. Benes uses the configured eligible search backend (dedicated search JSON, Anthropic hosted search, or a native ChatGPT Codex relay when that is the selected supported backend). It does not require ChatGPT OAuth when another eligible backend is configured, and it does not invent an unconfigured paid backend. A missing backend or a backend failure returns a backend-specific error, not a ChatGPT-login demand.

Sidecars stay off the core path until enabled. They are not a substitute for a provider that already implements search or vision natively.

## Per-Harness sidecar policy

The Harnesses board (`#harnesses`) shows a **Sidecar policy** section on the detail view of a Harness that Benes can stamp identity for. Web search and vision are independent modalities, and each offers the same three choices:

- **Use global** — live inheritance. The Harness stores no value of its own; every request re-reads the current global activation, so changing the global switch moves an inheriting Harness on the next request without the Harness setting being saved again. Clearing an override returns to this state.
- **On** — an explicit activation override for this Harness, including when the global default is off.
- **Off** — an explicit deactivation for this Harness, including when the global default is on.

The two modalities never move together: setting web search neither sends nor clears the vision override, and the reverse holds as well. Each row also shows the configured-effective result the server computed, for example `Configured: On · Harness override` or `Configured: Off · Global`.

Overrides persist in `~/.benes/harnesses.json` on the Harness row (`sidecars.webSearch` and `sidecars.vision`, values `enabled` or `disabled`; an absent field means inherit) and are written through the existing partial settings update:

```json
{ "clientId": "opencode", "sidecars": { "webSearch": "enabled" } }
```

Only the modality being changed is sent. An omitted modality keeps its current override, and `null` clears that modality back to inheritance — omission and `null` are different requests. The server owns this state end to end: the dashboard renders the canonical response and refetches it after every save, and none of the policy is stored in the browser.

### Eligible Harnesses

Only a Harness with a proven supported integration gets editable controls; being listed on the board is not enough. Benes configures its managed integrations to stamp explicit Harness identity (`X-Benes-Harness: <known-harness-id>`), and the server-owned registry decides which Harnesses qualify. Identity is never inferred from the User-Agent, the endpoint, the model, the provider, or the Harness display name, and Benes strips the header before any provider dispatch. Claude Code is not one of the eligible integrations, which is also why its Settings surface has no sidecar controls.

An integration configured before this feature can look stale or update-needed until the normal apply or refresh regenerates its managed configuration with the current identity marker. Reapply the Harness from the board so Benes rewrites the managed configuration; do not hand-write the header or edit generated files.

### Configured is not the same as executed

Four things stay distinct, and only the last one means the sidecar did work for a request:

1. the persisted Harness override in `harnesses.json`;
2. the configured-effective activation the server projects;
3. the per-request resolution, which also weighs proven native capability;
4. actual sidecar execution for that request.

A modality configured **On** therefore does not run on every request. A request that carries no accepted Harness identity follows the global defaults, and when the selected provider and request path prove native capability — the upstream already searches, or already reads the image — the native path is selected and no sidecar runs. An override can only choose among proven runtime candidates: if the global sidecar configuration cannot produce one, **On** does not manufacture a backend. The request's usage record reports the outcome as `sidecarPolicy`, with the identity status, the configured state, the resolution, and a `ran` flag that is set only where the sidecar is invoked.

### Vision stays inside the request

Vision describes image parts that are already in the request as inline `data:image/…;base64` payloads and replaces them with text before a text-only upstream sees them. It does not fetch remote image URLs: an `https://` image part is rejected instead of downloaded. Image count, per-image bytes, timeout, and descriptions per turn stay bounded by the global `visionSidecar` settings.

Agent Fabric is a lifecycle coordination sidecar with an optional fenced execute path. Toggle `fabric.enabled` from Control (`#startup`) via loopback `GET`/`PUT /api/fabric-settings` (live, no restart). Enabling it launches nothing. Task create/start/close/delete append kernel events only; they do not call a model, spawn a worker, or connect Codex JSON-RPC, the Claude SDK, or A2A. Close is terminal task closure, not worker termination. Distinct `POST /api/fabric/tasks/:id/execute` starts a fenced primary run that reuses the ordinary Benes data-plane (admit/route/bridge/providers/usage). Fabric never shells out, never dials providers from the fabric package, and never loops back through Fabric HTTP. Cancel cancels the live runtime for the current fence; stale cancels are rejected. Orphaned `RunStarted` events become `Interrupted`/`lost` on reopen with no auto-replay. Events and leases stay metadata-only (no prompts/transcripts/full outputs). Completed output is available only via a bounded in-memory result handle (`resultHandle` / `GET .../runs/:runId/result`); it expires by TTL and does not survive restart. Execute workers use a server-owned root context (not the HTTP request context) after 202. HTTP start/close `owner` is audit-only and does not become fencing ownership. Tasks survive a Benes restart as JSONL under `~/.benes/fabric` and are not resumed automatically. Ordinary events are capped with reserved slots for close/fail/cancel/remove; pre-cap histories that already exceed the new limit still get those slots. Oldest removed histories are reclaimed when the store is full; live tasks are not. While Fabric is disabled, task routes return **409** `fabric_disabled` rather than an empty list. Corrupt config is **500** `config_unreadable`, not disabled.
