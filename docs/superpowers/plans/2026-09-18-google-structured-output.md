# Google structured-output support

## Goal

Make Benes' structured-output capability truthful for Google-family providers.

A model must not be selected for a request that requires structured output unless the adapter can compile Benes' parsed `TextFormat` into the exact Google wire shape supported by that route.

## Current Benes evidence

- `protocol.RequestOptions.TextFormat` already represents structured output.
- OpenAI Chat and Responses compilers already serialize it.
- Provider capability projection carries `NoStructuredOutputModels`.
- The Google/Vertex/Antigravity paths do not currently have an obvious `TextFormat` compiler.
- Google documents JSON structured output for Gemini and requires an output MIME type together with a schema on the generation request.

Primary vendor references:

- https://ai.google.dev/gemini-api/docs/structured-output
- https://ai.google.dev/api/generate-content

## Required invariant

Capability admission and wire compilation must be the same truth.

If Benes says a routed model supports structured output, the physical request must carry the schema in the provider's supported field. If that route cannot express the schema, Benes must refuse before dispatch.

## Research sequence

1. Map Benes' Google protocols:
   - AI Studio / generateContent;
   - Vertex;
   - Cloud Code Assist / Antigravity.
2. Capture the final JSON body each path currently sends for a request with `TextFormat`.
3. Confirm which Gemini model families and endpoints accept:
   - JSON object mode without a schema;
   - JSON schema mode;
   - streaming structured output.
4. Check whether non-Gemini models exposed through a Google-hosted route share the same capability. Do not infer this from the transport name.
5. Add RED compiler tests before changing production code.

## Proposed compiler

Create one small structured-output compiler in the Google provider package.

Input: validated `protocol.TextFormat`.

Output: Google generation config fields only.

Rules:

- `json_schema`: set `responseMimeType: application/json` and the supported JSON-schema field for that endpoint;
- `json_object`: set JSON MIME type without inventing a schema;
- preserve caller schema values after validation; do not rewrite property order or silently drop unsupported constraints;
- reject unsupported format types before network I/O;
- reject route/model combinations without proven support.

Do not put Google wire fields in the shared protocol model.

## Better-than-minimal behavior

The capability layer should be positive evidence, not "not listed as unsupported".

Represent structured-output support as known true / known false / unknown where possible. Unknown routes should not satisfy a hard routing requirement.

Add a provider-specific schema validator only for constraints Google cannot accept. Do not build a second general JSON Schema implementation.

## Tests

Cover:

- Gemini schema mode produces the expected generation config;
- JSON object mode has no invented schema;
- unsupported model/route fails before dispatch;
- streaming keeps the same schema contract;
- caller-owned parsed request is not mutated;
- schema size remains inside existing request bounds;
- tools + structured output are tested only for model families where the vendor documents the combination.

## Non-goals

- Converting arbitrary JSON Schema into another schema language.
- Claiming support for every model behind a Google-hosted gateway.
- Relaxing structured-output requirements during failover.

## Acceptance gate

Implementation starts after endpoint-specific tests establish the current gap and the supported wire form.

The final capability projection, router decision, and provider body must agree on the same support matrix.
