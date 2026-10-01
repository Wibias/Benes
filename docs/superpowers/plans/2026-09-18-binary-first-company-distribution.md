# Issue #2 enabler — Binary-first installation and company distribution implementation plan

> **Plan-only PR.** This document specifies future implementation work. It must not change runtime behavior, release behavior, installation behavior, package contents, or tenant-isolation behavior by itself.

Date: 2026-09-18
Issue: #2 (`Multi-user hosting with tenant isolation`)
Frozen planning base: `dev@d11a024e4366fc280b2e084b2e1dbf1ebda5c67c`
Planning branch: `plan/2-binary-first-company-distribution`

## Goal

Make a released Benes build installable and runnable as a normal product without requiring the target machine to already have Node or Go.

The target distribution must also work in managed/company environments:

- pinned versions;
- checksummed artifacts;
- internal mirrors;
- offline/air-gapped installation;
- no compiler toolchain on the normal runtime path;
- no surprise toolchain or service-wrapper download;
- one Benes runtime binary per supported OS/architecture;
- current single-user workstation behavior preserved;
- current non-loopback admission behavior preserved.

This work is a deployment enabler for #2. It does **not** provide tenant isolation.

A remotely reachable Benes process with `BENES_API_AUTH_TOKEN` and `x-benes-api-key` remains an admission-protected shared process until #2 separately establishes tenant identity and isolation.

## Non-goals

This plan does not implement:

- tenant identity;
- per-tenant config, credentials, history, usage, routing, or account pools;
- a SaaS control plane;
- license enforcement;
- a second release authority;
- a new updater;
- telemetry;
- a machine-wide daemon as the default install;
- a new config format;
- weighted combo behavior;
- Compatibility Lab startup behavior;
- provider/runtime feature work unrelated to distribution.

## Current-state facts at the frozen base

The implementation must start by revalidating these facts against the then-current `dev`. They are true at the planning base.

### npm package

`package.json` currently publishes:

- `bin/`;
- `cmd/`;
- `internal/`;
- `go.mod`;
- `go.sum`;
- `gui/dist/`;
- selected README assets.

The package requires Node >=18.

The current npm install is therefore a source/toolchain distribution, not a standalone native product install.

### Launcher

`bin/benes.mjs` currently resolves in this order:

1. `BENES_BIN`;
2. an adjacent `benes` / `benes.exe`;
3. `go run ./cmd/benes`.

The source fallback sets `GOTOOLCHAIN=go1.27.0` unless the caller already supplied a value.

A normal end-user install can therefore cause Go to be required and may allow a Go toolchain fetch.

### Install scripts

`scripts/install.sh` and `scripts/install.ps1` currently:

- require Node;
- require npm;
- require Go 1.27.0;
- run `npm install -g benes`;
- verify only that the resulting launcher answers `benes help`.

They do not install a standalone native release artifact.

### Dashboard

The production server currently receives a dashboard directory through `internal/bootstrap`.

`internal/server/dashboard.go` serves an on-disk directory with `http.FileServer`.

There is no embedded dashboard fallback in the Go binary.

A lone native binary therefore cannot be treated as the complete workstation product until this is changed.

### Release authority

`scripts/release.ts` is the release coordinator.

`.github/workflows/release.yml`:

- validates an audited SHA;
- uses npm trusted publishing/OIDC;
- publishes npm;
- creates the release/tag through the existing release domain;
- does not currently build the standalone cross-platform Benes archive set required by this plan.

This plan must extend that authority rather than introduce a competing publisher.

### Existing capabilities to reuse

Do not redesign capabilities that already exist:

- `benes config import <path|-> --yes`;
- `benes config export`;
- `BENES_HOME`;
- non-loopback admission through `BENES_API_AUTH_TOKEN`;
- `x-benes-api-key` data-plane admission;
- per-user service installation as the default;
- `benes update` as a non-mutating update policy surface;
- current HTTP/HTTPS proxy handling for upstream providers;
- current release SHA/CI gates.

### Windows native service

`internal/winsw` pins a WinSW URL and checksum.

The ordinary Windows service path remains Task Scheduler.

Any future native-service distribution must not turn a default/offline install into an unexpected GitHub fetch.

---

# Product contract

## One released runtime

A release should provide one Benes runtime executable for each supported OS/architecture.

Expected shape:

```text
benes       # Unix-like platforms
benes.exe   # Windows
```

The released executable must contain everything required to serve the built dashboard.

A checkout may still prefer local `gui/dist` so developers can rebuild the UI independently.

## Workstation mode remains the default

Default behavior remains:

- one user;
- `127.0.0.1:23100`;
- state under `~/.benes` or `BENES_HOME`;
- user-scoped background service;
- no tenant subsystem required merely to run locally.

## Managed gateway deployment

The same binary may be supervised on an internal host with a non-loopback bind.

Existing listener admission requirements remain mandatory.

Distribution documentation must use language such as **gateway** or **shared deployment**, not **multi-tenant**, until #2 is implemented and verified.

## No unexpected network behavior

Installing from a local file must not require network access.

Starting an already installed binary must not:

- fetch Go;
- fetch Node;
- fetch npm packages;
- fetch another Benes executable;
- self-update;
- download a service wrapper on the ordinary default service path.

Provider requests are naturally outside this restriction once the operator configures providers.

---

# Release artifact contract

## Build properties

Release binaries should be produced with:

- `CGO_ENABLED=0` where the supported dependency graph permits it;
- `-trimpath`;
- a release version injected through the existing version mechanism;
- reproducible/stable archive structure;
- executable mode preserved on Unix.

Do not add GoReleaser or another release driver merely for convenience.

If artifact-building logic needs a helper, keep it subordinate to `scripts/release.ts` and `.github/workflows/release.yml`.

## Initial platform matrix

Revalidate supported compilation and runtime behavior before implementation.

The initial target should be at least the platforms already documented/supported for ordinary Benes use:

| OS | Architecture | Wave 1 expectation |
| --- | --- | --- |
| macOS | arm64 | required |
| macOS | amd64 | required |
| Linux | amd64 | required |
| Linux | arm64 | required |
| Windows | amd64 | required |

Windows arm64 must only be added when current Go build, installer, tray/service behavior, and required native dependencies have explicit evidence. Do not advertise it because Go can theoretically cross-compile a subset.

## Stable filenames

Use one stable, mirror-friendly naming scheme.

Proposed contract:

```text
benes_<version>_darwin_arm64.tar.gz
benes_<version>_darwin_amd64.tar.gz
benes_<version>_linux_amd64.tar.gz
benes_<version>_linux_arm64.tar.gz
benes_<version>_windows_amd64.zip
SHA256SUMS
```

The exact scheme may change before implementation only if one canonical alternative is selected and used by:

- release builder;
- GitHub Release upload;
- installer;
- mirror support;
- tests;
- docs.

Do not support multiple filename conventions.

## Checksums

Every release archive must be represented in one release-level SHA-256 manifest.

Installer behavior must fail closed when:

- checksum metadata is unavailable;
- the requested archive has no checksum entry;
- downloaded/local archive bytes do not match.

Do not run or install an unverified artifact.

## Release attachments

The release workflow should upload:

- all required platform archives;
- `SHA256SUMS`;
- installer scripts for that release where doing so preserves version/install contract;
- provenance/attestation metadata if implemented through the existing GitHub release job.

The existing npm release remains part of the same release authority.

---

# Dashboard embedding design

## Required precedence

The runtime should preserve useful checkout/developer behavior while making a standalone release complete.

Recommended precedence:

1. explicit `BENES_DASHBOARD_DIR`;
2. valid local checkout/package dashboard directory where current behavior requires it;
3. embedded release dashboard fallback.

Do not make embedded assets override an intentionally supplied developer directory.

## Package boundary

Create one small Go package dedicated to embedded dashboard assets.

The package must be importable by the server/bootstrap layer without importing CLI code.

The release build must embed the real Vite output.

Normal `go test ./...` from a fresh checkout must still compile before a GUI build has been run.

That means the source tree needs a minimal compile-safe embed input, while release assembly replaces or stages the built dashboard into the embed input before the release binary is compiled.

Do not commit generated `gui/dist` copies into a second source directory on every GUI build.

## Required tests

Focused tests must prove:

- explicit dashboard directory wins;
- valid current checkout/package directory wins when applicable;
- embedded fallback is used when no external directory exists;
- standalone fallback serves `index.html`;
- API routes are not swallowed by the dashboard file server;
- SPA fallback behavior matches current dashboard routing expectations;
- ordinary `go test ./...` works without running Vite first.

---

# Implementation sequence

The implementation should land as independently reviewable PRs.

Do not combine all distribution work into one large release/security PR.

## PR A — Embedded dashboard fallback

### Goal

Make a native Benes executable capable of serving the current dashboard with no adjacent `gui/dist`.

### Expected files

Likely create:

- `internal/server/dashboardembed/embed.go`;
- a compile-safe embedded placeholder/source asset location;
- focused embed tests.

Likely modify:

- `internal/bootstrap/dataplane.go`;
- `internal/server/dashboard.go`;
- focused bootstrap/server dashboard tests;
- package preparation only if required to keep npm's current path green.

### Required behavior

- current explicit/local dashboard directory behavior remains valid;
- embedded assets are fallback, not an unconditional replacement;
- no data-plane/API behavior changes;
- release binary can serve the dashboard from itself.

### Verification

At minimum:

```bash
go test ./internal/bootstrap ./internal/server/...
go test ./...
go vet ./...
npm run build:gui
npm run privacy:scan
```

Also build a test binary from staged release assets in a temporary directory with no neighboring `gui/dist`, start it with isolated `BENES_HOME`, and verify the dashboard root loads.

Do not publish anything.

---

## PR B — Deterministic native artifact builder

### Goal

Create a deterministic repository-owned artifact builder used by the release workflow.

### Expected files

Likely create:

- `scripts/build-release-artifacts.ts` or an equivalently focused helper;
- focused tests for platform mapping, names, checksum manifest, archive contents.

Likely modify:

- package/release helper tests;
- no release mutation yet unless necessary to expose the plan.

### Builder responsibilities

Given an exact audited source SHA and release version:

1. require a built dashboard;
2. stage the dashboard for embedding;
3. build each supported Go binary;
4. inject the release version;
5. create one archive per target;
6. preserve Unix executable mode;
7. generate `SHA256SUMS`;
8. produce machine-readable artifact metadata for the workflow/release layer;
9. never create a tag, release, or npm mutation itself.

### Determinism

Tests should normalize archive metadata where necessary so rebuilding the same source/version/toolchain gives the same logical artifact contents.

If byte-for-byte archive reproducibility is not practical with the selected native archiver, explicitly define which metadata is normalized and test the complete extracted file/checksum contract.

### Security

The builder must not:

- fetch executable tooling from arbitrary URLs;
- evaluate artifact filenames from untrusted input;
- write outside its staging directory;
- print secrets.

---

## PR C — Release workflow integration

### Goal

Extend the existing single release authority to upload native artifacts after the same audited-SHA gates used for npm.

### Expected files

Likely modify:

- `.github/workflows/release.yml`;
- `scripts/release.ts` only where orchestration contract genuinely requires it;
- `scripts/lib/release/**` only if artifact state must participate in existing recovery/idempotency;
- release automation tests.

### Authority rule

`scripts/release.ts` remains the release coordinator.

Do not create:

- a second release workflow that can independently tag/publish;
- a GoReleaser-owned tag path;
- a post-release side channel that uploads unaudited binaries.

### Ordering

The exact mutation order must be designed around existing release recovery semantics.

At minimum, no release should claim native artifacts exist until the workflow has successfully built and verified them.

If artifact upload occurs after npm publish, recovery semantics must explicitly handle npm-success / artifact-upload-failure without creating conflicting releases.

If artifact upload occurs before npm publish, avoid exposing a release as final before npm succeeds.

Use the existing release domain's fresh/recover/complete/conflict model rather than inventing another state machine.

### Permissions

Keep workflow permissions least-privilege.

Artifact attestations may require an additional permission; add only the minimum scope supported by the selected GitHub action/API and pin third-party actions to full commit SHAs.

### Tests

Automation tests must pin:

- exact audited SHA use;
- expected artifact names;
- checksum manifest presence;
- no token-based npm fallback;
- no second release authority;
- no artifact upload from an unaudited ref.

---

## PR D — POSIX standalone installer

### Goal

Turn `scripts/install.sh` into a release-artifact installer that does not require Node, npm, or Go.

### Interface

Required capabilities:

```bash
./install.sh --version <version>
BENES_INSTALL_BASE=<base> ./install.sh --version <version>
./install.sh --version <version> --file <local-archive>
./install.sh --version <version> --prefix <dir>
```

Exact flag syntax may be adjusted once, before implementation, then frozen by tests.

### Resolution

The installer must:

1. detect supported OS/architecture;
2. map it to exactly one release filename;
3. resolve the checksum manifest from the same release/mirror base;
4. verify SHA-256 before extraction/install;
5. install only the expected executable;
6. preserve executable mode;
7. run the installed binary's version/help check;
8. print an exact PATH instruction if the chosen user prefix is not already on PATH.

### Default prefix

Use a user-writable location.

Do not call `sudo` automatically.

System prefixes are explicit operator choices and may require privilege.

### Mirror contract

`BENES_INSTALL_BASE` changes only the artifact base URL/path.

It must not alter archive names or checksum semantics.

An internal mirror should be able to copy release files unchanged.

### Offline contract

`--file` must perform no network access.

The checksum manifest must be supplied/read locally according to one documented rule, for example alongside the archive or through an explicit checksum-manifest flag.

Pick one unambiguous contract and test it.

### Tests

Add shell/helper tests for:

- every supported OS/arch mapping;
- unsupported OS/arch;
- pinned version URL;
- mirror URL;
- valid checksum;
- checksum mismatch;
- missing checksum;
- offline mode with a network-disabled harness;
- install prefix;
- pre-existing unrelated `benes` on PATH;
- successful installed binary verification.

---

## PR E — PowerShell standalone installer

### Goal

Provide the same artifact/checksum/offline contract on Windows PowerShell 5.1+.

### Requirements

Keep parity with the POSIX installer for:

- version pinning;
- mirror base;
- local/offline archive;
- checksum verification;
- user-writable prefix;
- unsupported architecture error;
- exact installed binary health/version verification.

Do not require Node, npm, Go, winget, Chocolatey, or PowerShell 7 for the basic path.

Use built-in PowerShell/.NET capabilities available on the supported Windows floor.

### Default location

Prefer a current-user location under `LOCALAPPDATA` or another established Benes per-user path.

Do not modify machine-wide PATH without explicit user action.

If user PATH is modified, make the operation explicit, idempotent, and tested.

### Tests

Use a testable PowerShell structure rather than one monolithic script.

Tests must cover child-process exit propagation, URL/path selection, checksums, local file mode, and no-network offline installation.

---

## PR F — npm binary-first contract

### Goal

After native artifacts are real and verified, remove Go compilation/toolchain download from the normal npm end-user path.

This PR requires a design decision based on npm operational constraints at implementation time.

### Required outcome

A normal:

```bash
npm install -g benes
benes help
```

must not require Go.

It must not silently set/fetch `GOTOOLCHAIN` as the ordinary installed-product path.

### Candidate design

The preferred candidate is platform-specific optional npm packages:

```text
benes
benes-darwin-arm64
benes-darwin-amd64
benes-linux-amd64
benes-linux-arm64
benes-win32-x64
```

The main `benes` package remains the JS launcher/metadata package and declares the native packages as optional dependencies constrained by `os`/`cpu`.

Do not lock these exact package names until npm availability and trusted-publishing/release implications are checked.

### Launcher resolution target

Conceptually:

1. `BENES_BIN`;
2. installed matching native package;
3. adjacent development/release binary;
4. source `go run` only for an explicit development path.

A source fallback may remain for repository checkouts, but it must not make a normal published install look successful when the native package is missing.

An explicit environment opt-in for Go fallback may be retained if genuinely useful.

### `--omit=optional`

If the matching native package is absent because optional dependencies were deliberately omitted, fail with a clear message that points to:

- reinstalling normally;
- or installing a release archive.

Do not silently compile the product.

### Package surface

Once npm no longer needs Go sources at runtime, re-evaluate whether `cmd/`, `internal/`, `go.mod`, and `go.sum` should remain in the npm package.

Remove them only after launcher/package tests prove a normal global install works without them.

### Release coordination

All platform packages and the main package must be version-aligned and published under the same audited release authority.

Do not create partially published package sets without explicit recovery semantics.

---

# Wave 2 — managed distribution channels

Start only after Wave 1 is proven.

## Homebrew

Add a formula/tap only after stable GitHub archive URLs/checksums exist.

The formula must consume the same release archive/checksum contract rather than rebuild Benes from source.

## winget

Add winget after the Windows archive/install behavior is stable.

Do not make winget the only non-npm Windows path.

## Container image

A container is a gateway deployment option, not the primary workstation installer.

Requirements:

- non-root runtime where practical;
- no embedded secrets;
- non-loopback configuration still requires admission token;
- docs must not call this multi-tenant until #2 is complete.

## Windows native service

Remove runtime WinSW network dependency from the supported offline/native-service path.

Choose one:

- vendor the pinned verified WinSW executable in the Windows release artifact;
- or require an operator-supplied verified WinSW file and fail closed.

Do not download WinSW during an offline install or silently on default service installation.

Task Scheduler remains a valid no-download default.

---

# Wave 3 — enterprise packaging

Only after binary/archive semantics stabilize:

- MSI;
- macOS pkg;
- machine-wide/system service installation;
- Intune/Jamf-oriented deployment;
- Authenticode;
- notarization;
- SBOM/policy artifacts beyond Wave 1 checksums/attestations.

Do not block Wave 1 on signing certificates that do not yet exist.

---

# Relationship to Issue #2

This plan intentionally stops at **deployability**.

It makes it practical to place Benes on:

- user workstations;
- managed company laptops;
- internal gateway hosts;
- offline/mirrored environments.

It does not make one gateway safe for mutually independent tenants.

Issue #2 separately owns:

- trusted tenant identity;
- tenant policy;
- credential isolation;
- account-pool isolation;
- history/usage isolation;
- tenant catalog projection;
- cross-tenant negative tests.

No PR from this plan should use `Closes #2`.

Plan PRs may use `Related to #2` or `Enabler for #2`.

---

# Documentation rules

Do not change the public install docs to claim a future path has shipped before the corresponding implementation is merged and released.

Each implementation PR updates only the shipped-state documentation it actually changes.

In particular:

- PR A may document embedded standalone capability for developers/tests only if user-visible behavior changes;
- PRs B/C may document release artifacts only once release generation/upload exists;
- PRs D/E update installation docs when standalone installers are real;
- PR F updates npm requirements only when Go is no longer required for normal npm use.

Keep public docs truthful during intermediate states.

---

# Security invariants

1. **No unverified binary execution.** Downloaded/local archives are verified before extraction/install.
2. **No hidden compiler bootstrap.** Normal installed paths do not fetch Go.
3. **No second publisher.** Release authority stays centralized.
4. **No secret logging.** Install/release output never prints tokens, API keys, or private credentials.
5. **No surprise privilege.** User install does not invoke sudo/admin automatically.
6. **No implicit telemetry.**
7. **No gateway-as-tenancy claim.**
8. **No arbitrary artifact path writes.** Archive extraction must defend against traversal.
9. **No floating third-party Actions.** Pin by full commit SHA.
10. **No release from an unaudited SHA.**

---

# Acceptance — Wave 1

Wave 1 is complete only when all of the following are proven.

## Standalone machine

On a clean supported machine with neither Node nor Go installed:

1. obtain a Benes release archive;
2. verify it through the release checksum contract;
3. install it through the supported standalone installer;
4. run `benes version` and receive the exact release semver;
5. run `benes start`;
6. load the dashboard from the binary;
7. stop/restart normally;
8. observe no install/runtime dependency on Node or Go.

## Offline

With network access disabled:

1. provide the archive and required local checksum metadata;
2. install successfully;
3. run `benes version`;
4. run `benes start`;
5. load the embedded dashboard.

No installer attempt may contact GitHub/npm in this path.

## Mirror

An operator can mirror the release files without renaming them and set one base location.

The installer resolves the same filenames/checksums from that mirror.

## npm

A normal npm global installation works with Node alone and does not invoke Go.

If its native optional package is missing, it fails clearly rather than downloading/compiling Go silently.

## Release

One audited release operation owns:

- version;
- npm packages;
- Git tag;
- GitHub Release;
- native archives;
- checksum manifest.

Recovery/idempotency behavior is tested for partial external state.

---

# Verification expectations for every implementation PR

Use the narrowest focused tests first, then repository-required gates.

Depending on scope, final evidence includes:

```bash
go test ./...
go vet ./...
npm run test:automation
npm run privacy:scan
npm run lint:gui
npm run build:gui
cd docs && npm ci && npm run build
```

Packaging/release PRs must additionally run the exact packaging/local-PR gates required by current `AGENTS.md` and `MAINTAINERS.md`.

For each PR:

- record exact head SHA;
- record exact `dev` base/reconciliation state;
- distinguish hosted runner infrastructure failure from executed test failure;
- do not merge automatically.

---

# Recommended implementation order

1. **A — embedded dashboard fallback**
2. **B — deterministic native artifact builder**
3. **C — release workflow integration**
4. **D — POSIX standalone installer**
5. **E — PowerShell standalone installer**
6. **F — npm binary-first contract**
7. Wave 2 channels only after Wave 1 is released/proven
8. Wave 3 enterprise packaging only after archive/install contracts are stable

A–F should normally be separate PRs.

If two adjacent slices turn out to be inseparable, the implementing agent must explain the concrete coupling before combining them.

---

# Plan-PR acceptance

This PR is complete when:

- this document is the only intended repository change;
- every current-state assertion has been checked against current `dev`;
- #2 is linked only as an enabler relationship;
- no runtime/release/install behavior changes;
- no public docs claim the plan has shipped;
- no old-history commit is imported;
- no implementation branch is started from this plan PR.

After this plan is accepted, implementation still requires an explicit separate instruction.
