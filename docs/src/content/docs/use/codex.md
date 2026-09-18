---
title: Codex
description: Inject routing into Codex CLI, TUI, App, and SDK without patching binaries.
---

Benes does not modify Codex executables. `benes init` / `benes sync` write Codex-native files under the home resolved by `internal/config/paths.go` (`ResolveCodexHome`): `CODEX_HOME` if set and valid, otherwise `~/.codex`.

Typical writes:

- `config.toml` routing keys
- `benes-catalog.json` (routed `provider/model` rows plus any native Codex rows already on disk)
- `models_cache.json` (refresh with `benes sync-cache`)

`benes sync`, `benes sync-cache`, and the dashboard Codex restart path rebuild that catalog from the live Benes projection. Namespaced rows are listed with `visibility: "list"` and `supported_in_api: true`. They do not copy ChatGPT plan-gating fields (`available_in_plans`, `minimal_client_version`, `availability_nux`, `upgrade`). The clone source for those rows is a roster-supported native model (`gpt-5.6-sol`, `gpt-5.6-terra`, `gpt-5.6-luna`, …). A client-injected Reserve fallback such as `gpt-reserve` is never used as that template.

Codex CLI, TUI, App, and SDK share that home, so a model that appears in the App picker is the same catalog the CLI reads. The injected provider table name is `"Benes"` (`internal/codexrestore/inject.go`).

## Keep it pointed at the listener

```bash
benes start
benes sync
```

`start` leaves Codex `config.toml` alone. `sync` (or `benes start --inject`) writes routing to the **currently running** port. Useful after a port hop.

Built-in Codex `web_search = "live"` posts `POST /v1/alpha/search` to that same base URL. Search uses the dashboard web-search sidecar (`webSearch.enabled` plus the configured backend), not the main model route. ChatGPT OAuth is only used when that native Codex backend is the selected search backend.

## Autostart

- **Service** (`benes service install`) — always-on, restarts on crash.
- **Shim** (`benes codex-shim install`) — starts the proxy when `codex` launches; no resident daemon. On Windows a `.exe` launcher is replaced with a real PE wrapper (plus a sidecar), not a batch script saved as `.exe`. ChatGPT/Node spawn that PE; a batch-as-exe fails with `spawn UNKNOWN`.

## Undo

```bash
benes stop          # live process + restore
benes restore       # restore even if the proxy is already gone
```

History recovery for a pre-backup OpenAI App store: `benes recover-history --legacy-openai`.

## Native quota fallback limitation

After the ChatGPT 5-hour window is exhausted, Codex App may offer Luna Reserve (`gpt-reserve`) and grey out other picker rows. That gate runs in the client before a request reaches Benes.

Benes still writes routed catalog rows without ChatGPT eligibility metadata, which is the catalog-side defect that would make a Google or Anthropic model advertise a Plus plan. Whether the App still hides those rows once Reserve is active is unverified here; we cannot observe that closed client.

If the picker is locked, set the model id directly in `~/.codex/config.toml` instead of choosing it in the UI:

```toml
model = "google/gemini-2.5-pro"
model_provider = "benes"
```

Combos use `combo/<id>`. If a request then appears in the Benes log, the gate is client-side. Proxy routing for an explicit `provider/model` selector does not consult Codex quota.
