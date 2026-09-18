# Provider Access Runtime Hardening Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make the logical OpenAI provider's Access configuration control real runtime lane selection and make first-time API-key provisioning reliable without changing the approved Provider UI layout.

**Architecture:** Keep one user-facing OpenAI provider while allowing separate OAuth and API runtime connections. Centralize lane selection in config/server helpers, make access descriptor IDs internally consistent, and keep cross-lane failure fallback out of scope.

**Tech Stack:** Go data plane/server/config, React + TypeScript dashboard, existing credential store and Codex account pool.

**Spec:** `docs/superpowers/specs/2026-08-27-provider-hardening-design.md`

## Global Constraints

- Feature PRs ultimately target `dev`; stacked PRs may target an open parent branch until reconciliation lands.
- Existing valid provider configs and credential stores must continue to load.
- Do not expose Thread affinity as a user setting.
- Do not introduce OAuth-to-API failover after an upstream failure.
- Hidden runtime connections such as `openai-apikey` must not reappear as duplicate provider rows.
- Management APIs must not return raw credentials.

---

### Task 1: Make OpenAI lane resolution honor `defaultAccess`

**Files:** `internal/config/logical_provider.go`, `internal/config/logical_provider_test.go`, `internal/server/combo.go`, focused server routing tests.

- [ ] Add failing tests for both-lane default OAuth/API, OAuth-only, API-only, explicit API route, and explicit account route.
- [ ] Run `go test ./internal/config -run 'TestResolveOpenAILane' -count=1` and confirm RED.
- [ ] Change the resolver to receive the configured default access and choose it only when both lanes can serve the model.
- [ ] Run the focused config tests and confirm GREEN.
- [ ] Add failing server tests proving persisted `defaultAccess` reaches direct runtime resolution without rereading disk config per request.
- [ ] Thread immutable default-access state into handler routing and confirm GREEN.
- [ ] Add a failing combo test showing a logical OpenAI combo target obeys the same lane decision.
- [ ] Route combo target construction through the logical provider resolver; keep combo strategy failover-only.
- [ ] Run `go test ./internal/config ./internal/server -run 'OpenAI|Combo' -count=1`.
- [ ] Commit `fix: honor OpenAI default access at runtime`.

### Task 2: Provision the hidden OpenAI API connection on first key

**Files:** `internal/server/providers_keys.go`, `internal/server/providers_mutate_api.go`, server tests.

- [ ] Add a failing test with only canonical `openai`: first API-key POST succeeds, creates canonical hidden `openai-apikey`, stores secret securely, and workspace still shows one OpenAI row.
- [ ] Run focused test and confirm current `unknown_provider` RED.
- [ ] Add one canonical provisioning helper for the hidden OpenAI API connection using existing constants/preset semantics.
- [ ] Resolve logical OpenAI API-key mutations to `openai-apikey` after ensuring the lane exists.
- [ ] Verify GREEN.
- [ ] Add a credential-store failure test and ensure no dangling credential reference or false success is produced.
- [ ] Add a failing test for `defaultAccess: api` when API access is unusable; prefer `409 api_access_unavailable` until at least one API credential exists.
- [ ] Implement the guard and verify.
- [ ] Commit `fix: provision OpenAI API access atomically`.

### Task 3: Normalize Access descriptor IDs and selection mode

**Files:** `internal/config/logical_provider.go`, tests, `gui/src/provider-workspace/auth.ts`, `gui/src/components/provider-workspace/ProviderAccess.tsx`.

- [ ] Add failing descriptor tests: lane IDs are `oauth`/`api`; `defaultMethodId` equals a real method ID; API `kind` remains `api-key`; Direct reports `direct`; Pool reports `pool`; quota vs round-robin fields are conditional.
- [ ] Run `go test ./internal/config -run 'TestFoldLogicalProviders' -count=1` and confirm RED.
- [ ] Normalize backend descriptor generation and confirm GREEN.
- [ ] Update frontend consumers to use lane IDs and translate to hidden runtime connection IDs only at mutation boundaries.
- [ ] Add/adjust frontend regression tests for OAuth-only, API-only, OpenAI dual-access, Direct/Pool, quota/round-robin, and no Thread affinity.
- [ ] Run `cd gui && npm run lint && npm run build`.
- [ ] Commit `refactor: normalize provider access descriptors`.

### Task 4: PR 1 verification

- [ ] Run `go test ./internal/config ./internal/server -count=1`.
- [ ] Run `go test ./...`.
- [ ] Run `go vet ./...`.
- [ ] Run `npm run privacy:scan`.
- [ ] If GUI changed, run `cd gui && npm run lint && npm run build` and `npm run lint:i18n` if copy changed.
- [ ] Inspect diff for raw credential leakage, cross-lane failure fallback, Thread affinity UI, or unrelated redesign.
- [ ] Open using the repository PR template against the reconciliation parent while open, then retarget to `dev` after reconciliation lands.
