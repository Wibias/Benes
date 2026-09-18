---
title: Troubleshooting
description: Port hops, Codex restore, doctor, and remote binds.
---

## Wrong port

Unpinned `benes start` may bind a free port when 23100 is busy. `benes status` prints the live port. `benes sync` rewrites Codex to that port. `--port` never hops; it fails if the port is taken.

## Codex still talks to OpenAI

The listener is up but injection is stale. Run `benes sync`. To leave Codex natively: `benes restore`.

## Codex picker locked to Luna Reserve

After the ChatGPT 5-hour window hits 100%, Codex App may offer `gpt-reserve` and disable every other picker row, including Benes-routed `provider/model` ids. `benes sync` keeps those routed rows free of ChatGPT plan-gating metadata; it cannot re-enable a row the app has already greyed out. Set `model` in `~/.codex/config.toml` to the routed id (see [Codex](../use/codex/)) and check the Benes request log. If nothing arrives, the gate is in the Codex client.

## ChatGPT desktop: Windows setup / `config_load`

`benes sync` / `benes start --inject` write `model_provider = "benes"` as a **root** key in `~/.codex/config.toml`. If that assignment sits under the last table (for example `[tui.model_availability_nux]` or `[features.*]`), Codex rejects the file and the desktop app stops at `config_load`. Run `benes sync` from a build that places the key at the root, then restart ChatGPT.

## `provider add` exits nonzero

That command talks to the live proxy. Start it (`benes start` / `benes service start`) and retry.

## Remote bind refused

`"hostname": "0.0.0.0"` requires `BENES_API_AUTH_TOKEN`. The process will not start without it.

## Windows service / memory

`benes doctor` prints config, proxy, platform, and shim state. `GET /api/system/memory` (admin token) shows retained-byte budgets. The service wrapper exits with code 3 if the Go CLI path is missing, instead of retrying a hollow install.

## Codex `parallel_tool_calls` on Kiro

Codex often sends `parallel_tool_calls: true`. That is a client permission hint. Benes accepts it on `kiro` and still serializes tool calls; it does not ask Kiro to run tools in parallel. Other required controls Kiro cannot honor still error.

## Google / Gemini tool history 400

Interrupted or replayed tool history can leave unpaired `functionCall` / `functionResponse` parts. Benes repairs that at the Google adapter: missing results become an explicit unknown-status response, and orphan results become marked user text. It does not invent successful tool output.

`POST /api/github/star` without a dashboard session is `consent_required`. That is intentional.
