---
title: Sub-agents
description: Roster, fallback, and the v1/v2 spawn surface Codex reads from the catalog.
---

Codex can spawn helper agents. Benes publishes which routed (and native) models appear on that surface. The dashboard **Subagents** board (`#subagents`) edits the same roster, fallbacks, and first-call model.

```bash
benes agent status
benes agent subagents
benes agent fallback
benes v2 status
benes v2 mode v2
```

`subagentModels` is the roster. `subagentModelFallback` is the ordered backup list. Effort ceilings (`effortCap`, `subagentEffortCap`) clamp reasoning levels advertised in the catalog (`internal/catalog`).

`benes v2` switches the multi-agent surface (`v1` / `default` / `v2`). Catalog construction keeps spawn membership independent of picker display order.
