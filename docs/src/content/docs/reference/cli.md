---
title: CLI
description: Command groups on cmd/benes.
---

`benes help` prints this map. `start` and `serve` are the same process.

## Process

| Command | Role |
| --- | --- |
| `start` / `serve [--port] [--inject]` | Foreground data plane. Codex `config.toml` is unchanged unless `--inject`. |
| `stop` | Pid-file stop + Codex restore |
| `restart` | Stop then start |
| `ensure` | Start only if no runtime-port file |
| `status` | Pid/port from `runtime-port.json`, plus Codex route (`benes` / `native` / `other` / `unknown`) |
| `health [--json]` | Runtime-port present |
| `ready [--json] [--wait [--timeout N]]` | `/readyz` is `ready` |
| `gui` | Open dashboard; start if needed |
| `dev [--port 23200]` | Checkout GUI/data-plane for local work |
| `doctor` | Local snapshot |
| `sync` | Point Codex at the live port and rebuild `benes-catalog.json` |
| `sync-cache` | Rebuild the on-disk catalog and refresh `models_cache.json` |
| `restore` / `eject` | Strip managed Codex routing |
| `uninstall` | Stop, drop service/shim/tray, restore Codex |

`benes dev` is the checkout developer command. Default GUI port is **23200** (`--port` still overrides). `--proxy off` (default) is Vite HMR against the running `benes start` listener on **23100**. `--proxy on` also runs this checkout's Go data plane, still using `~/.benes` provider config and auth, without Codex inject and without replacing the main pid file. `--update` fast-forwards `--branch` (default `dev`). `--no-gui` is valid only with `--proxy on`. Go sources under `cmd/` and `internal/` rebuild on change.

## Background

| Command | Role |
| --- | --- |
| `service status` / `start` / `stop` / `install` / `uninstall` / `repair` | OS service |
| `codex-shim status` / `install` / `uninstall` | On-demand Codex wrapper |
| `tray …` | Windows tray |

## Config and catalog

| Command | Role |
| --- | --- |
| `init` / `setup` | Interactive first-run |
| `config show` / `get` / `set` / `unset` / `mutations` / `validate` / `export` / `import` | Durable JSON |
| `provider …` | Presets and accounts |
| `models …` | Catalog, custom models, probe, visibility, `models preset`, `models new-policy`, `models new-arrivals` |
| `account …` | ChatGPT pool and API-key slots. `account reset-credits <id|main> [--consume --yes]` lists or spends a banked reset; `--consume` mints one `redeemRequestId` for that invocation. |
| `login` / `logout` | OAuth / credential store |
| `combo …` / `route …` | Failover ids and policy |
| `alias list` / `set` / `remove` | Provider and model aliases |
| `agent …` | Injection, effort, roster, sidecars |
| `v2 …` | Spawn surface |

## Clients and ops

| Command | Role |
| --- | --- |
| `claude` / `opencode` / `mcode` / `mmx` / `zcode` | Launch or write client config |
| `grok status` / `grok apply` | Report or write the Grok projection of the Benes catalogue (needs the listener) |
| `pi` / `prime` / `omp` / `hermes` / `openclaw` / `kimi` / `gajae` / `dsh` | Enable that file client and launch it |
| `export` | Print a client snippet without applying it |
| `client …` | Managed-filesystem apply/restore |
| `access key …` | Dashboard API keys |
| `observe` / `logs` / `usage` / `memory` / `storage` / `debug` | Introspection. `observe usage` reads the local durable usage ledger (no listener). |
| `update` | Prints update policy; does not mutate |
| `version` | Version string |

`benes observe usage` (also `benes usage`) reports from the durable usage ledger under `~/.benes` without a running listener. That includes legacy `usage.jsonl` plus `$BENES_HOME/usage/` segments when present. `benes observe usage retention status|preview|run` talks to the live loopback retention API (destructive `run` requires a preview `--digest`). `--json` is the Summary payload, including completeness and cost metadata from the same aggregator as `GET /api/usage`; the default is Markdown tables. `--start`/`--end` accept date-only or minute/RFC3339 instants (inclusive start, exclusive end). `--provider`, `--model`, `--account`, `--surface`, and `--tz` compose with the window. `--surface codex` matches explicit `codex` rows only. Malformed, reversed, or >366-day ranges exit 2. See [Usage](../usage/).

`benes observe logs index-status` reports the derived request-history index at `$BENES_HOME/routing-history.sqlite`. The durable usage ledger is the authority; the SQLite file is a derived index. Normal listener operation incrementally indexes newly committed ledger rows. `index-status` prints schema version, unique indexed request ids, live source size, indexed offset, pending bytes, whether the index is caught up, and whether a rebuild is required. `IndexedOffset` is the committed logical byte cursor: every complete ledger line strictly before that offset was processed by this index version. Processing a line does not always insert a `requests` row (blank, malformed, oversized, or missing `requestId` still advance the cursor). After a successful sync of one snapshot, `indexedOffset` equals that snapshot’s size; live `sourceSize` can be larger when new rows arrived later. That lag is normal. `benes observe logs explain <request-id>` asks the running listener for `GET /api/request-history/<request-id>/route-decision` and does not require a manual rebuild after ordinary newly admitted requests. `benes observe logs rebuild-index` is an explicit repair: it discards and reconstructs the derived index from the complete logical durable ledger (legacy `$BENES_HOME/usage.jsonl`, sealed `$BENES_HOME/usage/segments/*.jsonl`, and committed `$BENES_HOME/usage/active.jsonl` rows). Use it for a missing, corrupt, incompatible, or untrusted index. Durable retention is opt-in via `observe usage retention`. Usage aggregation still reads only the newest 64 MiB of the retained logical stream. Request-history schema v3 stores per-request `ledger_offset` and a retention watermark; v2 indexes rebuild once in the background.

Exit codes used by `ready`: `0` ready, `1` not ready, `64` bad flags.

## Model presets

`benes models preset` is not `benes provider presets`. Provider presets are kinds (OpenRouter, Groq, …). Model presets are a managed **seed** of which catalog rows a provider should show.

```bash
benes models preset show openrouter
benes models preset apply openrouter
benes models preset apply openrouter --all
```

`apply` writes `providers.<id>.selectedModels` and a `modelPreset` marker (`preset` or `all`). `--all` clears the allowlist (empty/missing `selectedModels` means every catalog row is visible). A zero-match apply keeps the previous selection and prints a warning; it never writes an empty allowlist.

Today only **openrouter** ships a curated rule set. Other configured providers have no preset; `apply` without `--all` then warns and leaves the selection unchanged. Existing providers are not narrowed on upgrade. Editing the allowlist (`benes models selected --set/--clear`, or `PUT /api/selected-models`) while the marker is `preset` flips it to `custom`. Benes does not rewrite a custom selection. A custom model added while the marker is still `preset` is appended to the allowlist so it stays visible.

## New catalog arrivals

```bash
benes models new-policy show
benes models new-policy off
benes models new-policy off --provider openrouter
benes models new-arrivals
```

Missing `modelDiscovery` means policy **on** (current behavior). `off` seeds a baseline without hiding the existing catalog; later ids start disabled. `new-arrivals` lists auto-disabled rows and re-reconciles the configured catalog.

## Aliases

Optional short names for a configured provider or a canonical model on that provider. Upstream still receives the canonical ids. There are no built-in aliases.

```bash
benes alias set google-antigravity agy
benes alias set openrouter/anthropic/claude-opus-5 opus
benes alias list
benes alias remove google-antigravity
benes alias remove openrouter/anthropic/claude-opus-5
```

`agy/opus` then selects the canonical provider/model. A bare model alias such as `opus` works only when it is unique across providers; otherwise the listener returns 400 with the candidates. Restart the listener after writing aliases.
