---
title: Install
description: Put the Benes CLI on the machine that will run the proxy.
---

Benes ships as an npm package whose launcher execs a Go binary. You need **Node 18+** and **Go 1.27.0**.

## Package

```bash
npm install -g benes
benes version
```

The launcher looks for `BENES_BIN`, then a local `benes` / `benes.exe`, then `go run ./cmd/benes`.

## From source

```bash
git clone https://github.com/Wibias/Benes.git
cd Benes
go run ./cmd/benes start
```

Source runs the current tree. The npm tarball is the released CLI plus the built dashboard in `gui/dist`.

## Platforms

| OS | Background install |
| --- | --- |
| macOS (arm64 / x64) | `benes service` writes a launchd user agent |
| Linux (x64 / arm64) | `benes service` writes a systemd user unit |
| Windows (x64) | Task Scheduler by default; `benes service install --native` uses WinSW |

No WSL is required on Windows.

## State directory

Default home is `~/.benes`. Override with `BENES_HOME`. The resolver lives in `internal/config/paths.go` (`ResolveHome`). Codex’s own home is separate (`CODEX_HOME` or `~/.codex`) and is only written when you inject routing.
