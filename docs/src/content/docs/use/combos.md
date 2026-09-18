---
title: Combos
description: A virtual model id that failovers across real provider/model pairs.
---

A combo is a named id with a list of targets. The Go projector (`internal/config/combo_projection.go`) accepts strategy **`failover`** only. Empty, omitted, or leftover `round-robin` becomes failover. Any other strategy is skipped as `unsupported_strategy`.

```bash
benes combo list
benes combo set cheap --strategy failover --target ollama/llama3 --target openai-apikey/gpt-4.1-mini
benes combo show cheap
```

Clients then send `combo/<id>` as the model. A dashboard nickname is screen-only unless Native OpenAI alias is enabled. The router walks targets until one succeeds.

The dashboard Routing tab **Combos (failover)** persists the same resource: loopback `PUT /api/combos` body `{ id, renameFrom?, combo }` and `DELETE /api/combos?id=` write `config.json` and rebuild the live combo map. The editor is failover only. Writes reject round-robin. Leftover round-robin records on disk are treated as failover.

`/v1/models` lists that `combo/<id>` row after save so it stays selectable without a listener restart. `owned_by` is `openai` because the list is an OpenAI-compatible catalog; `combo` is a Benes routing namespace, not a provider owner.

`benes route combo …` is the same resource through the routing command group.
