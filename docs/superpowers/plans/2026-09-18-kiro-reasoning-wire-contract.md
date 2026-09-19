# Kiro reasoning wire contract

## Goal

Make Kiro reasoning effort and opaque reasoning replay model-aware and field-accurate.

Do not solve this with opaque string prefixes or by assuming every Kiro model uses the same reasoning member.

## Current Benes evidence

- `internal/providers/kiro/effort.go` sends native effort only for a small explicit model map.
- The Kiro catalog already exposes additional GPT-5.6 models with reasoning efforts.
- `reasoningContentEvent` currently reads `content` and `redactedContent`.
- Assistant history currently replays stored opaque Kiro reasoning only as `reasoningContent.redactedContent`.
- Benes' encrypted Responses reasoning envelope currently stores one Kiro opaque string without the original wire member.

The missing piece is a typed provider-state contract that preserves both value and wire member.

## Required invariant

Opaque reasoning state must round-trip to the exact Kiro member that issued it.

Model effort support must be explicit per model and per accepted rung. A shared global effort ladder must not silently widen a provider wire contract.

## Research sequence

1. Build provider-capture fixtures from current Kiro responses for every Kiro GPT-5.6 model Benes exposes.
2. Record, without committing secrets:
   - accepted effort rungs;
   - request field used for effort;
   - response member used for opaque reasoning state;
   - replay member accepted by the provider.
3. Add RED tests from sanitized synthetic fixtures before production changes.
4. Confirm stored Benes reasoning envelopes can be migrated without invalidating old conversations.

## Preferred data model

Replace the untyped Kiro opaque string in internal flow with a small typed value:

- `Kind`: known wire member enum;
- `Value`: opaque provider data.

The public/canonical protocol still treats the value as non-user-visible provider state.

Persist both fields in the Benes reasoning envelope. Keep decoding backward-compatible with existing envelopes that contain only the legacy Kiro value; legacy values retain their legacy replay semantics.

Do not encode the member name inside the opaque value itself.

## Effort model

Represent native effort support as a per-model map of accepted rungs and target field.

Example shape conceptually:

`model -> { field, accepted efforts }`.

Unknown model or unknown rung must not be sent natively. Existing fallback behavior remains explicit rather than accidental.

## Tests

Cover:

- each proven model/rung emits the expected native effort field;
- unsupported rungs do not leak into native fields;
- an opaque value issued on each supported reasoning member round-trips to that same member;
- provider state never becomes visible reasoning text;
- old encrypted envelopes still decode and replay with legacy semantics;
- malformed or conflicting member data fails closed;
- request-owned history is not mutated during compilation.

## Non-goals

- Inferring reasoning strength from opaque blob size.
- Treating all AWS/Kiro models as one family.
- Exposing opaque reasoning provider state to Chat Completions clients.
- Rewriting historical encrypted payloads in place.

## Acceptance gate

No model/rung or replay-member change lands without a sanitized capture or deterministic provider contract test.

The final design must preserve wire-member identity structurally and remain backward-compatible with existing Benes reasoning envelopes.
