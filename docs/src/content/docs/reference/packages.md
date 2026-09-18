---
title: Packages
description: Go layout for the production CLI and data plane.
---

Module: `github.com/Wibias/Benes`. Entry: `cmd/benes`.

| Package | Job |
| --- | --- |
| `internal/server` | HTTP listener, `/v1/*`, `/api/*`, dashboard files, Diagnostics request telemetry |
| `internal/config` | Home paths, config transactions, projections |
| `internal/router` | Provider/model selection |
| `internal/providerregistry` | Canonical preset registry |
| `internal/providers/*` | Adapters (`openaichat`, `openairesponses`, `google`, `antigravity`, `cursor`, `xaicapability`, `kiro`) |
| `internal/conformance` | Test-only protocol compile/stream harness. Not imported by production packages. |
| `internal/catalog` | Model catalog, effort ladders, spawn membership |
| `internal/responses` | Request parse, SSE, continuation |
| `internal/oauth` | Provider logins |
| `internal/codexauth` | ChatGPT account pool |
| `internal/codexrestore` | Inject / restore Codex files |
| `internal/codexappserver` | Codex app-server process matching |
| `internal/sidecar/websearch` | Search sidecar |
| `internal/sidecar/vision` | Vision sidecar |
| `internal/sidecar/fabric` | Opt-in Agent Fabric task kernel (lifecycle + fenced execute + single-child handoff, JSONL store) |
| `internal/contextprojection` | Cache-preserving context projection |
| `internal/lab` | Compatibility Lab JSONL ledger and rebuild |
| `internal/compat` | Derived harness×model capability matrix |
| `internal/combo` | Failover walker |
| `internal/timeline` | Per-request delivery traces |
| `internal/storage` | `CODEX_HOME` scan, archived cleanup, `.trash` quarantine/restore |
| `internal/sessions` | Durable session identity and request history (`~/.benes/sessions.sqlite`) |
| `internal/export` | Client config printers |
| `internal/servicectl` | Service install/control |

The dashboard is TypeScript in `gui/` and is not on the request path except as static files.
