---
title: Dashboard
description: The React UI served from the same port as the data plane.
---

The dashboard is the Vite app under `gui/`, packaged as `gui/dist` and served by `internal/server` on `GET /`.

```bash
benes gui
```

That command starts the data plane if it is not already running, then opens the browser.

## What it is for

- Add, test, and remove providers (`#providers`)
- ChatGPT / Codex account pool and quota refresh. Confirming a reset-credit redeem sends one `redeemRequestId` for that click.
- Model visibility, custom models, effort caps
- Per-provider **Preset | All | Custom** on the Models catalog (OpenRouter ships a curated seed; Custom is a state from editing the selection, not a mode you pick)
- **New models**, **Default context cap**, and **Apply to existing routed providers** on the Models catalog (flat strip under the toolbar; no overflow menu). Visibility and settings saves stay immediate; the catalog reload waits 5 seconds after the last change so a burst of edits becomes one sync.
- Combo, routing, and compatibility edits on **Routing** (`#startup/routing`, `#startup/compatibility`, `#startup/combos`). Combos and profiles persist through `/api/combos` and `/api/routing-profiles`. Compatibility shows two evidence layers: **protocol** (`CLAIMED` until `benes lab rebuild`, then stored local-run verdicts from `~/.benes/lab/events.jsonl`) and **live-route** (derived adapter+vision on every GET). Community stays empty. Profile gates still use `compat.Classify`, not the Lab disk.
- Sidecar, shadow-call, context-projection (`contextProjection.mode`), and Agent Fabric controls on **Control** (`#startup`). Context projection is Responses-only. Fabric uses loopback `GET`/`PUT /api/fabric-settings` and applies without a listener restart.
- **API** (`#api`) under System: generated gateway keys, Client exports, live endpoints plus the server auth matrix, the callable model catalog, and curl examples. Client apply/disable lives on **Harnesses** (`#harnesses`), not here. `#integrations/keys` redirects to `#api`.
- **Harnesses** (`#harnesses`): apply/disable client wiring, and open allowlisted config/log paths in the OS file manager via loopback `POST /api/harnesses/reveal` (`target`: `config` | `log`). Shift+click copies the path; open failures toast and copy the path. Claude Code settings live here under **Claude → Settings** (auth mode, automatic extended context and compaction, Benes subagent exposure, helper model, and Opus/Sonnet/Haiku/Fable routing). Launch with `benes claude`. See [Other clients](../clients/).
- **Tasks** (`#tasks`) appears under Observation only when `fabric.enabled` is true. Task start/close are kernel lifecycle events, not model calls. The board can show task state and the kernel event timeline. It cannot show model progress, workers, or execution output — Fabric v1 does not supply those facts.
- **Diagnostics** (`#logs`): in-memory request inspector (`GET /api/diagnostics/requests`) with a Requests | Debug tab. Requests is master/detail telemetry (no request/response bodies). Debug is opt-in capture (provider / usage / injection streams and Claude inbound). `GET /api/logs` remains the legacy array projection.
- **Usage** (`#usage`): Overview, Breakdown (Models / Providers / Accounts), and Coverage on `GET /api/usage`. Surface and range filters sit top-right (Today / Yesterday / 7d / 30d / Available history / Custom; reversed custom ranges fail closed). List-price uses structured `cost`, not a fabricated unified total. The on-disk file is unbounded; when it exceeds the 64 MiB Usage read window, the report is the newest retained window, not full history (see [Usage](../../reference/usage/)).
- Durable session history on **Sessions** (`#sessions`): namespace, protocol, usage coverage, and routing aggregates from `GET /api/sessions`. Request-level inspection is **Diagnostics** (`#logs?sessionId=`).
- **Storage** (`#storage`): `CODEX_HOME` report, archived-session cleanup (quarantine to `.trash` or permanent delete), restore, and optional cleanup policy. Active sessions are never candidates. Contract: [Storage](../../reference/storage/).
- The sidebar's GitHub/update row: star state plus the live update badge (`GET /api/update/badge`). Benes does not self-update in-process, so clicking the orb opens the repository's releases — update through your package manager or rebuild from source.

Most of those surfaces also exist as CLI verbs that hit the same `/api/*` routes. Headless servers can skip the UI entirely.

## Auth on the UI

Loopback (`127.0.0.1` / `::1`) needs no token for the dashboard or the data plane. Binding `"hostname": "0.0.0.0"` refuses to start unless `BENES_API_AUTH_TOKEN` is set; every remote data-plane request then carries `x-benes-api-key` (a generated `benes_` key or that env token).

### API board (`#api`)

**Endpoints** lists the live base URL and protocol paths, plus the **Server auth matrix**. That matrix is not documentation invented by the React app: `GET /api/keys` returns `authMatrix` from the running listener’s current admission policy (`Required` / `Accepted` / `Rejected` per header). Switch bind mode or token setup and the cells change on the next refresh.

Use the base URL with OpenAI-compatible clients; Responses, Chat Completions, Messages, and Models are under `/v1`. Client apply/disable still lives on **Harnesses**, not here.
