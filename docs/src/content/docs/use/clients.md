---
title: Other clients
description: Claude Code, Grok, OpenCode, MiniMax, ZCode, Prime Agent, and exported configs.
---

Anything that can speak Responses, Anthropic Messages, or Chat Completions to `http://127.0.0.1:23100/v1` can use Benes. Named commands wire the usual coding clients to that listener.

## Claude Code

```bash
benes claude
```

`benes claude` is the canonical launch command. It starts Claude Code against the same loopback listener.

Configure Claude Code from **Harnesses → Claude → Settings** (`#harnesses/claude/settings`). That dashboard surface is the supported settings UI. The current contract is:

- **Auth mode:** Auto, Subscription, or Proxy
- **Context:** automatic extended context, plus a compaction threshold (inherited default `829800`, or an explicit override from `100000` to `1000000`)
- **Agents:** expose Benes subagents to Claude Code
- **Model routing:** a background helper model, plus Opus / Sonnet / Haiku / Fable family routes from the live `/api/models` catalog

The same values are stored through `GET`/`PUT /api/claude-code`. Claude-specific auto-connect, fast-mode, and nested sidecar controls are not part of this Settings surface.

## Claude Desktop

Claude Desktop is a separate Harnesses client with its own runtime contract: `GET /api/claude-desktop` and `GET /api/claude-desktop/status` (canonical status), `PUT /api/claude-desktop` (desired enablement only), `POST /api/claude-desktop/apply`, and `POST /api/claude-desktop/disable`. It does not share the Claude Code Settings form.

**Harnesses → Claude Desktop → Settings** is the only Claude Desktop settings surface. It reports host support, installation, configurability, the managed native path, desired enablement, applied and stale truth, and the exact machine-readable refusal, and it offers Apply, Re-apply, and Disable through that lifecycle. A saved preference is never presented as applied native state.

Claude Desktop's supported local native configuration does not expose inference or model routing. Opus, Sonnet, Haiku, and Fable assignments therefore cannot have a truthful native Claude Desktop runtime effect through that surface, and Benes does not fabricate one. No model, family, endpoint, or credential is written to a Claude Desktop file.

- **Supported hosts:** Windows (`%APPDATA%\Claude\claude_desktop_config.json`) and macOS (`~/Library/Application Support/Claude/claude_desktop_config.json`). Every other host fails closed and leaves native files untouched. Installation is never inferred from a bare `claude` command, which is the Claude Code CLI.
- **Native ownership:** at most one entry, `mcpServers.benes`, in that JSON document. Every unrelated root key and every other MCP server is preserved. A value under `mcpServers` that Benes does not own is refused rather than replaced.
- **Apply:** Benes ships no stdio MCP runtime, so there is no executable projection to bind. `POST /api/claude-desktop/apply` refuses with `benes_mcp_runtime_unavailable` and leaves the file untouched rather than writing a placeholder or dangling entry. `POST /api/claude-desktop/disable` removes the entry only when it is exactly the projection Benes wrote, and is an unchanged no-op otherwise.
- **Status:** one canonical response reporting host support, installation, configurability, the managed native path, desired enablement, the observed native projection, applied and stale truth, and fingerprints derived only from native state. `applied` is never inferred from a preference, a stored profile, a previous API response, or process detection.
- **Staleness:** `stale` is true only when a Benes-owned native entry exists and differs from the desired projection. Unrelated user edits and formatting-only rewrites do not make it stale.
- **Restart:** Claude Desktop reads this file at startup, so a real native mutation requires a full quit and restart. Benes cannot observe that a running instance consumed a changed file, and reports `restartRequired` only after an actual mutation.
- **Secrets:** none are stored, fingerprinted, or surfaced. The historical `claudeDesktop` profile block is compatibility-only data; it is not desired state and never reaches native configuration.

## Grok Build

```bash
benes grok
```

Talks to the running proxy’s Grok surface (`/api/grok`).

Grok Build carries no model list of its own. Benes owns exactly one managed block in the Grok config, and that block is a projection of the Benes catalogue: every model the listener routes is written into it, and nothing else decides what Grok sees. Add or remove models in **Models** — there is no second list to maintain.

Refresh the projection from **Harnesses → Grok** (the Overview shows how much of the catalogue is registered) or from the CLI:

```bash
benes grok status   # registered models versus the current catalogue
benes grok apply    # (re)write the managed block from the running catalogue
```

The block is written when the Grok Harness is applied. After that it keeps itself current: with the Harness `Auto apply changes` setting on, Benes reconciles the projection whenever the catalogue changes or the listener comes up on an address, so a model added in **Models** reaches Grok without pressing Re-apply. With that setting off, nothing writes the block behind your back, the Overview reports `Out of date`, and Re-apply writes the current catalogue. Applying on a different port also rewrites the block, because the address it points at is part of the projection.

Disabling the Grok Harness removes only the managed block; the rest of the file is untouched.

## OpenCode

OpenCode is a **third-party** coding client (`opencode.ai`), not this project.

```bash
benes opencode
```

Writes a provider block so OpenCode uses the Benes listener. Env: `BENES_OPENCODE_API_KEY` / `OPENCODE_CONFIG_CONTENT`.

## MiniMax

```bash
benes mcode    # enable MiniMax Code, then launch it against loopback
benes mmx      # MiniMax CLI text commands through a loopback bridge
```

`benes mcode` writes the managed provider block first. You do not need a separate enable command unless you only want the file and not the CLI.

## Other file clients

These write the same managed Benes block as `benes integration client enable --client <id>`, then exec that client's CLI:

```bash
benes pi
benes prime
benes omp
benes hermes
benes openclaw
benes kimi
benes gajae
benes dsh
```

Extra args go to the client (`benes pi --help`). Pi, Prime, OMP, Kimi, Gajae, and DSH are loopback-only. Hermes and Openclaw can target a non-loopback listener and then send `x-benes-api-key`. `benes export --client <id>` still prints the snippet without applying it.

## ZCode

```bash
benes zcode
```

Writes a managed `provider.benes` fragment. Benes owns `kind`, `baseURL`, `apiKey`, model membership, `name`, `input`, and `contextWindow` when it emits one. The fragment is `kind: "openai-compatible"` with `baseURL` at the listener `/v1` root so ZCode appends `/chat/completions`. Apply keeps ZCode’s documented per-model `reasoning` and `limit.output` (and `limit.context` only when Benes did not emit `contextWindow`). Unknown keys are dropped. A second connection envelope (`options`) is refused. Disable removes only `provider.benes`.

## Exported JSON

`benes export` prints a client snippet aimed at the live base URL. Builders in `internal/export/clients.go` cover OpenCode, Pi, Prime Agent, Hermes, Openclaw, Kimi, Gajae, DSH, Mcode, and related layouts. Loopback usually omits a real key; non-loopback injects `x-benes-api-key`.

Pi’s default file is `~/.pi/agent/models.json`. If `PI_CODING_AGENT_DIR` is set, export and the dashboard Pi integration both write `models.json` in that directory instead. The override must be an absolute path; a relative value is rejected. With no override, install detection still uses `~/.pi`.

Prime Agent uses the same `models.json` provider contract as Pi, with its own files and ownership. The default is `~/.prime/agent/models.json`. `PRIME_AGENT_CODING_AGENT_DIR` writes `models.json` in that directory and is detected there; it must be absolute. With no override, detection uses `~/.prime`. Enable with `benes prime` or `benes integration client enable --client prime`. Disabling removes only Benes’ `providers.benes` block.

## Generic HTTP

Point the client’s OpenAI-compatible base URL at `http://127.0.0.1:23100/v1`. Use `x-benes-api-key` when the listener is not loopback-only.
