---
title: First run
description: Start the listener, open the dashboard, and point Codex at it.
---

## Bring the proxy up

```bash
benes start
```

`start` is an alias of `serve`. Preferred port is **23100**. If that port is taken and you did not pass `--port`, Benes may pick another free port. An explicit `--port` never hops. `start` does **not** rewrite Codex `config.toml`; pass `--inject` or run `benes sync` when you want Codex pointed at the listener.

Open **http://127.0.0.1:23100** (or `benes gui`, which starts the listener if needed).

To edit the dashboard against that live listener:

```bash
benes dev
```

`--proxy off` (the default) is Vite HMR on **23200**. `--proxy on` also runs the checkout's Go data plane, still using `~/.benes` provider config and auth, without injecting Codex or replacing the main pid file. Go sources under `cmd/` and `internal/` rebuild on change.

## Wire Codex

```bash
benes init
```

`init` writes `~/.benes/config.json` and, if you accept the Codex step, injects routing into the resolved Codex home. It does **not** start the listener. Start first or after; order is flexible, but live commands such as `benes provider add` talk to the running process and fail when it is down.

`benes status` reports whether the proxy is running and, separately, whether Codex is configured to use Benes. A leftover `runtime-port.json` is only evidence: a dead PID is stale, and a recycled PID that now belongs to another process is not treated as Benes and is not signaled by `benes stop`. Running requires the recorded process identity plus loopback `/readyz`. A live listener is not enough to print `benes`; missing or unreadable Codex config is `unknown`.

```bash
benes status
benes health --json
benes ready --wait
```

`GET /healthz` is immediate liveness. `GET /readyz` is post-sync readiness (`200` when `status` is `ready`; `pending` / `failed` return `503`).

## Pick a model

Codex (and other clients) send a model id. Namespaced ids look like `provider/model`:

```bash
codex -m "anthropic/claude-sonnet-4-6" "summarize this diff"
```

A bare id uses the default provider or a name match. Inner slashes in upstream ids are also published with `-` in place of `/`; the raw slash form still works.

## Shut down

```bash
benes stop
```

Stop tears down the process and restores native Codex from the injection journal. `benes restore` (alias `eject`) does the Codex restore without needing a live proxy. `benes uninstall` also removes the service, shim, and tray.
