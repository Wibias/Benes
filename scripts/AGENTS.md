# `scripts/`

Parent: `/AGENTS.md`. Release, install, privacy, and local-CI tooling live here. This is not the Go data plane.

## Runtime

Stay on the Node and POSIX/PowerShell tools already in this directory. Do not add Python, Ruby, or a second package manager to ship a script.

`node-runtime.ts` and `win-exec.ts` are the portable process helpers. Call those instead of hard-coding `cmd.exe` or a bash-only path.

A script must return the child exit status. Do not swallow a failure and exit 0.

Where the same inputs should yield the same outputs (privacy scan, changelog assembly, package prep), keep the script deterministic. Do not read the clock, RNG, or the ambient user profile unless that is the point of the command.

## Evidence and browser automation

Evidence/browser tooling is a security boundary. Compose a run through the canonical runner rather than assembling browser lifecycle pieces yourself:

```
task-specific / transient automation
  -> runBrowserEvidence()            scripts/evidence-browser-runner.ts
  -> evidence scratch                scripts/evidence-scratch.ts
  -> browser automation runtime      scripts/evidence-browser-runtime.ts
  -> owned processes                 scripts/evidence-process.ts
  -> CDP session + request registry  scripts/evidence-cdp-session.ts, scripts/cdp-request-registry.ts
```

Ownership is exclusive; do not duplicate these responsibilities in a caller:

- only the browser runtime owns `--user-data-dir=`; the persistent automation profile is intentionally separate from the per-run evidence scratch, is reused across runs, and must not live under a real user profile or another protected path;
- only the canonical runner owns high-level evidence-browser lifecycle composition: scratch, profile lease, the overall deadline, managed shutdown, CDP disposal, and the decision to delete evidence;
- only `EvidenceCdpSession` and `CdpRequestRegistry` own CDP request lifecycle, including the bounded request timeout and socket close/error binding;
- only `evidence-scratch.ts` owns recursive evidence deletion, behind its ownership-marker and path validator;
- only the browser runtime starts and terminates browser processes, and only recorded child PIDs/process trees through `evidence-process.ts` with a hard termination deadline.

Behaviour those primitives must keep:

- the persistent automation profile is leased exclusively and fails closed: a lease is released only after the owned browser is confirmed terminated, and a failed termination leaves the lease held;
- shutdown is graceful-first (`Browser.close`), with owned-process termination only as a bounded fallback, and it happens at most once per run;
- a pre-CDP startup failure terminates only the owned browser (`terminateBeforeCdp`) because no usable CDP session exists to close;
- a task failure keeps the task error primary and still runs managed shutdown and CDP disposal; a teardown failure is reported through `AggregateError` rather than hidden;
- evidence scratch is retained for any failed run and deleted only after a fully successful run;
- callers must not supply `--user-data-dir`, `--remote-debugging-port`, or `--remote-debugging-pipe`; the runner rejects them before creating scratch, acquiring the lease, or starting a process.

Do not:

- kill Chrome or another executable globally by image/process name, or terminate a process this automation does not own;
- supply a caller-owned browser profile, or point one at the real Chrome user profile;
- preseed Chromium password-manager preferences (for example `password_manager.os_password_blank`) into the automation profile;
- recursively delete an arbitrary path from an evidence runner.

Transient `.tmp`, scratch, generated, or one-off evidence/browser scripts are not exempt: they call `runBrowserEvidence()` and the committed primitives instead of implementing their own browser lifecycle, recursive cleanup, or process-by-name teardown. The tracked source-policy test is defense in depth only; it does not sandbox untracked/generated scripts, so path/process/timeout containment must remain enforced by the committed runtime primitives. If a PowerShell shim is unavoidable, use `Set-StrictMode -Version Latest` and `$ErrorActionPreference = 'Stop'`, and do not use names that collide with PowerShell automatic variables such as `$HOME` or `$PID`.

Tests for this surface must use synthetic temporary directories and fake/local child transports. Never point a destructive regression test at a real user profile.

## Secrets

Never print tokens, API keys, `auth.json`, account ids, or request bodies. `privacy-scan.ts` is the repo check; do not add a log line it would flag.

## Release and install

`release.ts` is the only publish driver. Dry-run, exact-commit, and confirmation gates stay on. Generated npm payload comes from `prepare-package.ts` — do not hand-edit `gui/dist` or staged package files.

Changes to release, packaging, dependency install, credentials, or GitHub automation need security review (`MAINTAINERS.md`).

## Tests

Add or extend a focused test next to the script. Comments are not a substitute for a check, and they must not hide complexity.
