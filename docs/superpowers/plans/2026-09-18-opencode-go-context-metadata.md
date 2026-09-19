# OpenCode Go context metadata authority

## Goal

Stop live-discovered OpenCode Go models from silently inheriting Benes' conservative 128k fallback when stronger context-window evidence exists.

Do not solve this by pasting an unversioned table into the preset.

## Current Benes evidence

- `internal/catalog.ConservativeContextWindow` is 128000.
- The `opencode-go` preset has live model discovery enabled.
- Its current `modelContextWindows` seed contains only a small subset of the models for which the preset already carries other capability metadata.
- OpenCode's public Go model list changes over time and explicitly says models may change.

Provider reference:

- https://opencode.ai/v2/docs/console/go

The public provider page is useful for roster membership, but it does not establish every context-window value. Context-window values therefore need a separate evidence source.

## Required invariant

A catalog row must distinguish:

- discovered context window;
- curated provider metadata with source/provenance;
- operator override;
- conservative fallback.

A fallback must never masquerade as measured or vendor-published metadata.

## Research sequence

1. Enumerate the live OpenCode Go roster through the existing discovery path.
2. Compare every discovered model with:
   - configured `modelContextWindows`;
   - Benes model metadata if any;
   - an authoritative or directly testable context source.
3. Produce a missing/known/conflicting matrix.
4. Do not write a number when evidence is absent.
5. Add regression coverage proving that a known large-context model does not collapse to 128k when its metadata source is present.

## Preferred design

Move durable model facts out of the hand-maintained provider seed when they are shared metadata rather than provider configuration.

Use a small generated metadata snapshot with explicit fields:

- provider id;
- upstream model id;
- context window;
- max output if known;
- source identifier;
- source revision/date.

Generation should be deterministic and checked into the repository. The runtime consumes the generated Go/JSON artifact and performs no network request for metadata on the hot path.

Provider preset values remain valid as operator/product policy and may override the shared metadata only through the existing precedence rules.

## Conflict policy

When live discovery and curated metadata disagree:

- live explicit context wins when the provider returns it;
- operator override wins when configured;
- otherwise keep the curated value but expose its source;
- if evidence is stale or contradictory, prefer unknown/conservative behavior over an invented exact value.

## Tests

Cover:

- discovered explicit window wins;
- curated window fills an otherwise missing live value;
- operator override/cap precedence remains unchanged;
- an unknown model still gets the conservative fallback;
- generated metadata has no duplicate provider/model keys;
- preset/model metadata cannot silently disagree without a test failure.

## Non-goals

- Hardcoding the entire provider roster forever.
- Treating request-cost estimates as context-window evidence.
- Fetching third-party metadata during every request.
- Changing the 128k conservative fallback globally.

## Acceptance gate

No context number is added without source evidence recorded by the generation input.

The implementation is complete only when the catalog can explain why a model has a given window and when stale provider discovery cannot silently downgrade a model with stronger known metadata.
