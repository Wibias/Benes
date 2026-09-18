---
title: How traffic flows
description: What the Go listener accepts and how a request reaches a provider.
---

Benes is a **local** HTTP process. Clients stay unmodified. They call loopback; Benes translates, selects a provider, and streams the answer back.

```text
Codex / Claude Code / other client
        |
        |  HTTP on 127.0.0.1:23100
        v
internal/server
        |
        |  internal/router picks provider + model
        v
internal/providers/...
        |
        v
upstream API
```

## Inbound shapes

The same listener answers:

| Path | Shape |
| --- | --- |
| `POST /v1/responses` | OpenAI Responses (Codex’s primary wire) |
| `POST /v1/messages` | Anthropic Messages (Claude Code, plus a native Anthropic passthrough) |
| `POST /v1/chat/completions` | OpenAI Chat Completions |
| `POST /v1/images/generations` | Codex built-in image_gen JSON relay |
| `POST /v1/images/edits` | Codex image edits (JSON or multipart) |
| `GET /v1/models` | Catalog the client can list |
| `GET /v1/catalog` | Codex-oriented catalog payload |
| `GET /healthz` | Liveness |
| `GET /readyz` | Readiness |

Streaming is HTTP/SSE through `internal/responses/bridge`. That bridge is also how a sparse custom Responses gateway still commits in Codex: Benes reconstructs the lifecycle instead of relaying incomplete snapshots. WebSocket upgrades are off unless you turn them on; with the flag off the server answers **426**.

## Selection

`internal/router` is selection-only. `benes start` executes no Lab code and does not enable Fabric.

Send `provider/model`. The first `/` splits the provider from the model, so inner slashes in an upstream id stay on the model (`openrouter/anthropic/claude-opus-5`). Combos use the `combo/<id>` namespace and walk targets in order until one succeeds (failover only). Routing profiles use `policy/<id>`: require and compatibility skip ineligible members, optimization weights reorder what remains, a thread stays on the committed member, then the same failover walker runs. Dashboard PUT/DELETE on `#startup/combos` and `#startup/routing` persist into `config.json` and hot-reload combo/policy traffic and `/v1/models` rows without a restart.

Optional aliases rewrite the selector **before** that split is sent upstream. `benes alias set google-antigravity agy` stores `providers.google-antigravity.alias`. `benes alias set openrouter/anthropic/claude-opus-5 opus` stores a provider-scoped model alias keyed by the canonical model id so a catalog refresh does not drop it. Clients can then send `agy/opus`. A bare model alias such as `opus` resolves only when it is unique across providers; if two providers share it, the listener answers **400** with the candidate list instead of picking the first match. Canonical ids keep working. There are no built-in aliases.

`benes lab rebuild` writes `~/.benes/lab/events.jsonl` from `config.json` (classifier only, no provider HTTP). `benes lab` without a subcommand exits 2 with usage. The dashboard Compatibility tab merges that ledger into `GET /api/lab/*`: the protocol column is `CLAIMED` until a rebuild; after rebuild it shows stored local-run verdicts. The live-route column stays derived adapter+vision. Community stays empty. Profile gates still use `compat.Classify`, not Lab disk.

Provider-visible tool results can be projected with `contextProjection.mode` (`off` default, `shadow`, `duplicate`, `recovery`). Canonical Responses history stays exact. Native Codex passthrough is never projected. Chat Completions and Anthropic Messages are not projected. Set the mode from Control (`#startup`) or `config.json`. Set `BENES_CONTEXT_PROJECTION_EMERGENCY_DISABLE=1` to skip projection.

Agent Fabric v1 is an opt-in lifecycle sidecar (`fabric.enabled`, default false). Control toggles it with loopback `GET`/`PUT /api/fabric-settings` without a restart and without starting Codex, Claude, Grok, or a worker. The Tasks nav (`#tasks`) appears only when Fabric is enabled. Create/start/close/delete are kernel events, not model calls. Close is terminal task closure. Disabled task routes return **409** `fabric_disabled`. Unreadable config is **500** `config_unreadable`. Task JSONL under `~/.benes/fabric` survives restart and is not auto-resumed.

Live routing still uses the process that was started — restart the listener after writing provider aliases, the same as other provider config. Combo and routing-profile mutations from the dashboard apply to the running combo/policy maps and synthesized `/v1/models` rows without that restart.

## Persistence

| Location | Role |
| --- | --- |
| `~/.benes/config.json` | Providers, routing, sidecar flags, combos |
| `~/.benes/auth.json` | OAuth and key material (mode 0600) |
| `~/.benes/codex-accounts.json` | ChatGPT / Codex account pool |
| `CODEX_HOME` | Injected `config.toml`, catalog, cache — only after `init` / `sync` |

`benes stop` / `restore` put Codex back. Deleting `~/.benes` does **not** undo Codex writes by itself.
