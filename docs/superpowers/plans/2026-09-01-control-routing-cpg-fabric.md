# Control Routing, CPG, and Fabric Implementation Plan

> **For agentic workers:** Isolated packages (CPG, Fabric kernel) may run in parallel. Routing-plane files (`internal/server/*`, GUI control tabs) stay with the coordinator. Do not edit `responses.go` until the coordinator wires CPG.

**Goal:** Make Routing Profiles, Compatibility, and Combos production-ready end to end; port Cache-Preserving Context Projection into Go; land Agent Fabric as an opt-in sidecar. When this is done the dashboard boards persist, failover actually runs, and unused subsystems stay off the default start path.

**Architecture:** Combos remain reusable failover chains (`combo/<id>`). Routing profiles are named ordered candidate lists (`policy/<id>`) that reuse `combo.Walker` — not a scoring optimizer and not a second failover product. Compatibility is a derived harness×model matrix, not a SQLite lab. CPG compresses provider-visible tool results only. Fabric is a separate event-sourced task kernel behind `fabric.enabled`.

**Tech Stack:** Go data plane (`internal/`), React dashboard (`gui/src`), existing `/api/*` management surface.

**Sources:** Anthropic, 29 Sep 2025: [Effective context engineering for AI agents](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents). Claude context editing: [clear_tool_uses_20250919](https://platform.claude.com/docs/en/build-with-claude/context-editing) is lossy server-side clearing; CPG is stricter (exact receipts + hidden recovery).

## Global Constraints

- Production runtime is Go. No TypeScript under `cmd/` or `internal/`.
- Combos are failover only. Round-robin may be stored historically but PUT rejects it; projector still skips `unsupported_strategy`.
- Routing profiles must not invent a weighted optimizer. Ordered candidates + require-gates + `combo.Walker`.
- CPG default is `off`. Canonical Responses history stays exact. No second persistent copy of tool text. Passthrough/native Codex is never projected. Hidden recovery never failovers providers.
- Fabric is opt-in and off the default `benes start` path. No A2A remote-primary in this slice (that would be a new product).
- Management APIs stay loopback-gated and never return secrets.
- Dashboard copy goes through `gui/src/i18n/*.ts` + `useT()`.
- Match the Control mock tab strip: Profiles | Compatibility | Combos. Evaluation/Analytics stay as profile-detail surfaces, not top tabs.

---

### Layer map

| Layer | Owns | Request-time effect |
| --- | --- | --- |
| Combos | `combos.<id>` config + `internal/combo.Walker` | Yes — `combo/<id>` |
| Profiles | `routingProfiles.<id>` config | Yes — `policy/<id>` walks candidates |
| Compatibility | derived `/api/lab/*` | No — evidence only |
| CPG | `contextProjection.mode` | Yes — provider-visible context only |
| Fabric | `~/.benes/fabric` when enabled | No — sidecar |

### Task 1: Combo persist + hot reload

**Files:** `internal/server/combos_api.go`, `internal/server/combos_api_test.go`, `internal/server/combo.go`, `internal/server/responses.go` (mutex field only if needed)

- PUT `/api/combos` body `{ id, renameFrom?, combo }` → `{ success: true }`
- DELETE `/api/combos?id=` → `{ success: true }`
- GET returns full dashboard fields from disk when `configPath` is set (alias, strategy, effort, imageInput, nativeAlias, displayName, stickyLimit, targets)
- Reject round-robin, empty targets, unknown providers, invalid ids
- After mutate, rebuild `h.combos` from `config.ProjectCombos` so live `combo/<id>` traffic updates without restart
- Protect combo map with `sync.RWMutex`

### Task 2: Routing profile persist + policy walker

**Files:** `internal/server/routing_profiles_api.go`, `internal/server/routing_profiles_api_test.go`, `internal/server/combo.go`

- PUT `/api/routing-profiles` body `{ mode, id, expectedRevision?, profile }` with optimistic revision
- DELETE `/api/routing-profiles?id=`
- GET always emits `require`, `optimize`, `limits`, `unknownEvidence`, `compatibility` objects (GUI parse requires them)
- `policy/<id>` and profile alias resolve through `combo.Walker` over candidates
- Dry-run returns `DryRunResult`: per-candidate `eligible`/`exclusions`, `selectedIndex` = first eligible
- GET `/api/routing-analytics` returns the GUI `Analytics` shape (zeros until traffic; increment on policy/combo walks when cheap)

### Task 3: Compatibility capability matrix

**Files:** `internal/compat/matrix.go`, `internal/server/lab_api.go`, tests

- Derive Compatible/Degraded/Unsupported for harnesses `codex|claude|grok|opencode` × configured models from adapter/protocol + vision
- Serve existing GUI contracts: `/api/lab/status`, `/verdicts`, `/subjects`, `/subjects/:id`, `/observations`, `/catalog`, `/production-signals`, `/public/community` (empty community is fine)
- `projectionAvailable: true`, `projectionSpecVersion: "capability-v1"`
- Catalog scenarios for the profile suite picker

### Task 4: Dashboard wiring

**Files:** `gui/src/pages/control-tab.ts`, `control-tab-strip.tsx`, `control-page-shell.tsx`, combo/profile pages as needed, i18n

- Routing tabs: Profiles, Compatibility, Combos (hashes for evaluation/analytics still work)
- Combos persist already calls PUT/DELETE — they start working
- Prefetch routing boards with the other nav boards
- Round-robin create stays in the editor but save is denied by API; do not add a new strategy

### Task 5: CPG Go package (isolated)

**Files:** `internal/contextprojection/*` only

Port into `internal/contextprojection` against `protocol.Context`:

- Modes `off|shadow|duplicate|recovery` (`on` = recovery)
- Duplicate: string or single text part, threshold, namespace/tool/error match, SHA-256 bucket **and** exact equality, earlier full copy still visible
- Large recovery: 96 KiB, 8 KiB head/tail receipts, errors stay full
- Refs: `toolCallId + namespace + toolName + occurrence ordinal`; domain `benes-context-ref-v1`; tool `__benes_context_v1`
- Request-local registry only; continuation latch is four fields
- Tests from the TS projector/duplicate/recovery-protocol contracts
- Do not hook `responses.go` in this task

### Task 6: CPG request hook

**Files:** `internal/server/responses.go`, continuation store, config schema

- After parse, before `provider.Open`
- Shadow: metrics only
- Duplicate: project copy of `Context`, leave `Raw`/canonical history
- Recovery: inject tool, hidden loop, one fail-open restart, no combo hop inside hidden trajectory
- Skip native Codex passthrough and compaction turns
- Env `BENES_CONTEXT_PROJECTION_EMERGENCY_DISABLE=1`
- Default config `off`

### Task 7: Fabric sidecar kernel (isolated)

**Files:** `internal/sidecar/fabric/*` only

Port Go spike + fab-final kernel/store/projection:

- Hash-chained JSONL per task under a caller-supplied dir
- Create/list/detail/timeline projections
- Fencing + expected-sequence stale-writer rejection
- Remove + initial-input abandon from fab-final
- No Codex JSON-RPC, no Claude SDK, no A2A
- Tests for hash chain, sequence, rebuild, crash recovery

### Task 8: Fabric HTTP + dashboard gate

**Files:** `internal/server/fabric_api.go`, GUI Tasks page gated on `GET /api/fabric/status`

- Off unless config `fabric.enabled` is true
- Loopback management routes matching fab-final read/create/start/close/remove subset
- Sidebar entry hidden when disabled

### Task 9: Wire + verify

- Focused Go tests for each new package and API
- `gui` lint:i18n if copy changed
- Docs: combos persist from dashboard; profiles as `policy/<id>`; CPG off-by-default; fabric opt-in
- `benes lab` stays exit 2 (capability matrix is not Compatibility Lab)
