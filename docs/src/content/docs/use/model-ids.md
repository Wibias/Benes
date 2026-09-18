---
title: Model ids
description: How clients name a provider and model on the way in.
---

The selector in `internal/router` parses the inbound `model` field.

## Namespaced

```text
<provider-id>/<upstream-model>
```

Example: `google/gemini-2.5-pro`. This always wins over the default provider.

## Bare

A id without a slash uses:

1. The configured default provider, or
2. A unique match against known model names.

## Slashes inside upstream ids

Some vendors use `/` inside the model name. Benes also publishes those ids with inner slashes rewritten to `-`. Both forms route.

## Combos

A combo id is a virtual model that expands to a failover list. See [Combos](../combos/). `/v1/models` still lists `combo/<id>` so clients can select it; `owned_by` is the OpenAI-compatible inbound owner (`openai`), not the `combo` routing namespace.

## Routing profiles

A routing profile is a named candidate list. Clients send `policy/<id>`. The dashboard nickname is not a router alias. Require and compatibility gates skip ineligible candidates on live traffic and in preview. `encryptedCodexTasks` keeps only ChatGPT/Codex login (`openai-responses` + `authMode: "forward"`). Remaining candidates are reordered by the profile's optimization weights (latency, health, cost, quota) when those weights are set; omitted weights use the dashboard defaults. Missing quota, health, or price is not treated as zero — `unknownEvidence` allow / penalize / exclude decides. Cost has no catalog USD source: unknown cost is penalized, never invented as `$0`. A thread sticks to the committed member (`previous_response_id`, prompt cache key, user, or first-user-text hash). The listener then walks that ordered list with the same failover walker combos use. Loopback `GET /api/routing-analytics` reports in-process `policy/<id>` walks (empty traffic stays empty). Dashboard Profiles persist through loopback `PUT`/`DELETE /api/routing-profiles`. Saving a combo or profile also synthesizes `combo/<id>` or `policy/<id>` on `/v1/models` without a restart.

## Visibility presets

Each configured provider can keep an allowlist in `selectedModels`. Empty or omitted means all catalog rows for that provider are visible (the global `disabledModels` denylist still applies).

`benes models preset apply <provider>` seeds that allowlist from a curated rule set. That is a seed, not a lock: later edits become `custom`. `--all` (or the dashboard **All** segment) clears the allowlist. A zero-match seed keeps the previous selection and warns; it never writes `selectedModels: []`, because that would mean “show everything”.

Only OpenRouter ships a v1 rule set. Adding a high-volume provider that has a preset seeds the allowlist when the add payload already lists matching models; existing providers are not rewritten on upgrade.

## New arrivals

Existing installs keep today's behavior: a model that appears in the provider catalog is visible unless you already have an allowlist or you disabled it. `benes models new-policy off` (or the Models toggle **New models start disabled**) records a baseline on the first successful reconcile and later genuine arrivals start in `disabledModels`. Enabling one is durable. Empty allowlists only: a preset/custom allowlist already excludes unknowns, so those providers are not double-managed. Custom models are never treated as discoveries. Failed or empty live lists do not shrink the baseline.

