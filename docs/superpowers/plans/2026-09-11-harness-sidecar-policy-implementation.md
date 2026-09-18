# Issue #257 — Generic per-Harness sidecar policy implementation plan

> **For the implementing agent:** Use the `superpowers:test-driven-development` workflow for behavior changes and `superpowers:verification-before-completion` before claiming the branch is ready. Do not merge this branch. Do not absorb #234 or #256 work.

Date: 2026-09-11
Issue: #257
Approved design: `docs/superpowers/specs/2026-09-11-harness-sidecar-policy-design.md`
Design head: `dfbb2cd7d6186bfa83bc6f18b1f0a531b5424a25`
Frozen implementation base: `dev@4b6d33c2583545d4c55baf3d6ec43a34a9b41d6e`
Branch: `feat/257-harness-sidecar-policy`

## Goal

Ship one generic, server-authoritative per-Harness sidecar activation policy for web search and vision. The feature must:

- persist only `enabled` / `disabled` overrides while representing inheritance by field absence;
- identify eligible managed Harness requests explicitly through `X-Benes-Harness` rather than inference;
- resolve native request intent, Harness override, current global default, and proven capability through one canonical resolver;
- make both web-search and vision policy affect real runtime behavior;
- expose configured policy and structured request-time evidence;
- edit the override in the existing Harness detail UI;
- preserve global sidecar ownership for model/backend/limits and keep inherited global toggles live;
- keep sidecar policy out of browser local storage;
- leave #234 GUI work and #256 service-tier policy untouched.

## Architecture

Use a small dependency direction:

```text
internal/harnesspolicy
        ^
        |
internal/harnessboard        internal/server
        ^                         ^
        |                         |
managed config writers       request runtime / evidence
                                  ^
                                  |
                           Harnesses GUI API
```

`internal/harnesspolicy` owns pure activation types and the resolver only. `internal/harnessboard` owns persisted Harness settings and may import the pure package. `internal/server` owns the stampability/modality registry, request identity parsing, the in-memory override snapshot, current-global lookup, sidecar application, and API projection. This avoids circular imports and keeps persistence from becoming the runtime decision engine.

The server runtime snapshot contains **only per-Harness overrides**. Global sidecar activation is read from the current global sidecar authority at decision time, so inheritance is live.

Vision transformation is per selected route/candidate from an immutable original parsed request. Never mutate a request once and reuse that transformed request across combo/failover candidates with different vision capabilities.

## Known collision boundaries

Avoid unrelated edits in files being actively reauthored under #234, especially shared GUI shell/utilities such as `data-surface.tsx`, `section-tabs.tsx`, `use-copy-feedback.ts`, `main.tsx`, `grok-groups.ts`, `logs-surface-filter.ts`, and `models-shared.ts`. Prefer a new Harness-local component for #257.

Do not alter service-tier / priority / fast-mode policy owned by #256.

Before final review readiness, rebase or otherwise reconcile onto the then-current `dev` and re-run the exact-head verification. During that reconciliation, preserve the landed ownership of #234/#256 rather than copying their work into #257.

---

## Task 1 — Create the pure sidecar policy contract and resolver

**Files**

- Create: `internal/harnesspolicy/policy.go`
- Create: `internal/harnesspolicy/policy_test.go`

### Step 1: write the failing table tests

Define the smallest domain model needed by the rest of the feature, for example:

```go
type Activation string

const (
    ActivationEnabled  Activation = "enabled"
    ActivationDisabled Activation = "disabled"
)

type Overrides struct {
    WebSearch *Activation `json:"webSearch,omitempty"`
    Vision    *Activation `json:"vision,omitempty"`
}

type Source string

const (
    SourceRequestNative   Source = "request_native"
    SourceHarnessOverride Source = "harness_override"
    SourceGlobal          Source = "global"
    SourceUnsupported     Source = "unsupported"
)

type ResolveInput struct {
    GlobalEnabled   bool
    Override        *Activation
    NativeRequested bool
    NativeProven    bool
    SidecarProven   bool
}

type Decision struct {
    Enabled bool
    Native  bool
    Source  Source
}
```

Test the full matrix required by the design:

- native requested + native proven wins regardless of Harness/global activation;
- explicit Harness enabled overrides global disabled when a sidecar path is proven;
- explicit Harness disabled overrides global enabled;
- absent override follows global enabled/disabled;
- desired enabled + unproven sidecar returns unsupported;
- native requested but not proven can use sidecar only when policy resolves enabled and a sidecar is proven;
- invalid `Activation` values fail validation and are never treated as enabled.

Also test `Overrides.Validate()` independently for web search and vision.

### Step 2: prove the tests fail for the intended reason

Run:

```bash
go test ./internal/harnesspolicy
```

Expected: package/build/test failure because the contract does not exist yet. Do not proceed if failure is unrelated.

### Step 3: implement the minimal pure resolver

Implement only:

- activation validation;
- independent web-search / vision override values;
- the deterministic resolver algorithm approved in the design;
- no disk, HTTP, global config, provider, Harness-ID, or GUI dependencies.

Do not put model/backend/timeout/reasoning fields into this package.

### Step 4: make the focused tests green

Run:

```bash
go test ./internal/harnesspolicy
```

Expected: PASS.

### Step 5: commit

```bash
git add internal/harnesspolicy
git commit -m "feat: define harness sidecar policy resolver"
```

**Stop condition:** if the resolver needs imports from `internal/server`, `internal/harnessboard`, provider packages, or GUI-specific concepts, the abstraction is too coupled. Fix the boundary before continuing.

---

## Task 2 — Extend `harnesses.json` persistence and define exact API patch semantics

**Files**

- Modify: `internal/harnessboard/settings.go`
- Modify/Create focused tests near: `internal/harnessboard/settings_test.go`
- Modify: `internal/server/harnesses_api.go`
- Modify/Create focused tests near: `internal/server/harnesses_api_test.go`

### Step 1: write failing persistence tests

Cover:

1. an old file containing only the existing four booleans loads unchanged;
2. `sidecars.webSearch="enabled"` and `sidecars.vision="disabled"` round-trip;
3. nil fields serialize as absence;
4. an empty `sidecars` object is omitted;
5. an unsupported persisted activation causes the existing unreadable-settings failure rather than coercion;
6. saving one Harness does not mutate another Harness row.

Extend `harnessboard.Settings` with a sidecar override value owned by `internal/harnesspolicy`.

### Step 2: write failing management API tests

For `PUT /api/harnesses/settings`, assert all four patch states distinctly:

```json
{"clientId":"opencode"}
```

leaves sidecars unchanged;

```json
{"clientId":"opencode","sidecars":{"webSearch":"enabled"}}
```

sets only web search;

```json
{"clientId":"opencode","sidecars":{"vision":null}}
```

clears only vision to inheritance;

and unsupported strings/types return `400 invalid_body` without mutating disk.

Use raw-message/presence-aware decoding so JSON omission and JSON `null` cannot collapse into the same operation.

### Step 3: run focused failures

```bash
go test ./internal/harnessboard ./internal/server
```

Expected: new tests fail on missing sidecar fields/patch handling.

### Step 4: implement persistence + patch parsing

Requirements:

- persisted inheritance remains field absence, never a stored `"inherit"` string or `null`;
- management API `null` means clear only at the patch boundary;
- validate all requested sidecar patch operations before calling `PutSettings`;
- failed validation performs no write;
- retain the atomic `0600` settings write path;
- response returns the canonical saved settings. Effective projection is added in Task 5 when the runtime registry exists.

### Step 5: focused verification

```bash
go test ./internal/harnessboard ./internal/server
```

Expected: PASS.

### Step 6: commit

```bash
git add internal/harnessboard internal/server/harnesses_api.go internal/server/*harnesses*test.go
git commit -m "feat: persist harness sidecar overrides"
```

**Stop condition:** do not solve malformed-row recovery by silently dropping an invalid override. The existing unreadable-settings error surface is safer and matches the approved design.

---

## Task 3 — Add the server-owned capability registry, live override snapshot, and Harness API projection

**Files**

- Create: `internal/server/harness_sidecar_policy.go`
- Create: `internal/server/harness_sidecar_policy_test.go`
- Modify: `internal/server/responses.go` or the file that owns `handler` / `Options` construction
- Modify: `internal/server/harnesses_api.go`
- Modify: `internal/harnessboard/probe.go` only if a neutral DTO field is needed there; prefer server-side projection when possible

### Step 1: write registry tests first

Create one explicit registry whose row contains at least:

```go
type harnessSidecarCapabilities struct {
    IdentityStampable bool
    WebSearch         bool
    Vision            bool
}
```

Tests must prove:

- every registry ID is a canonical `harnessboard.Known` ID;
- `IdentityStampable=false` implies both override controls are omitted;
- modality flags are independent;
- unknown IDs fail closed;
- no UI consumer has to derive capability from a Harness name.

Do **not** mark an entry stampable merely because it is a known Harness. Task 4 supplies the evidence for each `true` row.

### Step 2: write runtime snapshot tests

Create a small immutable/atomically published runtime snapshot that contains only `map[harnessID]Overrides`.

Test:

- startup load from `harnesses.json`;
- swap only after successful persistence;
- failed save leaves old snapshot active;
- snapshot contains no copied global activation state;
- clearing an override removes it from the next snapshot.

Keep this runtime distinct from any service-tier/general policy runtime that #256 may modify.

### Step 3: write live-global inheritance tests

Using the existing global sidecar settings authority, assert:

1. Harness override absent + global web search ON => configured ON/global;
2. mutate global web search to OFF through the normal settings path;
3. without reloading/re-saving Harness settings, the same Harness now resolves configured OFF/global;
4. explicit Harness enabled stays enabled across that global toggle;
5. clearing it immediately resumes the current global OFF value.

Repeat the essential inheritance case for vision.

### Step 4: write API projection tests

`GET /api/harnesses` should project, per Harness:

- canonical persisted sidecar overrides;
- server capability flags;
- configured-effective activation and source (`harness_override` or `global`);
- whether identity stamping is currently usable/current where that status can be proven by existing integration state.

Do not claim a request-specific provider/model outcome here.

### Step 5: run focused failures

```bash
go test ./internal/server ./internal/harnessboard ./internal/harnesspolicy
```

### Step 6: implement minimally

Wire startup loading into `NewHandler` / handler initialization without per-request file reads. After successful settings PUT, swap the runtime override snapshot from the saved canonical state.

For configured-effective projection, read the **current** global enabled values from the same config authority used by `/api/sidecar-settings`; never cache those values inside the Harness snapshot.

### Step 7: focused verification and commit

```bash
go test ./internal/server ./internal/harnessboard ./internal/harnesspolicy

git add internal/server internal/harnessboard
git commit -m "feat: expose effective harness sidecar policy"
```

**Stop condition:** if a global sidecar toggle requires a Harness resave/restart before inherited state changes, the implementation is wrong. Do not proceed until inheritance is live.

---

## Task 4 — Stamp explicit Harness identity into only proven managed integrations

**Files likely involved**

- Modify: `internal/export/clients.go`
- Modify focused exporter tests under: `internal/export/*_test.go`
- Modify: `internal/integrations/writer.go` only if existing desired-state/conflict logic needs a generic marker hook
- Modify focused integration tests under: `internal/integrations/*_test.go`
- Modify: `internal/codexrestore/inject.go`
- Modify: `internal/codexrestore/inject_test.go`
- Modify: `internal/codexrestore/restore_test.go` if restore coverage needs extension
- Modify: `internal/grok/inject.go`
- Modify Grok injection tests
- Conditionally modify: `internal/claude/env.go`, `internal/claude/env_test.go` **only if a currently supported Claude Code custom-header contract is proven**
- Modify: `internal/server/harness_sidecar_policy.go` registry rows after each integration has a passing generated-config contract test

### Step 1: audit each managed Harness before changing the registry

Treat `identityStampable=true` as an evidence claim. For each Harness, locate its actual desired-config/injection path and answer:

1. Can Benes deterministically emit a custom request header to its Benes endpoint?
2. Can the writer preserve unrelated user headers/config?
3. Can it detect/refuse a conflicting user-owned `X-Benes-Harness` value?
4. Does normal disable/restore remove Benes-owned state through the existing snapshot contract?
5. Can a focused test inspect the generated desired config and prove the header exists?

At the frozen base, likely proven candidates include:

- `codex`: managed provider already emits Benes provider headers;
- `grok`: managed provider already emits `extra_headers`;
- `opencode`: exporter has a header map;
- `hermes`: exporter has `extra_headers`;
- `openclaw`: exporter has a header map;
- `dsh`: exporter has a header map.

Do **not** assume Pi, Prime, OMP, Kimi, Gajae, Mcode, Claude Desktop, or another Harness is stampable without a tested supported field.

For `claude`, first verify an official/current Claude Code custom-header mechanism. If that cannot be proven, leave `claude.identityStampable=false` and omit its controls. Do not invent or depend on undocumented environment variables.

### Step 2: write failing desired-config tests per proven Harness

For every candidate that will become stampable, assert exact desired output includes:

```text
X-Benes-Harness: <canonical-id>
```

or the format/casing required by that client.

Also assert existing authentication/header material is preserved. Where the managed format can already contain user headers, add a conflict test proving a different user-owned `X-Benes-Harness` is not silently overwritten.

For Codex, preserve existing `X-Benes-Surface` semantics; add the new Harness marker rather than repurposing that header.

### Step 3: run focused failures

```bash
go test ./internal/export ./internal/integrations ./internal/codexrestore ./internal/grok ./internal/claude
```

Expected: only tests for paths actually changed should fail initially.

### Step 4: implement marker emission and migration behavior

Requirements:

- desired config always emits the canonical marker for supported Harnesses;
- emission is deterministic and idempotent;
- a pre-#257 Benes-managed config missing only the marker compares stale/update-needed against the new desired config;
- apply/refresh writes the marker;
- existing snapshot/restore removes it when the integration is restored/disabled;
- user-owned conflicting marker follows the existing conflict/refusal path.

### Step 5: update registry only after evidence exists

Set `IdentityStampable=true` only for Harnesses with the passing generated-config contract above. Set web-search/vision flags only where the Harness request protocol can reach the corresponding normalized runtime path; header support by itself is insufficient.

### Step 6: focused verification and commit

```bash
go test ./internal/export ./internal/integrations ./internal/codexrestore ./internal/grok ./internal/claude ./internal/server

git add internal/export internal/integrations internal/codexrestore internal/grok internal/claude internal/server/harness_sidecar_policy.go
git commit -m "feat: stamp managed harness request identity"
```

**Stop conditions:**

- no registry `IdentityStampable=true` without a generated-config test;
- no modality flag `true` without a real normalized runtime path;
- no undocumented Claude custom-header mechanism;
- no writer may overwrite an unrelated user-owned conflicting Harness marker.

---

## Task 5 — Parse, validate, and strip `X-Benes-Harness` at the data-plane boundary

**Files**

- Create or modify: `internal/server/harness_identity.go`
- Create: `internal/server/harness_identity_test.go`
- Modify CORS/admission header list in the existing server admission/CORS file
- Modify route-boundary tests for Responses, Chat Completions, Anthropic Messages, and any identity-capable alpha-search path

### Step 1: write boundary tests

Test:

- exactly one known+stampable header is accepted;
- blank, unknown, non-stampable, duplicate, or comma/ambiguous values yield no Harness identity;
- invalid selector alone does not cause a 4xx;
- invalid selector falls back to global policy;
- `X-Benes-Harness` is deleted from raw request headers after parsing;
- no provider dispatch or captured upstream request contains `X-Benes-Harness`;
- CORS preflight allows the header where a browser-capable managed client needs it.

Use a private typed context key or explicit request metadata structure; do not pass the raw string header deep into routing code.

### Step 2: run focused failures

```bash
go test ./internal/server
```

### Step 3: implement the boundary parser

Implement one helper used before data-plane handlers dispatch:

```go
identity, status := parseHarnessIdentity(r.Header.Values("X-Benes-Harness"), registry)
r.Header.Del("X-Benes-Harness")
ctx := contextWithHarnessIdentity(r.Context(), identity, status)
```

Exact implementation may differ, but all routes must share the same validation semantics.

Do not attach authority/credentials to this identity.

### Step 4: focused verification and commit

```bash
go test ./internal/server

git add internal/server
git commit -m "feat: bind harness identity to data-plane requests"
```

**Stop condition:** if any accepted/rejected `X-Benes-Harness` value can reach an upstream provider as an ordinary forwarded header, stop and fix the boundary before runtime policy integration.

---

## Task 6 — Route web-search sidecar activation through the canonical resolver

**Files**

- Modify: `internal/server/chat.go`
- Modify: `internal/server/responses.go`
- Modify: `internal/server/anthropic.go`
- Modify the shared web-search helper file where `webSearchClient` / sidecar enablement is implemented
- Modify: `internal/server/alpha_search.go` only as required by the approved global/identity rule
- Add/modify focused web-search tests under `internal/server/*web*test.go`, Chat/Responses/Anthropic tests

### Step 1: write end-to-end runtime matrix tests

For each normalized request surface that currently invokes `websearch.OpenLoop`, exercise one Harness identity and prove:

- inherit + global ON runs sidecar mediation;
- inherit + global OFF does not run sidecar mediation;
- Harness ON + global OFF runs sidecar mediation when a proven sidecar candidate exists;
- Harness OFF + global ON does not run sidecar mediation;
- clear override immediately returns to global behavior;
- explicit provider-native request + proven native support remains native even when Harness sidecar override is OFF;
- native requested but unproven + sidecar OFF fails instead of silently enabling mediation;
- policy ON but no proven sidecar candidate fails closed;
- no Harness identity uses global behavior.

Capture whether the sidecar client/open-loop was invoked, not merely the persisted state.

### Step 2: run focused failures

```bash
go test ./internal/server ./internal/sidecar/websearch
```

### Step 3: centralize the decision before `OpenLoop`

Replace independent global-only checks with one helper that builds `harnesspolicy.ResolveInput` from:

- current global sidecar activation;
- typed request Harness identity;
- current override snapshot;
- native request intent and proven native support;
- proven sidecar candidate availability.

Keep provider-specific web-search selection/model/backend in the existing global sidecar selection code. The Harness policy changes only the activation decision.

The three main request surfaces consume the same helper/result.

For `/v1/alpha/search`, keep existing global authority. Apply a Harness override only if that request actually carries an accepted Harness identity; otherwise remain global-only.

### Step 4: focused verification and commit

```bash
go test ./internal/server ./internal/sidecar/websearch

git add internal/server
git commit -m "feat: apply harness policy to web search sidecars"
```

**Stop condition:** a settings-only test is insufficient. At least one test per normalized surface must prove the actual sidecar execution path changes.

---

## Task 7 — Build a deterministic, all-or-nothing vision transformation helper

**Files**

- Modify: `internal/sidecar/vision/describe.go` only where reusable client behavior belongs
- Create: `internal/sidecar/vision/transform.go`
- Create: `internal/sidecar/vision/transform_test.go`

### Step 1: write transformation contract tests

Use `protocol.ParsedRequest` / message content fixtures with interleaved text and multiple image parts.

Prove:

- requests with no images are unchanged;
- roles and non-image part order are preserved;
- each image position becomes one deterministic marked text description;
- image #1/#2 correspondence remains deterministic;
- no raw image part remains after successful transform;
- failure on any image yields no transformed request for dispatch;
- too many images / oversized or malformed data URLs return typed failures;
- remote image URLs are not fetched/accepted;
- global max-description/timeout/other runtime bounds are honored rather than silently ignored.

Prefer one image per `Describe` invocation if that is the simplest way to preserve exact positional correspondence; enforce the turn-level maximum separately. Do not rely on a single free-form multi-image description whose image-to-text mapping is ambiguous.

### Step 2: run focused failure

```bash
go test ./internal/sidecar/vision
```

### Step 3: implement the helper

Create a transformation API that accepts an immutable parsed request plus a describer interface/config and returns a fresh request or typed error. It must never mutate the caller's original request in place.

The replacement text should be deterministic in wrapper format, e.g. conceptually:

```text
[Benes image description]
<description>
[/Benes image description]
```

Choose one stable format and test it exactly. Do not include original base64 payloads in replacement text or diagnostics.

### Step 4: focused verification and commit

```bash
go test ./internal/sidecar/vision

git add internal/sidecar/vision
git commit -m "feat: transform images through vision sidecar"
```

**Stop condition:** if transformation mutates the original request or can partially succeed and still permit target dispatch, stop and fix it before server integration.

---

## Task 8 — Integrate vision at the actual per-candidate pre-provider seam

**Files**

- Modify the shared model-turn/candidate-dispatch file that owns `runModelTurn` / per-candidate provider invocation
- Modify: `internal/server/responses.go`, `internal/server/chat.go`, `internal/server/anthropic.go` only to pass immutable original request / request metadata if the shared seam needs it
- Add focused candidate/failover tests in the owning server test file(s)
- Possibly add a small server helper: `internal/server/vision_sidecar.go`

### Step 1: locate and prove the correct seam before editing

Find the last shared point where all of these are simultaneously known:

- the selected candidate/provider/model for this **attempt**;
- the candidate's catalog vision capability (`true`, `false`, or `unknown`);
- the immutable original parsed request;
- typed Harness identity and effective policy inputs;
- the provider has not yet been called.

This must be per attempt, because combo/failover candidates can differ in vision capability.

If no clean shared seam exists, first create a minimal candidate-dispatch seam and move the existing provider call through it. Add characterization tests before behavior changes. Do **not** duplicate three independent vision rewrites in Responses/Chat/Anthropic handlers.

### Step 2: write failing per-candidate tests

Cover:

1. image request + target `Vision=true` => original image reaches provider unchanged; vision sidecar not called;
2. image request + target `Vision=false` + policy enabled => sidecar called and target receives transformed text only;
3. same target + policy disabled => typed pre-dispatch error, target provider call count remains zero;
4. target `Vision=unknown` => original request passes through; no sidecar guess;
5. sidecar transform failure => target call count zero;
6. combo attempt A text-only fails before output then attempt B native-vision receives the **original image request**, not A's transformed copy;
7. reverse ordering (native-vision candidate then text-only fallback where relevant) also starts each attempt from immutable original input;
8. Harness override ON/OFF changes the text-only candidate behavior while global config remains otherwise identical.

### Step 3: run focused failures

```bash
go test ./internal/server ./internal/sidecar/vision
```

### Step 4: implement per-attempt vision policy

At the selected-candidate seam:

- inspect whether request contains images;
- if no images, no sidecar work;
- if catalog says native vision true, preserve request;
- if unknown, preserve request;
- if false, resolve Harness/global activation and proven vision sidecar candidate;
- disabled/unsupported => typed refusal before target dispatch;
- enabled/proven => transform a fresh clone and dispatch only that attempt's transformed request.

Use the current global vision model/backend/limits from the global sidecar authority. Do not copy those into Harness settings.

### Step 5: focused verification and commit

```bash
go test ./internal/server ./internal/sidecar/vision

git add internal/server internal/sidecar/vision
git commit -m "feat: apply harness vision policy per route candidate"
```

**Stop conditions:**

- no transform at parse time;
- no reuse of transformed request across candidates;
- no raw image to a proven text-only target after successful transform;
- no target dispatch after transform failure.

---

## Task 9 — Add structured request/decision evidence

**Files**

- Modify the existing request telemetry / decision-evidence structures under `internal/server`
- Modify request timeline/history projection only if that is the existing canonical evidence surface
- Add focused evidence tests next to the telemetry owner

### Step 1: write failing evidence tests

Assert structured fields, not prose parsing, for cases including:

- accepted Harness ID;
- rejected identity selector status without raw untrusted value if unnecessary;
- configured source `harness_override` vs `global`;
- actual resolution `request_native`, sidecar enabled, policy disabled, unsupported;
- whether web-search or vision sidecar actually ran;
- missing identity/global-only case;
- vision transform failure reason category.

Explicitly assert serialized evidence does **not** contain:

- request body/prompt;
- image data URL/base64;
- API keys/tokens;
- arbitrary raw header values.

### Step 2: run focused failure

```bash
go test ./internal/server
```

### Step 3: implement evidence plumbing

Prefer a small nested sidecar-policy evidence object attached to the existing request decision record over adding free-form debug strings.

Emit configuration decision before sidecar execution and update/complete execution outcome through the existing request-lifecycle mechanism without creating a new log subsystem.

### Step 4: focused verification and commit

```bash
go test ./internal/server

git add internal/server
git commit -m "feat: record harness sidecar decision evidence"
```

**Stop condition:** if tests need to parse human-readable diagnostic strings to know why a sidecar ran, the evidence shape is not sufficiently structured.

---

## Task 10 — Add the generic Harness-detail UI without creating a second source of truth

**Files**

- Modify: `gui/src/pages/harnesses/types.ts`
- Modify: `gui/src/pages/harnesses/harness-api.ts`
- Modify: `gui/src/pages/harnesses/HarnessDetail.tsx`
- Create: `gui/src/pages/harnesses/SidecarPolicy.tsx`
- Modify Harness-local CSS file(s), not shared #234-owned utilities
- Modify: `gui/src/i18n/en.ts`
- Add/modify focused GUI policy test, preferably under `gui/scripts/integrations-harnesses-policy.test.ts` or a new Harness-specific sidecar contract test
- Modify `gui/src/pages/harnesses/live.ts` only as needed to keep sidecar state out of local storage

### Step 1: write GUI contract tests first

Test source/runtime contracts for:

- tri-state `Use global / On / Off` for each server-supported modality;
- unsupported/non-stampable modality omitted;
- configured line uses server response, e.g. `Configured: On · Harness override`;
- stale/update-needed Harness does not claim request-active identity;
- choosing `Use global` sends JSON `null` for that modality;
- changing web search does not send or overwrite vision;
- failed save restores/refetches canonical server state;
- sidecar policy fields are **not** written to `benes.harnesses.settings.v1` or any other localStorage key.

Do not test request-specific provider outcome in this component; that belongs to runtime evidence.

### Step 2: run focused GUI policy tests

Use the narrow Node test if a dedicated file is created, for example:

```bash
cd gui
node --experimental-strip-types --test ./scripts/harness-sidecar-policy.test.ts
```

or the existing Harness policy test if extended.

Expected: FAIL before implementation.

### Step 3: implement API types + dedicated component

Extend the Harness DTO with server-projected sidecar capability, override, and configured-effective state. Keep the existing four local preferences intact, but do not merge sidecar policy into `HarnessSettings` if doing so would cause `persistSettings()` to serialize it locally.

Prefer a separate type such as:

```ts
type HarnessSidecarMode = "enabled" | "disabled";

type HarnessSidecarPolicy = {
  capabilities: { webSearch: boolean; vision: boolean };
  override: { webSearch?: HarnessSidecarMode; vision?: HarnessSidecarMode };
  configured: {
    webSearch?: { enabled: boolean; source: "global" | "harness_override" };
    vision?: { enabled: boolean; source: "global" | "harness_override" };
  };
};
```

Exact DTO names may follow existing conventions.

`SidecarPolicy.tsx` owns the generic controls and renders inside the selected Harness's existing Configuration/detail layout. It must not create a new tab/workspace.

### Step 4: implement server-authoritative save behavior

Add a dedicated sidecar policy save call or a carefully separated extension of `saveHarnessSettings` that preserves patch semantics. Do not route these fields through `persistSettings()` / `readStoredSettings()`.

On success, use returned server canonical state or refresh the Harness board. On failure, restore/refetch server state and show the existing Harness toast/error treatment.

### Step 5: run GUI checks

```bash
cd gui
node --experimental-strip-types --test ./scripts/harness-sidecar-policy.test.ts
npm run lint
npm run lint:i18n
npm run build
cd ..
```

If an existing test was extended rather than a new file, substitute its exact path in the first command.

Expected: PASS.

### Step 6: commit

```bash
git add gui

git commit -m "feat(gui): edit harness sidecar overrides"
```

**Stop condition:** any sidecar override appearing in browser localStorage is a design violation. Remove it before proceeding.

---

## Task 11 — Update public docs and add regression guards against retired Claude-specific ownership

**Files**

- Modify: `docs/src/content/docs/use/sidecars.md`
- Modify existing config/Claude docs only if they still describe the retired nested source
- Add/extend a focused Go or Node regression test that searches/parses supported configuration paths for `claudeCode.webSearchSidecar` / `claudeCode.visionSidecar`

### Step 1: write/extend the regression guard

The guard must fail if either retired Claude-specific nested sidecar field becomes a supported source of truth again. Avoid a naive text ban that flags historical migration tests or the design document itself; target supported config schema/API/GUI ownership.

### Step 2: update user-facing sidecar docs

Document:

- global sidecar settings remain model/backend/default authority;
- eligible Harness detail can override activation only;
- `Use global` is live inheritance, not a copied value;
- only supported/stampable Harnesses show controls;
- an existing managed Harness may need normal reapply/refresh when its generated config is stale and lacks the new identity marker;
- native provider capability can supersede sidecar activation for a request;
- no remote image-fetching promise.

Do not document an unsupported Harness as eligible merely because it exists in the board.

### Step 3: docs verification

```bash
cd docs
npm ci
npm run build
cd ..
```

Expected: PASS.

### Step 4: commit

```bash
git add docs internal gui/scripts

git commit -m "docs: explain per-harness sidecar policy"
```

---

## Task 12 — Full verification, current-`dev` reconciliation, PR, screenshot, and exact-head proof

No feature work should be added in this task unless verification exposes a #257 defect.

### Step 1: run focused suites one final time

```bash
go test ./internal/harnesspolicy ./internal/harnessboard ./internal/sidecar/websearch ./internal/sidecar/vision ./internal/server ./internal/export ./internal/integrations ./internal/codexrestore ./internal/grok ./internal/claude
```

Run the dedicated Harness GUI test as well.

### Step 2: run repository-required review gates

From repo root:

```bash
go test ./...
go vet ./...
npm run privacy:scan
npm run lint:gui
npm run build:gui
```

Also keep the docs build from Task 11 green.

Do not claim GitHub Actions success unless the exact commit actually has successful executed jobs.

### Step 3: inspect the branch diff against the frozen base

Confirm:

- no #256 service-tier policy files/behavior were absorbed;
- no #234 shared-GUI reauthoring was absorbed;
- no Claude-specific nested sidecar owner returned;
- no per-Harness model/backend/etc fields appeared;
- no localStorage sidecar authority appeared;
- no remote image fetching appeared;
- no heuristic Harness identity appeared.

### Step 4: reconcile with current `dev`

Fetch current `dev` and compare it to the branch base.

If #234 and/or #256 have landed, rebase/merge according to the repo's ordinary branch policy while preserving their canonical ownership. Resolve only conflicts necessary for #257. Do not recreate superseded versions of their files.

After reconciliation, rerun every check affected by conflict resolution, then rerun:

```bash
go test ./...
go vet ./...
npm run privacy:scan
npm run lint:gui
npm run build:gui
```

### Step 5: open the PR against `dev`

Use the repository PR template and include:

- Summary with the generic policy contract;
- Verification with exact commands/results;
- Checklist fully completed as applicable;
- `Closes #257`;
- explicit note that #234/#256 scopes were not absorbed;
- a screenshot of the Harness detail sidecar policy controls, unless `gui-screenshot-waived` is explicitly applied.

Do not merge.

### Step 6: run canonical exact-head local PR CI

Once the PR number exists:

```bash
npm run ci:pr -- <PR_NUMBER>
```

This must run against the exact current PR head. If any code changes after it passes, rerun it against the new head.

### Step 7: final exact-head review

Before declaring the PR review-ready, record:

- exact head SHA;
- current `dev` SHA / ancestry state;
- focused test results;
- `go test ./...`;
- `go vet ./...`;
- GUI lint/i18n/build;
- privacy scan;
- docs build;
- exact-head `ci:pr` result;
- screenshot/waiver state;
- any hosted CI infrastructure failures separately from local proof.

Use `superpowers:verification-before-completion` before making the completion claim.

---

## Execution invariants

These invariants apply across every task:

1. **No fake runtime effect.** A persisted override is incomplete until a stamped request reaches the real resolver and changes actual web-search/vision behavior.
2. **No copied inheritance.** Never snapshot global activation into Harness state.
3. **No inferred identity.** No User-Agent/model/endpoint heuristics.
4. **No policy-as-auth.** Harness identity may select sidecar policy only.
5. **No upstream leak.** `X-Benes-Harness` never reaches provider/upstream dispatch.
6. **No unproven controls.** Registry flags and GUI controls require a tested real path.
7. **No partial vision rewrite.** Transform is immutable, deterministic, all-or-nothing, and per candidate.
8. **No browser authority.** Sidecar override state stays server-authoritative.
9. **No global sidecar duplication.** Model/backend/reasoning/timeouts/limits remain global.
10. **No scope theft.** #234 and #256 remain separate owners of their domains.

## Recommended commit sequence

1. `feat: define harness sidecar policy resolver`
2. `feat: persist harness sidecar overrides`
3. `feat: expose effective harness sidecar policy`
4. `feat: stamp managed harness request identity`
5. `feat: bind harness identity to data-plane requests`
6. `feat: apply harness policy to web search sidecars`
7. `feat: transform images through vision sidecar`
8. `feat: apply harness vision policy per route candidate`
9. `feat: record harness sidecar decision evidence`
10. `feat(gui): edit harness sidecar overrides`
11. `docs: explain per-harness sidecar policy`

Keep commits logically reviewable. Do not squash away useful implementation checkpoints before review unless the maintainer explicitly asks.
