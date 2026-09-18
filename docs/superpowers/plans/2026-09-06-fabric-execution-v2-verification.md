# Fabric execution v2 - verification matrix

Local verification only. Do not claim hosted Actions green from this matrix.

| # | Check | Command / evidence | Result |
|---|-------|--------------------|--------|
| 1 | Old HEAD | `804bf74f323c3189c26e38c8bb1e6adb84b1ef65` | recorded |
| 2 | Shared primitive | `runModelTurn` in `authoritative_turn.go` invoked by both `/v1/responses` and Fabric execute (`TestFabricExec31`). Covers route, `resolveProviderWithEvidence`, policy/combo, context projection/recovery, provider dispatch, bridge consumption, committed usage, attribution, terminal usage. HTTP-only exclusions: SSE writer (caller EmitFrames), CORS, data-plane bearer, chat/anthropic wire, images. No loopback. | local |
| 3 | requestId vs runId | Canonical `req_*` minted once at execute admission; Fabric `run_*` remains distinct. Result.requestId threads usage/diagnostics/timeline (`TestFabricExec30`). | local |
| 4 | Cross-task result isolation | Exact taskID+runID required; no handle fallback (`TestFabricExec29`). | local |
| 5 | Terminal intent | Mutex/CAS selectTerminal; complete/cancel/shutdown race orders (`TestFabricTerminalIntentSelectOrders`); completed+shutdown no longer overwrites to CompleteExecute. | local |
| 6 | Orphan lease authority | RecoverOrphans validates current owner+fence; fail closed on malformed/wrong/stale (`TestExecG4/G5`); valid v2→Interrupted (`TestExecG6`); v1 untouched (`TestExecG3`). | local |
| 7 | Terminal persist fail-closed | Append failure => no success result; lease retained; reconcile Fail/Interrupt (`TestFabricExec33`, `TestExecPersistFailClosedNoReleaseOnAppendFailure`). | local |
| 8 | Result bounding | True LRU; byte bound over material fields; `truncated` flag (`TestFabricExec32`, `TestFabricResultStoreLRUAndByteBound`). | local |
| 9 | Fabric surface | Explicitly unattributed (`Surface: ""`); do not send `"fabric"` into CanonicalSurface. | local |
| 10 | go test fabric/server +race | `go test -race ./internal/resourcebudget/ ./internal/sidecar/fabric/ ./internal/server/ ./internal/bootstrap/ ./cmd/benes/` | local |
| 11 | Hosted Actions | not claimed | n/a |

## Distinctions

- `POST .../start` / `.../close` - lifecycle marks only (no model).
- `POST .../execute` - fenced primary run via shared `runModelTurn` under server-owned ctx.
- Cancel => `RunCancelled`; orderly shutdown => `RunInterrupted` (task remains open).
- Orphan recovery touches only positively identified v2 execute RunStarted with verified lease authority; v1 MarkStarted untouched.
- Completed output available via result handle/store only - never in Fabric events/leases/debug/timeline.
