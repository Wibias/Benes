---
title: Config
description: ~/.benes/config.json and the environment that overrides it.
---

Durable config is `~/.benes/config.json` (or `$BENES_HOME/config.json`). Writes go through `internal/config` transactions (temp file, rename). Successful commits append a redacted, bounded row to `config-mutations.json` next to that file. Secrets are stripped before that log is written. No-op saves do not add a row. Newest-first reads: `benes config mutations` and loopback `GET /api/config/mutations`.

```bash
benes config show --json
benes config get hostname
benes config set defaultProvider '"anthropic"'
benes config mutations --limit 20
benes config validate
```

`import` requires `--yes`. Secret-looking keys (`apiKey`, `token`, …) are redacted in `show` unless you are looking at the raw file.

Per-provider `selectedModels` is an allowlist (empty/omitted = all visible). `modelPreset` records whether that list came from `preset`, `all`, or a later `custom` edit. Top-level `modelDiscovery` holds the opt-in new-arrival policy and known-id baseline. See [Model ids](../../use/model-ids/).

## Environment

| Variable | Effect |
| --- | --- |
| `BENES_HOME` | State root |
| `BENES_BIN` | Absolute Go CLI path for the npm launcher |
| `BENES_API_AUTH_TOKEN` | Required if `hostname` is not loopback |
| `BENES_CONTEXT_PROJECTION_EMERGENCY_DISABLE` | `1` skips provider-visible context projection |
| `CODEX_HOME` | Codex state root when set and valid |
| `CODEX_SQLITE_HOME` | Optional SQLite thread home (else `CODEX_HOME`) |

## Bind address

Default hostname is `127.0.0.1`. `"hostname": "0.0.0.0"` will not boot without `BENES_API_AUTH_TOKEN`. Clients then send `x-benes-api-key`. Constant: `BenesAPIAuthTokenEnv` in `internal/config/admission_env.go`.

## Anthropic Messages API keys

Key-auth Anthropic Messages endpoints use the native `anthropic` adapter (`anthropic-messages` is also accepted as the explicit protocol name). Benes normalizes the configured base URL to `/v1/messages`, uses the stored credential reference, and sends `anthropic-version: 2023-06-01`.

The default key transport is Anthropic's native `x-api-key`. Set `apiKeyTransport` to `bearer` only for an Anthropic-compatible gateway that requires `Authorization: Bearer …`. This adapter is key-auth only: it does not replace the separate Claude subscription/OAuth flow, and it does not accept an API-key pool.

```json
"anthropic-api": {
  "adapter": "anthropic",
  "baseUrl": "https://api.anthropic.com",
  "apiKey": "sk-ant-..."
}
```

For a compatible gateway:

```json
"anthropic-gateway": {
  "adapter": "anthropic",
  "baseUrl": "https://gateway.example/v1",
  "apiKey": "...",
  "apiKeyTransport": "bearer"
}
```

Do not commit raw keys; prefer the dashboard/secure credential store for durable credentials.

## Transient 5xx retry

Key-auth `openai-chat`, `openai-responses`, and `anthropic` providers can opt into a bounded retry of HTTP 500/502/503/504 **before** any model-visible output. The policy is off unless `enabled` is true. `attempts` is the total send count (1–4, default 2). Valid `Retry-After` is honored and capped at 2s. 429 key-pool failover stays a separate recovery and is not wrapped in this loop.

```json
"my-gateway": {
  "adapter": "openai-chat",
  "baseUrl": "https://gateway.example/v1",
  "apiKey": "sk-...",
  "transientRetryOn5xx": { "enabled": true, "attempts": 2 }
}
```

## Storage cleanup policy

`"storageCleanup"` is the optional archived-session cleanup policy used by dashboard Storage. Omitted means **disabled**, `schedule: "manual"`, `mode: "quarantine"`. Enabling it never happens automatically. Loopback `GET`/`PUT /api/storage/cleanup-policy` and `POST /api/storage/cleanup-policy/run` read and write this object. Clocks are process-local. See [Storage](../storage/).

## Codex vs Benes

Benes state and Codex state are different trees. Uninstalling Benes state does not revert Codex; `restore` / `stop` does.

## Context projection

Omitted or `"contextProjection": { "mode": "off" }` leaves tool results intact. Other modes:

| Mode | Effect |
| --- | --- |
| `shadow` | Metrics only |
| `duplicate` | Collapse exact repeated tool text on the provider-visible copy |
| `recovery` or `on` | Large receipts plus hidden exact recovery |

Canonical Responses history is not rewritten. Native Codex passthrough is never projected. Chat Completions and Anthropic Messages are not projected. The Control board (`#startup`) loopback `GET`/`PUT /api/context-projection` writes `contextProjection.mode` (`off`, `shadow`, `duplicate`, `recovery`; `on` stores as `recovery`). The listener reads that field per request, so a restart is not required.

## Agent Fabric

Agent Fabric v1 is an opt-in lifecycle sidecar. `"fabric": { "enabled": true }` (default **false**) opens loopback `/api/fabric/*`. The Control board loopback `GET`/`PUT /api/fabric-settings` writes `fabric.enabled` live — no listener restart. Enabling Fabric does not start Codex, Claude, Grok, a worker, a subprocess, or model inference. This is not A2A, not a Claude SDK, and not Codex JSON-RPC.

Tasks persist as hash-chained JSONL under `~/.benes/fabric` (next to `config.json`). A Benes restart keeps those files; it does not resume or relaunch work. The live cap is 256 non-removed tasks. Each task may store 256 ordinary events plus 8 reserved control events so a live task can always close, fail, cancel, delete, and converge one single-child handoff (outbound/return propose+commit, child/primary terminals, plus failure/recovery alternatives such as `HandoffFailed`/`HandoffRolledBack`). A history with more ordinary events than that ordinary budget — including counts at or just under the modern absolute total of 264 — is treated as pre-policy: ordinary growth is frozen, and at most 8 trailing control events may still be appended so the task can reach a removable state. Total retained files including tombstones cap at 1024; the oldest **removed** histories are reclaimed to make room for a new create. Live tasks are never evicted. Oversized `title`/`goal` values are rejected; prompt-like goals are rejected. Event payloads still redact secrets and home paths. HTTP `owner`/`principal` values that are oversized, secret-looking, or home-path-like are rejected. HTTP `owner` on start/close is audit attribution (`actorId`); it does not become `currentOwner` or fencing identity. `StartRun` with a runtime session id still records richer ownership.

`start` and `close` are kernel lifecycle marks (`RunStarted` / `RunCompleted`). Close is terminal task closure: `taskState` and `runState` become `completed`, `terminal` is true, and start is forbidden. `RunFailed` is terminal failure: `taskState` and `runState` are `failed`. They are not model calls and do not terminate a worker. `delete` writes a tombstone and hides the task from the list; history stays readable by id until reclaimed. Disabling Fabric does not emit close events. While disabled, `/api/fabric/tasks*` returns **409** `fabric_disabled`. Unreadable or corrupt config returns **500** `config_unreadable` — it is not treated as disabled. `GET /api/fabric/status` answers `{enabled, kind:"lifecycle", capabilities:["lifecycle","execution","single_child_handoff"], schemaVersion}` when config is readable. Execute may set opt-in `delegation.model` for one fenced child handoff; instruction/output never land in Fabric events or leases.
