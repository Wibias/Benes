# Lab v1 and Fabric v1 design

Date: 2026-09-02

## Purpose

Make Compatibility Lab and Agent Fabric production-ready as **opt-in** products. `benes start` with one provider and one model still pays for neither. CPG, combo round-robin, live probes, and Fabric role routing to Codex/Claude/A2A stay out of this slice.

Approved scope:

- Lab v1: deterministic Protocol Conformance harness (no paid HTTP to providers), durable local store, `benes lab rebuild` opt-in. Compatibility UI reads stored verdicts when present.
- Fabric v1: existing task kernel. Tasks nav only when `fabric.enabled`. Dashboard create / start / close / timeline. No second model router.

## Non-goals

- Live route probes (CL-03) and task-effectiveness suites (CL-07)
- Community evidence ingest
- Fabric dispatch through `policy/<id>` or any runtime
- Codex JSON-RPC, Claude SDK, A2A remote-primary
- CPG changes
- Combo round-robin
- Lab or Fabric on the default start path

## Zone map

| Zone | Product | This slice |
| --- | --- | --- |
| 1 Evidence | Compatibility Lab | Durable protocol-conformance runs + merge into `/api/lab/*` |
| 2 Inference routing | Profiles / Combos | Unchanged. Gates already consume verdicts; they stay derived until a later live-probe slice |
| 3 Task orchestration | Agent Fabric | Kernel already exists. Dashboard + Control toggle |
| (none) | CPG | Stays on Control. Not Lab, not Routing, not Fabric |

---

## Lab v1

### What exists today

`GET /api/lab/*` is a derived harness×model chart from adapter + vision (`internal/compat.Classify`). Verdicts are published on `evidenceLayer: live_route_compatibility`. Observations, events, artifacts, and community are empty. `benes lab` prints that the Lab is not activated and exits 2.

### Honest layers

| Column in Compatibility UI | Source after this slice |
| --- | --- |
| Protocol conformance | Always present on GET. Without a Lab store: `CLAIMED` (catalog/adapter claim, no run). After `benes lab rebuild`: `Classify` result (`VERIFIED` / `DEGRADED` / `UNSUPPORTED` / `UNKNOWN`) plus contributing event ids |
| Derived compatibility (adapter + vision) | Unchanged in-process `compat.Classify` on `live_route_compatibility` |
| Task effectiveness | Empty |

`CLAIMED` means no Lab run is on disk. After rebuild, `VERIFIED` means “local classifier run recorded against this catalog snapshot,” not a paid HTTP probe. `Classify` does not gain new HTTP. The product is durability, events, and an explicit opt-in run.

Today GET emits only `live_route_compatibility` rows, so the protocol column is empty. After this slice GET always emits protocol-conformance rows so that column is honest even before the first rebuild.

### Store

Root: same home as `config.json` (`filepath.Dir(configPath)/lab`, default `~/.benes/lab`).

- Append-only JSONL ledger `events.jsonl` with Fabric-style `eventId` + `prevHash` chain. Lab-specific `eventKind` values only (`lab.run.started`, `lab.verdict.recorded`). No secrets, no request bodies, no home paths
- Do not reuse `internal/sidecar/fabric.Repo` (that kernel is tasks)
- Loopback GET rebuilds an in-memory projection from the ledger on each request (or when mtime changes). CLI rebuild writes the ledger; it does not talk to a running listener
- No Lab work on `benes start` / `serve`

Each rebuild appends a new run envelope (audit). The same catalog+adapter snapshot produces the same verdict set; the projection keeps the latest run’s rows per `subjectId`×harness.

Subjects are concrete `provider/model` ids from `config.json`: provider `models` / `selectedModels` / `defaultModel`, `customModels`, and `modelDiscovery` known ids. Combo and `policy/` synthesized catalog rows stay on the derived live-route chart only.

Harnesses are `compat.Harnesses`: `codex`, `claude`, `grok`, `opencode`. Rebuild does not open provider HTTP.

### CLI

`benes help` lists `lab`. `benes lab` without a subcommand prints Lab usage and exits 2 (replace `TestRunLabRefusesWithoutActivatingLab`).

| Command | Effect |
| --- | --- |
| `benes lab help` | Same usage, exit 0 |
| `benes lab rebuild` | Classify stored catalog against provider adapters; append ledger. Exit 0. No provider calls. Exit 1 if config unreadable or ledger corrupt |
| `benes lab status` | Print subject/verdict/event counts from the store (zeros if the dir is missing). Exit 0 |

No Lab timers. No background rebuild inside the listener.

### HTTP

Loopback `GET /api/lab/*` stays the GUI contract.

- `status.observationCount` / `eventCount` come from the store (0 if none). `corruptionCount` is 0 unless the ledger fails to parse
- `verdicts` merge: derived live-route rows unchanged; protocol-conformance is CLAIMED placeholders, overlaid by store rows when present
- `GET /api/lab/observations` returns one observation per latest protocol-conformance verdict (eventId, subjectId, evidenceLayer, suiteId, verdict, asOf). Cap 200. Empty array if no store. No prompt-like payloads
- `GET /api/lab/events/:id` returns the matching ledger event metadata (no secrets) or 404
- `GET /api/lab/public/community` stays empty on purpose
- Listener does not require a rebuild to boot. A rebuild written while the listener is up is visible on the next GET (disk read)

### Routing

Profile compatibility gates keep using `compat.Classify` as today. They do not wait on Lab disk. A later slice can prefer stored `VERIFIED` when present.

### Tests

- Rebuild with two catalog models writes protocol-conformance verdicts and non-zero event count
- Rebuild does not call a fake HTTP provider
- GET `/api/lab/verdicts?layer=protocol_conformance` returns store rows after rebuild
- GET without a store still returns derived live-route rows plus CLAIMED protocol-conformance rows
- `benes lab` with no args exits 2; `rebuild` exits 0
- Privacy scan: ledger has no API keys or home paths

---

## Fabric v1

### What exists today

`internal/sidecar/fabric` is a hash-chained task kernel (create, start, complete, cancel, remove, timeline). Loopback `/api/fabric/status` always answers `{enabled}`. Other `/api/fabric/tasks*` exist only when `fabric.enabled` is true. There is no dashboard page and no Control toggle.

### Enablement

Control Request handling gets a Fabric switch after the CPG row, same persist pattern as `/api/sidecar-settings` and `/api/context-projection`:

- Loopback `GET`/`PUT /api/fabric-settings` `{enabled: boolean}` writes `fabric.enabled` in `config.json`
- Default omitted/false
- PUT does not start Codex, Claude, or a worker
- Enablement is live: `fabricEnabled()` already reloads disk per request. No listener recycle

`GET /api/fabric/status` remains `{enabled}`. Existing task routes stay 404 with `fabric is disabled` while off.

### Dashboard

- New page hash `#tasks` (Page id `tasks`)
- Sidebar item under Observation, **omitted from the nav list unless `GET /api/fabric/status` reports enabled**
- Board: divider sections, no cards. List + selected detail (title, goal, state, timeline). Create in page-head. Start / Close / Remove as text actions with toasts. Map to existing `POST /api/fabric/tasks`, `POST .../start`, `POST .../close` (`CompleteRun`), `DELETE .../:id`
- Cancel stays API-only in this slice
- Empty state when enabled and no tasks
- Direct `#tasks` while disabled: short “Fabric is off” board with a Control link; do not 404 the SPA
- Prefetch only while the nav item is visible (enabled)
- i18n keys in every locale module
- No Fabric Tasks item when disabled — not a disabled grey row

### Runtime

Start/Close/Remove record kernel events only. They do not send `/v1/responses`. A run that is “started” is an operator mark on the log, not a model call. Store remains `filepath.Dir(configPath)/fabric` (default `~/.benes/fabric`).

### Tests

- Settings PUT persists `fabric.enabled`; GET `/api/fabric/status` flips without process restart
- Tasks API 404 while disabled; create/list/start/close when enabled
- GUI: nav omits Tasks when status.enabled is false
- Kernel tests already in `internal/sidecar/fabric` stay green

---

## Delivery order

1. Lab store + CLI + GET merge (Compatibility board becomes honest protocol column)
2. Fabric settings + Tasks board
3. Docs: `benes lab rebuild`, Control Fabric toggle, Tasks nav, community still empty, CPG unchanged

## Error handling

- Missing lab dir: GET returns derived live-route plus CLAIMED protocol-conformance, counts 0
- Corrupt ledger: GET returns derived live-route; status `corruptionCount` ≥ 1; CLI rebuild reports the error and exits 1
- Fabric store open failure: 500 `fabric_unreadable`, no secrets in the body

## Success

- One provider, one model, default config: no Lab disk, no Fabric nav, `benes start` unchanged
- After `benes lab rebuild`: Compatibility protocol column has stored verdicts; live-route column still derived
- After enabling Fabric on Control: Tasks appears; create/start/close persist under `~/.benes/fabric`
- `benes lab` is a real command; it is still not on the start path
