---
title: Providers
description: Built-in presets, OAuth, keys, and the ChatGPT account pool.
---

Presets live in `internal/server/provider_presets.json` and are projected through `internal/providerregistry`. `benes init` and the dashboard both read that list. The dashboard **Providers** board (`#providers`) is the control surface: add an upstream, inspect credentials and quota, and jump to models, routing, and sub-agents.

The workspace is one shell: search and filter on the left rail, five live KPIs at the top (Total, Healthy, Attention, Disabled, Exposed models — only Attention is yellow), and **+ Add provider** at top-right. Add Provider lists one catalog row per vendor (Command Code, OpenAI, Anthropic, …). The Connection step picks which config id to add — OAuth vs API key, Codex login vs API, Coding Plan vs token plan — and you can run the flow again to add another member of the same family. Verify then lists the live catalog (`POST /api/model-discovery`) and remaining quota (`GET /api/provider-quotas?refresh=1`) when the provider reports them; Command Code's catalog is `https://api.commandcode.ai/provider/v1/models` for both OAuth and API-key connections. With one or more providers and none selected, the right pane is the fleet overview: needs attention, model/catalogue availability, downstream harness/route/sub-agent counts, and recent provider events. `GET /api/providers/workspace` is the aggregate for those counts; rail groups use the same healthy / attention / disabled lifecycle. While `#providers` is visible, quota rows also refresh from `GET /api/provider-quotas` without `?refresh=1` (30s); a hidden tab does not poll. Model catalogs fill when you run **Sync models now** on a provider. That fetch uses the stored API key or OAuth token and writes ids into `providers.<id>.models`. ChatGPT/Codex uses the Codex models endpoint, not Codex's own `models_cache.json`. Idle discovery still skips `authMode: "forward"` so the overview does not auto-probe. Catalog **Probe** on the Models tab is a `{baseUrl}/models` GET with a stored API key, so it is hidden for ChatGPT login; **Sync models now** is the live list for that account. A successful sync replaces that provider's in-memory catalog on the running listener, so fleet available/unavailable counts and last-sync update without a restart. Sync also stores each model's advertised context window (`context_length` and the usual aliases) so Models **Max** is the live window (for example 1.05M on GPT-5.6), not the 128k conservative default. Restart is still required before adapter, pacing, or newly added providers serve traffic.

Do not auto-select the first provider. Select a rail row for **Overview**, **Access**, and **Configuration**. Click the selected row or the detail back control to return to the fleet overview. Local or no-auth providers omit Access when there is nothing to manage.

Overview answers how Benes can reach that provider and what depends on it (access methods, default access, last validated, exposed models, downstream use, recent events). Account quotas stay on Access.

Access is capability-based. A logical provider can expose OAuth accounts and API keys together. Canonical **OpenAI** is one rail row: ChatGPT/OAuth on `openai` plus the OpenAI Responses API on hidden connection `openai-apikey`. Default access chooses that lane (ChatGPT pool vs OpenAI API) when both lanes can serve the requested model; if only one lane exposes the model, Benes uses that capable lane. An explicit account-targeted OpenAI route stays on OAuth, and an explicit `openai-apikey/...` route stays on API. Credential selection appears only when a pool is relevant (more than one selectable OAuth account or key). Its **Mode** is the real OpenAI Pool/Direct account mode, while Strategy and strategy-specific controls apply only to Pool mode. Thread affinity stays automatic runtime behavior and is not an Access setting. There is no silent OAuth ↔ API-key failover after an upstream failure.

Configuration is view-first, per-section Edit. OpenAI shows separate ChatGPT and API connection contexts. Request pacing and model pacing overrides are API-scoped (`openai-apikey`); ChatGPT/OAuth traffic does not use that limiter. Queue / next-slot / last-model are live when the listener publishes them; otherwise they show as unknown (`—`). **Edit**, plus add, disable, set-default, and remove, persist through `/api/providers` into the listener config. `GET /api/config` returns those public fields and never secrets. Live routing still uses the process that was started — restart the listener to apply adapter, pacing, aliases, or newly added providers to traffic. Footer links open the models catalog, routing, and provider logs.

Provider and model aliases are CLI-only (`benes alias set|list|remove`). They are optional short names for a configured provider or a canonical model id on that provider; the upstream request still uses the canonical ids.

## Add one

```bash
benes provider list
benes provider add anthropic
benes login anthropic
benes provider set-default anthropic
```

Key-only presets take `benes provider add <id> --key …` or a `${ENV}` reference in config. Do not commit raw keys.

## Auth styles

| Style | Typical ids |
| --- | --- |
| ChatGPT / Codex login, optional pool | `openai` |
| API key on OpenAI | `openai-apikey` |
| OAuth | `anthropic`, `xai`, `kimi`, `cursor`, `command-code`, `nous` |
| API key | `openrouter`, `groq`, `deepseek`, `mistral`, `together`, `minimax`, `minimax-cn`, … |

`xai` keeps one logical provider id. OAuth/subscription login uses the Grok CLI proxy (`https://cli-chat-proxy.grok.com/v1`) with Grok CLI identity headers. An xAI API key stays on `https://api.x.ai/v1` and never rides that proxy. Restart the listener after login so the oauth token is bound.
| Local server | `ollama`, `vllm`, `lm-studio` |
| OpenCode Zen (third-party product) | `opencode-zen`, `opencode-go`, `opencode-free` |
| Kiro CLI session | `kiro` |

`opencode-go` stays `openai-chat` by default. Before send, Benes uses OpenAI Responses only for the tested model ids `grok-4.5` and `grok-4.6`. Other Go models stay on Chat Completions. The wire is not inferred from a `grok-` prefix, and Benes does not retry the other wire after the request has started. Restart the listener after upgrading. Do not copy the live Go catalog into `providers.opencode-go.models`.

For OpenCode Go requests, Benes derives an opaque `x-opencode-session` value from stable client lane identity before the provider-specific wire is selected. The same logical lane keeps the same value across turns, retries, API-key rotation, and Chat/Responses/Messages selection; sibling lanes remain distinct. Raw thread or session identifiers are not sent in this header, and requests without stable identity omit it rather than creating a random value. Operators can set `providers.opencode-go.headers.x-opencode-session` explicitly; that case-insensitive override wins. Other custom provider headers remain unsupported.

`kiro` imports a local Kiro CLI session. Identity Center / desktop accounts that own a CodeWhisperer profile still need that `profileArn`. AWS Builder ID does not: those snapshots are valid with a token and region and no account-owned profile. Benes does not synthesize a profile ARN. `benes login kiro` follows the same rule.

Codex and other OpenAI-compatible clients may send `parallel_tool_calls: true`. That is a permission hint, not a requirement that Kiro run tools concurrently. Benes accepts the hint, keeps Kiro serialized, and does not send a parallel-control field upstream. Actually unsupported required controls (`service_tier`, structured text format, required tool choice) still fail closed.

On GenerateAssistantResponse, Benes sends the materialized callable catalog on the current user message (`userInputMessageContext.tools[].toolSpecification`). Shared `tools.Materialize` decides that set: deferred `tool_search` helpers stay off the wire until used, byte bounds can omit filler tools, and freeform top-level `exec` (Code Mode) stays pinned so it is not dropped as filler. Names already used in history are aliased the same way as before. Tool JSON Schema is sanitized only at schema-object positions for current Kiro/Bedrock constraints (`additionalProperties`, string/number bounds, root `oneOf`/`anyOf`/`allOf`); a user property named `format` or `pattern` is kept. Constructs that cannot be normalized fail locally instead of sending a weaker schema. Restart the listener after upgrading if `kiro` is configured on it.

`openai` in Pool mode can hold several ChatGPT/OAuth accounts (`~/.benes/codex-accounts.json`). New sessions pick a healthy account according to the configured policy (quota by default; opt-in `reset-window` ranks by the governing quota reset when the evidence is fresh); threads usually keep affinity to the account that started them. A pre-output HTTP 401 on a stored pool account force-refreshes that same physical account once and retries it; a second 401 ends that recovery and does not hop accounts. Quota and 429 recovery can still rebind. Direct mode uses only the current main login. Replacing the ChatGPT account behind that Main slot does not keep the previous account's cooldown, reauth, or failure evidence. The dashboard shows that pool on the OpenAI Access tab as **OAuth accounts**. The OpenAI API key lane (`openai-apikey`) is a separate connection under the same OpenAI row; adding the first key creates that hidden connection in config, but newly added runtime providers still require the listener restart described above before traffic can use them. It never fails over into the ChatGPT pool.

`google-antigravity` can hold several imported OAuth accounts. Before downstream output, an HTTP 401 first refreshes the selected physical account once. If that refresh fails or the refreshed account still returns 401, Benes may use another eligible account. A 403 can rebind only when the bounded response identifies an account validation or verification failure; geoblocks and unknown 403 responses do not rotate. Hard physical and continuation-owned requests stay bound to their selected account.

## Custom endpoints

`minimax` and `minimax-cn` stay on OpenAI-compatible Chat Completions. For models listed in `reasoningSplitModels`, Benes sends `reasoning_split=true` and keeps MiniMax `reasoning_details` (`type`, `id`, `format`, `index`, `text`) through stream parse, Chat inbound, and next-turn replay. That structured field is not copied into visible `content`. Other OpenAI-compatible providers are unchanged.

Any OpenAI-compatible Chat Completions base URL can be added as a custom provider. Anthropic Messages, Gemini, and OpenAI Responses have dedicated adapters under `internal/providers/`.

A custom `openai-responses` gateway that streams sparse lifecycle snapshots (missing `output`, `role`, close events, and similar canonical fields) still reaches Codex as a complete turn. Benes does not passthrough that SSE. The adapter keeps the text and tool events; `internal/responses/bridge` rebuilds `response.created` through `response.completed`, including `output_item.done` and a completed snapshot with `output`, `parallel_tool_calls`, `tool_choice`, and `tools`. There is no `responsesSnapshotRepair` flag: translation is the data-plane path. Ambiguous or unsupported upstream events still fail closed.

`benes provider test <id>` hits the running proxy. `benes models probe` checks live discovery.

OpenRouter can also take a curated **model** preset (`benes models preset apply openrouter`, or Preset on the Models board). That is separate from `benes provider presets`, which lists provider kinds.
