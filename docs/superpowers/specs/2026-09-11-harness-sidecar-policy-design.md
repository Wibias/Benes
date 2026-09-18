# Issue #257 — Generic per-Harness sidecar policy design

Date: 2026-09-11
Issue: #257
Base: `dev@4b6d33c2583545d4c55baf3d6ec43a34a9b41d6e`
Branch: `feat/257-harness-sidecar-policy`

## Goal

Define one generic Benes per-Harness sidecar policy layered over the existing global sidecar authority. The policy must be persisted once, consumed by the real request path, support explicit inheritance, gate controls by proven capability, expose effective decisions as request evidence, and remain owned by the Harnesses product surface.

This design intentionally does not restore `claudeCode.webSearchSidecar` or `claudeCode.visionSidecar` as supported sources of truth.

## Non-goals and parallel-work boundaries

This change does not:

- implement or alter OpenAI service-tier policy owned by #256;
- perform the shared GUI work owned by #234;
- redesign the global Sidecars settings surface;
- infer Harness identity from User-Agent, model names, endpoint shape, or other heuristics;
- add remote image fetching;
- add per-Harness sidecar model, backend, reasoning, timeout, or quota fields;
- create an Integrations-era parallel workspace;
- make Harness identity an authentication or authorization primitive.

The branch starts from the landed `dev` commit above and must not depend on in-flight #234 or #256 branches.

## 1. Canonical ownership and persistence

The existing global sidecar configuration remains authoritative for defaults, including model/backend and other modality-specific details.

Per-Harness overrides live in the existing Harness settings document, `harnesses.json`, under each known Harness row. The persisted shape is deliberately small:

```json
{
  "clients": {
    "opencode": {
      "autoDetect": true,
      "autoApply": true,
      "retainSnapshot": true,
      "allowRestart": false,
      "sidecars": {
        "webSearch": "enabled",
        "vision": "disabled"
      }
    }
  }
}
```

Rules:

- `sidecars` absent means inherit both capabilities from global defaults.
- An individual field absent means inherit that capability from the global default.
- Persisted override values are only `enabled` or `disabled`.
- UI state `inherit` is represented by field absence, not a stored third value.
- Clearing an override deletes that field rather than copying the current global value.
- Empty `sidecars` is omitted when serializing.
- Existing `harnesses.json` files without sidecar fields remain valid and continue to decode unchanged.
- Invalid values are rejected by the management API before persistence and are never coerced into a supported state. A manually corrupted settings file follows the existing `harness_settings_unreadable` path rather than silently inventing an override.

The Harness settings store remains the canonical persisted owner. Runtime policy logic must not be embedded in the persistence package.

### Management API patch semantics

`PUT /api/harnesses/settings` remains a partial update. The sidecar patch contract is explicit so that “leave unchanged” and “clear to inherit” cannot be confused:

```json
{
  "clientId": "opencode",
  "sidecars": {
    "webSearch": "enabled",
    "vision": null
  }
}
```

Semantics:

- omitted `sidecars` means do not change any sidecar override;
- omitted modality inside `sidecars` means do not change that modality;
- `"enabled"` sets an explicit enabled override;
- `"disabled"` sets an explicit disabled override;
- `null` clears that modality and restores inheritance;
- any other value or type returns `400 invalid_body` and does not mutate disk or live runtime state.

The response projects the canonical saved override state and the server-computed configured-effective state. The persisted file continues to use field absence for inheritance; `null` is an API patch operation, not a persisted third state.

### Global defaults stay live

The runtime snapshot for this feature contains per-Harness overrides only. It must not copy global `webSearchSidecar.enabled` or `visionSidecar.enabled` into Harness state.

At decision time the resolver combines the current global sidecar state with the current Harness override. Therefore:

- changing a global sidecar enabled flag immediately changes every inheriting Harness without restart or resave;
- clearing a Harness override immediately resumes the current global value, including future global changes;
- a Harness `enabled` override replaces only the global activation bit for that modality. Model, backend, timeout, reasoning, limits and other sidecar configuration remain global and current;
- if the global sidecar configuration is incomplete or cannot produce a proven runtime candidate, an `enabled` Harness override cannot manufacture one. Capability selection still fails closed.

## 2. Harness identity contract

Benes-managed Harness integrations that can reliably add request metadata stamp:

```text
X-Benes-Harness: <known-harness-id>
```

This header is a routing-policy identity hint, not authentication.

At the data-plane boundary Benes:

1. reads the header;
2. requires exactly one non-empty value; duplicate or ambiguous values are rejected as identity metadata;
3. validates the value against the known Harness registry and the set of Harnesses whose managed integration can reliably stamp identity;
4. converts an accepted value into a typed request-bound Harness identity;
5. strips the raw `X-Benes-Harness` header before every provider/upstream dispatch path;
6. treats blank, unknown, unsupported, duplicated or ambiguous values as no Harness identity;
7. records rejected identity evidence without selecting another Harness or returning a request error solely because the selector was invalid.

A request with no accepted Harness identity uses global sidecar defaults.

If a managed browser-capable client can send this header cross-origin, the listener CORS allow-header contract must include `X-Benes-Harness`. CORS support does not make the value trusted; the same validation and stripping rules apply.

Spoofing the selector must not grant credentials, provider access, permissions, admission bypass, or any other authority. It may only select sidecar policy for a known Harness.

## 3. Server-owned capability registry and identity migration

One server-owned registry defines which Harnesses can participate in per-Harness sidecar policy. The GUI must not infer this from Harness names.

For each Harness the registry exposes the equivalent of:

```text
identityStampable: bool
webSearch: bool
vision: bool
```

Semantics:

- `identityStampable=true` means Benes can reliably configure that managed Harness to send `X-Benes-Harness`.
- `webSearch=true` means that managed Harness path can reach a Benes request path where web-search sidecar policy can affect behavior.
- `vision=true` means that managed Harness path can reach a Benes request path where vision sidecar policy can affect behavior.
- If `identityStampable=false`, no sidecar override controls are exposed.
- If only one modality is meaningful, only that modality is exposed.

The Harness probe/API projects these server-owned capabilities to the dashboard alongside the Harness settings. Runtime capability checks remain authoritative even if UI state is stale.

### Existing managed Harnesses

The identity marker becomes part of the desired managed configuration for every stampable Harness. Injection must be deterministic and idempotent.

For Harness configurations created before #257:

- a managed config that otherwise matches Benes but lacks the required identity marker is considered stale/update-needed rather than silently “identity ready”;
- the existing apply/refresh path writes the marker;
- existing auto-apply behavior may repair the drift where that Harness already supports auto-apply;
- disabling or restoring a managed integration continues to honor the existing snapshot/restore contract and must not leave an orphaned Benes identity header behind;
- a conflicting user-owned `X-Benes-Harness` value must not be silently overwritten as if it were Benes-owned metadata. The integration path must surface conflict/refusal through its normal conventions.

The dashboard may allow the user to save an override for a stampable but stale Harness, but it must not claim that the override is active on requests until the managed identity marker is present. The Harness status should make the existing reapply/update-needed state visible.

This migration is required because persistence alone is not enough: pre-#257 managed configs otherwise cannot identify their requests to the policy runtime.

## 4. Canonical effective-policy resolver

One Benes-owned runtime policy module resolves the effective result for web search and vision. Individual request handlers must not implement independent merge rules.

Conceptually the resolver consumes:

- current global enabled state for the modality;
- optional validated Harness identity;
- that Harness's optional override;
- explicit provider-native request intent;
- proven provider/model/request-path capability;
- proven sidecar candidate availability where sidecar mediation would be required.

It returns inspectable effective capability state including at least:

- enabled/disabled result;
- accepted Harness ID, if any;
- source/reason such as `request_native`, `harness_override`, `global`, `unsupported` or `policy_disabled`.

The resolution algorithm is deterministic:

1. If the request explicitly asks for a provider-native capability and the selected provider path has proven native support, choose `request_native`. Sidecar activation policy does not suppress proven native behavior.
2. Otherwise determine the desired sidecar activation bit: explicit Harness override if present, else current global enabled state.
3. If desired activation is false, return disabled with source `harness_override` or `global` as appropriate. Do not run a sidecar fallback.
4. If desired activation is true but the required sidecar/provider/model/request-path capability is unsupported or unproven, return `unsupported`; policy cannot force an impossible path.
5. If desired activation is true and the required sidecar path is proven, enable sidecar mediation with source `harness_override` or `global` as appropriate.

This makes the previously shorthand precedence explicit:

```text
proven provider-native request
> Harness activation override
> current global activation default
> proven sidecar/runtime capability
```

The capability check is a hard final gate, not a weaker preference.

If native intent is present but native support is not proven, the request may use the sidecar only when the resolved sidecar activation is enabled and a proven sidecar path exists. If sidecar activation is disabled, fail rather than silently escaping through the sidecar.

## 5. Web-search runtime behavior

Web search and vision are independent policy dimensions.

For web search:

- If the incoming request explicitly requests provider-native search and the selected provider path has proven native-search support, preserve native behavior. Harness/global sidecar policy does not suppress it.
- Otherwise, if a hosted web-search tool needs Benes mediation, resolve the Harness override and current global default through the canonical resolver.
- Effective sidecar `enabled` uses the existing web-search sidecar execution path.
- Effective sidecar `disabled` must not silently re-enable the sidecar. If the request cannot otherwise be satisfied, return the stable web-search-unavailable failure rather than ignoring the requested tool.
- Native intent without proven native support may fall back to the sidecar only when the resolver returns an enabled and proven sidecar path.

The existing Responses, Chat Completions, and Anthropic request handlers must consume the same effective policy rather than diverging.

The Codex alpha-search path must continue to obey the same global web-search authority. If it can carry a validated Harness identity, it uses the same resolver; if it cannot, it remains global-only rather than gaining heuristic identity inference.

## 6. Vision runtime behavior

The repository already has a vision sidecar implementation and sidecar capability catalog, but no production data-plane call site currently consumes the vision sidecar. Because #257 requires vision overrides to affect the actual runtime path, the minimal production vision seam is part of this issue.

For a request containing image parts:

1. If the selected target model has proven native vision capability, send images unchanged. Native capability wins.
2. If the target model is proven text-only, resolve the effective vision sidecar policy.
   - `enabled`: select the configured proven vision describer, describe the bounded image payloads, and replace the image parts with deterministic, clearly marked text description parts before target-provider dispatch.
   - `disabled`: refuse before target dispatch with a stable vision-unavailable error. Never silently drop images.
3. If target-model vision capability is unknown, preserve the request and native provider behavior. Unknown is not treated as proven text-only.

The image transformation operation belongs in the vision sidecar/runtime package and is shared by request handlers. It returns either the unchanged request, a fully transformed request, or a typed refusal. Handlers must not implement independent image rewriting.

Transformation requirements:

- preserve message roles and the relative order of non-image content;
- preserve deterministic image-to-description correspondence when multiple images are present, regardless of whether the implementation batches sidecar calls;
- replace each image position with deterministic text that is visibly machine-generated image description context;
- never forward the original raw image part to a target that was proven text-only after successful transformation;
- never partially transform and dispatch after a sidecar failure;
- honor the existing global vision-sidecar bounds/configuration that apply to runtime execution, including image count/size and configured timeout/description limits where present;
- exceeding a bound is a typed pre-dispatch failure, not silent truncation of image semantics.

The existing sidecar image input contract remains authoritative. #257 does not introduce remote image fetching or broaden accepted image input beyond the bounded current contract.

If the vision sidecar fails while preparing a request for a proven text-only target, the target request is not dispatched.

## 7. Runtime refresh model

`harnesses.json` remains canonical on disk, but request processing must not read it on every completion.

Runtime behavior:

- handler/bootstrap startup loads a read-only per-Harness override snapshot;
- the snapshot contains only Harness overrides and validation state, never copied global sidecar defaults;
- `PUT /api/harnesses/settings` validates and writes the settings file first;
- only after a successful persisted write does the server atomically replace the live override snapshot;
- a failed save leaves the previous in-memory override snapshot active;
- request resolution reads the current immutable/atomically published override snapshot without per-request `harnesses.json` I/O;
- the resolver obtains current global sidecar state from the same live authority used by the global sidecar settings surface, so global toggles affect inherited Harnesses immediately.

This keeps persistence and runtime effect synchronized while preserving the existing optional-subsystem performance rule.

## 8. Harnesses UI

Per-Harness sidecar policy is edited in the existing selected Harness detail. It does not get a new top-level workspace and does not move global defaults out of the global Sidecars settings surface.

The generic Harness Configuration area gains a compact Sidecar policy subsection for capabilities exposed by the server registry.

For each supported modality the control is tri-state:

- `Use global`
- `On`
- `Off`

The UI displays the configured-effective result and its source, for example:

```text
Configured: On · Harness override
```

or

```text
Configured: Off · Global
```

“Configured” is deliberate: a specific request can still resolve to `request_native` or `unsupported` depending on its selected provider/model. Request/decision evidence is authoritative for the actual runtime outcome.

Rules:

- Unsupported/non-stampable modalities are omitted rather than shown as controls that cannot affect runtime behavior.
- A stampable Harness whose managed config needs reapply may expose the control but must also retain its existing update-needed/reapply state; the UI must not label the override request-active until identity stamping is current.
- No per-Harness model/backend/reasoning/timeout selectors are added.
- Sidecar policy is server-authoritative. Do not add the sidecar override fields to `benes.harnesses.settings.v1` or another browser-local persistence layer.
- Sidecar saves use the server response as canonical state. Optimistic UI is allowed only if failure rolls back to that server state.
- Saving follows existing Harnesses toast/error conventions.
- Server probe/settings data remains authoritative after refresh.
- UI changes stay inside existing Harness detail composition and must not broaden into #234 shared-shell reauthoring.

## 9. Observability

Effective sidecar decisions are emitted through existing request/diagnostic evidence rather than a new logging subsystem.

For each applicable request, evidence should make it possible to determine:

- whether a Harness identity was accepted or rejected;
- the accepted Harness ID when present;
- configured web-search policy and source;
- configured vision policy and source;
- actual request resolution (`request_native`, sidecar enabled, policy disabled, or unsupported);
- whether a sidecar actually ran;
- if it did not run, whether the reason was explicit disablement, global disablement, native capability, unsupported/unproven capability, missing identity, or stale/non-active identity stamping where knowable.

The evidence schema should be structured and stable enough for tests to assert fields without parsing prose log messages.

Evidence must not include request bodies, image payloads, credentials, or other sensitive input.

## 10. Failure semantics

- Invalid/ambiguous `X-Benes-Harness`: ignore as identity, record rejected identity evidence, and use global policy. Do not return 4xx solely for invalid selector metadata.
- Invalid API override value: return `400 invalid_body`; do not mutate disk or the live override snapshot.
- Manually corrupted Harness settings: use the existing unreadable-settings failure surface; never coerce unsupported persisted values into enabled overrides.
- Override `enabled` with unsupported runtime/provider capability: unsupported wins and the normal capability error is returned.
- Override `disabled`: never silently run the sidecar as a fallback.
- Vision sidecar transform failure for a proven text-only target: fail before target dispatch.
- Native capability path: preserve unchanged request behavior when the resolver selects `request_native`.
- A stale managed Harness without an active identity marker cannot receive a per-Harness runtime decision; until reapply it behaves as an unidentified request and therefore follows global policy. The UI/status must not misrepresent the saved override as request-active in that state.

## 11. Required tests

Focused contract tests must cover at least:

1. persistence and inheritance:
   - old-file compatibility;
   - absent override inheritance;
   - explicit enabled/disabled;
   - clear-to-inherit removes the field;
   - partial API patch semantics distinguish omitted/no-change from `null`/clear;
   - invalid API values are rejected without disk/runtime mutation;
2. live global authority:
   - inheriting Harness follows a global enabled-state change without restart/resave;
   - explicit Harness override remains effective across global changes;
   - clearing the override resumes the latest global state;
3. identity:
   - known stampable Harness IDs accepted;
   - unknown, non-stampable, duplicate or ambiguous IDs rejected;
   - raw identity header never reaches provider dispatch across relevant protocol paths;
   - CORS allow-header behavior covers the marker where required;
4. managed identity migration:
   - apply/refresh stamps the correct Harness ID idempotently;
   - pre-#257 managed config without the marker is update-needed/stale rather than identity-ready;
   - disable/restore does not leave an orphaned marker;
   - conflicting user-owned marker is refused rather than overwritten;
5. resolver matrix for both modalities:
   - proven native request;
   - native intent without proven native support;
   - Harness override enabled/disabled;
   - global enabled/disabled;
   - sidecar candidate supported/unsupported/unproven;
6. capability registry projection and UI omission of meaningless controls;
7. web-search runtime behavior under inherit/on/off;
8. vision runtime behavior:
   - proven native vision remains unchanged;
   - proven text-only + enabled transforms before dispatch;
   - proven text-only + disabled refuses before dispatch;
   - unknown vision capability remains unchanged/native;
   - multiple images retain deterministic order/correspondence;
   - configured bounds are enforced;
   - transform failure prevents target dispatch;
9. request evidence contains stable configured policy, actual resolution and accepted/rejected Harness identity where applicable;
10. GUI tri-state interaction, configured-state rendering, update-needed identity state, omitted unsupported controls, and save failure/success behavior;
11. GUI sidecar policy does not create a browser-local source of truth;
12. regression guard that Claude-specific nested sidecar fields do not become supported sources of truth again.

## 12. Verification, docs and delivery

Implementation must use focused tests during development, then run the repository-required review gates because this issue changes shared runtime/server behavior:

```bash
go test ./...
go vet ./...
cd gui
npm run lint
npm run lint:i18n
npm run build
cd ..
npm run privacy:scan
```

Update the public Sidecars documentation to describe generic per-Harness activation overrides, inheritance, identity/reapply behavior and the distinction between global sidecar configuration and Harness activation policy. Do not document a Harness override for capabilities that the shipped registry cannot actually expose.

Before review-ready acceptance, run exact-head local PR CI:

```bash
npm run ci:pr -- <PR_NUMBER>
```

The implementation PR targets `dev`, follows the repository PR template, and links #257. Because the design includes visible Harnesses UI changes, the PR must provide the required screenshot or receive an explicit `gui-screenshot-waived` label.

Hosted Actions failures with no executed steps are infrastructure evidence only; they do not substitute for exact-head local verification.

Before review-ready status, rebase/retarget onto the then-current `dev` as required by repository policy. Resolve any #234/#256 overlap by preserving their landed ownership and keeping this branch limited to #257 integration seams.

## 13. Acceptance criteria

#257 is complete only when all of the following are true:

- sidecar overrides have one generic per-Harness policy contract;
- inheritance from global settings is explicit, live and contract-tested;
- explicit Harness overrides can change global activation defaults in either direction without copying global sidecar configuration;
- management API clear/set/no-change semantics are unambiguous;
- web-search and vision overrides affect the actual runtime path;
- clearing an override restores live global inheritance;
- provider-native request intent and unsupported-native fallback behavior have explicit precedence;
- unsupported Harness/capability combinations fail closed or omit the control;
- existing managed Harness configs have a defined identity-stamp migration/reapply path;
- effective policy is observable/testable rather than persistence-only;
- the UI distinguishes configured policy from per-request runtime resolution;
- Harnesses owns the per-Harness UI while global defaults remain global;
- no browser-local sidecar-policy source of truth is added;
- no Claude-specific nested sidecar source-of-truth returns;
- no heuristic Harness identity inference is introduced;
- focused tests, repository checks, privacy scan, GUI checks, docs build where changed, and exact-head local PR CI pass;
- no #234 or #256 work is absorbed into this branch beyond a minimal integration seam strictly required by #257.
