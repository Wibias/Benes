# Provider hardening design

Date: 2026-08-27

## Purpose

PR #5 established the final Providers workspace shape: fleet overview when no provider is selected, selected-provider Overview / Access / Configuration tabs, a logical OpenAI row that folds ChatGPT OAuth access and OpenAI API-key access, and a capability-driven Access descriptor.

This hardening pass finishes the runtime and data contracts behind that UI. The goal is not another redesign. The four approved provider views remain the product contract.

## Branch reconciliation prerequisite

Repository policy requires feature pull requests to target `dev`, but PR #5 was merged directly to `main`. `dev` and `main` now diverge while the provider implementation exists on `main`.

Before the hardening PR stack can land normally, reconcile the current `main` provider redesign back into `dev`. The hardening branches are based on current `main` so they can be developed against the implementation they fix. Once the reconciliation PR lands, retarget the first hardening PR to `dev`; stacked children then follow the normal repository workflow.

## Delivery split

The work is intentionally split into three reviewable hardening PRs.

### PR 1: provider access and runtime correctness

This PR fixes logical-provider and credential-lane semantics without changing the approved UI layout.

#### OpenAI default access

`Default access` is an access-lane preference, not a cosmetic field.

For a logical OpenAI request:

- an explicit `openai-apikey/...` route remains on the API lane;
- an explicit OpenAI OAuth/Codex-account selection remains on the OAuth lane;
- if only the OAuth lane can serve the model, use OAuth;
- if only the API lane can serve the model, use API;
- if both lanes can serve the model, use the configured `defaultAccess` value;
- if the preferred lane is absent or cannot serve the model, use the lane that can serve it;
- do not introduce cross-lane failover after an upstream failure. Provider/route failover remains a separate concern.

The same logical-lane decision must apply when OpenAI is referenced from combo targets. A combo must not bypass the logical-provider access contract merely because it resolves targets internally.

#### OpenAI API-lane provisioning

The logical OpenAI Access tab may expose the API-key method before the hidden `openai-apikey` connection exists. Adding the first OpenAI API key must therefore be atomic from the user's perspective:

1. ensure the canonical hidden API connection exists with the canonical OpenAI API adapter/base URL;
2. store the key in the secure credential store;
3. publish/select the credential for that connection;
4. return a single success/failure result to the UI.

A partial failure must not leave an unusable credential reference or silently claim success.

Changing `Default access` to API must not create an impossible configuration. The UI/backend should either provision the canonical API lane as part of the operation or reject/disable that choice until an API lane exists. The chosen behavior must be deterministic and covered by tests.

#### Access descriptor cleanup

The generic access descriptor should use method IDs and default-method IDs from the same namespace. The lane ID describes the access lane (`oauth`, `api`, etc.); credential kind remains separate metadata (`oauth`, `api-key`, `local`, etc.).

`selection.mode` must reflect actual provider state. OpenAI Direct mode must not be described as Pool. Strategy-specific fields stay capability-driven:

- quota strategy exposes auto-switch threshold;
- round-robin exposes sticky/rotation behavior where supported;
- fill-first exposes only fields it uses;
- thread affinity remains internal runtime behavior and is never exposed as an editable Access setting.

#### Tests

Add focused regression coverage for:

- both OpenAI lanes available + default OAuth;
- both lanes available + default API;
- model available on OAuth only;
- model available on API only;
- explicit API route remains API;
- explicit OAuth/account route remains OAuth;
- logical OpenAI inside combo resolution;
- first API-key creation when the hidden API connection does not yet exist;
- failed provisioning does not produce a misleading success state;
- descriptor method/default IDs are internally consistent;
- Direct vs Pool mode and strategy-specific capability flags.

### PR 2: provider pacing enforcement and telemetry

This PR makes Configuration pacing controls represent real runtime behavior.

#### Canonical pacing model

The persisted configuration may expose human-friendly RPM and minimum interval values, but runtime enforcement must have one deterministic effective delay per scope. Do not maintain conflicting independent limiters for equivalent settings.

For a provider/API connection:

1. derive the base pacing rule from provider-level configuration;
2. if the resolved model has a model override, derive that override's effective rule;
3. the model override takes precedence for fields it explicitly specifies, with provider-level values as fallback for unspecified fields;
4. disabled pacing means no pacing delay;
5. invalid, zero, or negative values are rejected or normalized according to one documented rule.

RPM must be enforced, not merely displayed. A configured RPM converts to a minimum spacing constraint. When both RPM and `minIntervalMs` are supplied, use the stricter effective spacing so configuration cannot accidentally exceed either limit.

#### Runtime state

The provider pacing implementation must expose read-only runtime telemetry needed by Configuration:

- current queue depth;
- time until the next dispatch slot;
- last resolved model handled by the pacer.

The telemetry endpoint/field must follow existing loopback management API conventions and must not expose request bodies, keys, account identifiers, or other secrets.

For the logical OpenAI page, this pacing data is specifically the API connection lane. ChatGPT/OAuth traffic must not be presented as governed by the OpenAI API pacer.

#### Tests

Add deterministic tests for:

- RPM-only pacing;
- minimum-interval-only pacing;
- both values using the stricter constraint;
- model override precedence;
- provider fallback values for partially specified model overrides;
- disabled pacing;
- concurrent queue behavior;
- telemetry queue/next-slot/last-model state;
- logical OpenAI Configuration reading the API lane rather than OAuth.

### PR 3: authoritative provider workspace data and UI polish

This PR makes the workspace API the source of truth and removes remaining UI/data drift.

#### Authoritative workspace contract

`GET /api/providers/workspace` becomes the normalized source for:

- top summary counts;
- logical provider lifecycle grouping;
- attention issues;
- model/catalogue availability summary;
- downstream impact counts;
- recent provider events;
- provider access descriptors.

The React shell must not independently reimplement lifecycle categorization and then combine unrelated endpoints into a second fleet definition. Detail-specific endpoints may remain for mutations and richer account/model data, but board-level truth comes from the workspace contract.

Summary invariants:

`totalProviders = healthy + attention + disabled`

The rail and KPI strip must use the same lifecycle classification so they cannot disagree.

#### Attention classification

Attention is a lifecycle presentation state for an enabled logical provider with a current actionable issue. Issue type is separate metadata.

Supported issue families should include, when evidence exists:

- missing required credential;
- active credential requires reauthentication;
- exhausted/reported quota or rate limit;
- provider health failure;
- stale model catalogue/discovery state.

Do not invent issues when the backend has no evidence.

#### Availability and timestamps

Remove placeholder values from the workspace contract.

- `staleProviderCatalogues` must be derived from real discovery/catalogue state or represented as unknown when Benes cannot know it;
- `lastModelSync` must be a real sync/discovery timestamp, not the React component's fetch time;
- selected-provider `Last validated` must be based on an actual credential/connection validation timestamp. A quota refresh timestamp is not a validation timestamp.

If Benes cannot currently observe a metric truthfully, expose `null`/unknown and render `—` rather than fabricating zero or a current timestamp.

#### Downstream impact

Use names that match what Benes actually measures.

- routes: count real route/combo references to the logical provider and its hidden connections;
- sub-agent profiles: count actual configured sub-agent profiles when that concept exists, not a count of arbitrary unique model IDs;
- harnesses: only show `Harnesses using` for a provider if Benes has a real provider binding/reference. If harnesses do not bind to providers, selected-provider Overview must show unknown/omit that metric instead of claiming a count.

Fleet-level global harness count may remain if it is explicitly labelled as global connected/detected harnesses rather than provider dependency.

#### Provider activity

Use the bounded provider-activity log as the normalized source for Recent events, but use semantically correct event types.

Examples:

- `api_key_added` when a credential is stored;
- `api_key_removed` when removed;
- `api_key_selected` when made active;
- `api_key_used` only when real request dispatch uses the credential;
- OAuth token refreshed / reauthenticated;
- default access changed;
- model catalogue synchronized;
- provider health failure/recovery;
- configuration changed.

Credential activity remains separate from high-volume request/usage analytics.

#### Final UI cleanup

Preserve the approved visual direction.

- KPI strip has exactly five columns;
- only Attention KPI is yellow; all other KPI values/labels remain neutral;
- provider rail group headings remain neutral, while status dots may carry state color;
- zero-provider state returns to the approved quiet divider/onboarding treatment, without a large icon hero or card/tile grid;
- Fleet right pane includes the `Fleet overview` heading;
- no new cards, glow, gradients, colored section headings, or duplicate CTAs;
- no reintroduction of provider-local Models/Usage tabs;
- no Thread affinity control.

#### Frontend/backend tests

Add focused regression tests for:

- fleet state with providers and no selection;
- no provider row selected in Fleet state;
- exactly Overview / Access / Configuration for selected providers;
- workspace summary/rail lifecycle consistency;
- attention issue classification;
- unknown values render as unknown rather than fabricated zero;
- OAuth/API capability combinations;
- Credential selection conditional rendering;
- five KPI columns / single Add Provider placement;
- quiet zero-provider state;
- recent event severity and semantic event mapping.

## Error handling and compatibility

Existing valid provider configs and credential stores must continue to load. The hardening work must not require users to recreate OAuth accounts or API keys.

Logical providers may continue projecting to multiple runtime connection/spec IDs internally. Hidden connection IDs are implementation details and must not reappear as duplicate provider rows.

Management APIs remain loopback/admin surfaces and must not leak raw credentials. New activity and telemetry data must remain bounded and privacy-safe.

## Verification

Each PR runs the smallest focused tests during development. Before a non-trivial PR is marked review-ready, follow repository policy and run:

- `go test ./...`
- `go vet ./...`
- `npm run privacy:scan`
- for GUI-touching PRs: `cd gui && npm run lint && npm run build`
- `cd gui && npm run lint:i18n` after visible copy/i18n changes
- docs build if public docs are changed.

Do not claim hosted CI passed unless GitHub Actions reports success for that exact commit.

## Non-goals

This hardening pass does not:

- redesign the Providers workspace;
- add cross-provider routing-profile failover;
- make OAuth and API credentials one mixed pool;
- add automatic OAuth-to-API failover after upstream errors;
- expose Thread affinity as a user setting;
- add fake metrics to satisfy the screenshots;
- rewrite functioning credential/token machinery without a concrete correctness reason;
- change unrelated Benes pages.
