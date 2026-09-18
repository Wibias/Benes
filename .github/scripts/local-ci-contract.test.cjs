"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const { execFileSync } = require("node:child_process");
const fs = require("node:fs");
const os = require("node:os");
const path = require("node:path");
const { pathToFileURL } = require("node:url");

const ROOT = path.join(__dirname, "..", "..");
const read = (rel) => fs.readFileSync(path.join(ROOT, rel), "utf8");

describe("Local CI mirrors hosted cross-platform evidence", () => {
  const ci = read(".github/workflows/ci.yml");
  const goCore = read(".github/workflows/go-core.yml");
  const windows = read("scripts/ci-local.ps1");
  const linux = read("scripts/ci-local-linux.sh");
  const macos = read("scripts/ci-local-macos.sh");

  it("keeps the hosted Go core commands in every full OS runner", () => {
    for (const command of ["go build", "go test ./...", "go vet ./..."]) {
      assert.ok(goCore.includes(command), `go-core.yml missing ${command}`);
      assert.ok(windows.includes(command), `ci-local.ps1 missing ${command}`);
      assert.ok(linux.includes(command), `ci-local-linux.sh missing ${command}`);
      assert.ok(macos.includes(command), `ci-local-macos.sh missing ${command}`);
    }
    assert.match(goCore, /go test -race \.\/\.\.\./);
    assert.match(linux, /go test -race \.\/\.\.\./);
  });

  it("keeps Linux gates and credential smoke represented locally", () => {
    for (const command of [
      "node --experimental-strip-types scripts/privacy-scan.ts",
      "go run ./cmd/benes help",
      "scripts/keyring-smoke.ts",
      "dbus-run-session",
      "gnome-keyring-daemon",
    ]) {
      assert.ok(ci.includes(command), `ci.yml missing ${command}`);
      assert.ok(linux.includes(command), `ci-local-linux.sh missing ${command}`);
    }
  });

  it("keeps packaging checks aligned and normalizes npm pack metadata", () => {
    const packageJson = JSON.parse(read("package.json"));
    assert.equal(
      packageJson.bin?.benes,
      "bin/benes.mjs",
      "npm must preserve the Benes CLI bin entry instead of dropping an invalid manifest path",
    );
    assert.match(
      packageJson.scripts["build:gui"],
      /npm ci --dry-run=false/,
      "publish dry-runs must still install GUI dependencies for a real build",
    );
    for (const script of [ci, linux, macos, windows]) {
      assert.match(script, /npm pack --json/);
      assert.match(script, /npm-pack-json\.cjs/);
      assert.match(script, /pack\.json/);
      assert.doesNotMatch(script, /require\('\.\/pack\.json'\)\[0\]/);
    }
    for (const script of [linux, macos, windows]) {
      assert.match(script, /npm install --omit=dev/);
      assert.match(script, /npm run build:gui/);
    }
    assert.doesNotMatch(ci, /wibias-benes-\*\.tgz/);
  });

  it("pins Go 1.27.0 consistently across runtime and install surfaces", () => {
    const goMod = read("go.mod");
    const launcher = read("bin/benes.mjs");
    const installSh = read("scripts/install.sh");
    const installPs1 = read("scripts/install.ps1");
    const readme = read("README.md");
    const installDocs = read("docs/src/content/docs/start/install.md");

    assert.match(goMod, /^go 1\.27\.0$/m);
    assert.doesNotMatch(goMod, /^toolchain\s+/m, "go mod tidy must keep the Go 1.27 module canonical");
    assert.match(launcher, /go1\.27\.0/);
    for (const surface of [installSh, installPs1, readme, installDocs]) {
      assert.match(surface, /Go 1\.27\.0/);
      assert.doesNotMatch(surface, /Go 1\.26\.6/);
    }
  });

  it("resolves the CLI in the documented order and pins the Go toolchain", async () => {
    const launcher = await import(pathToFileURL(path.join(ROOT, "bin", "benes.mjs")).href);
    const { launcherEnv, packagedBinaryPath, packagedBinaryUsable, resolveLauncher } = launcher;

    assert.deepEqual(
      resolveLauncher({ env: { BENES_BIN: "  /opt/benes/cli  " }, root: ROOT }),
      { command: "/opt/benes/cli", args: [], source: "BENES_BIN" },
      "an explicit BENES_BIN wins over anything on disk",
    );

    const emptyRoot = fs.mkdtempSync(path.join(os.tmpdir(), "benes-launch-empty-"));
    assert.deepEqual(
      resolveLauncher({ platform: "linux", env: {}, root: emptyRoot }),
      { command: "go", args: ["run", "./cmd/benes"], source: "go run ./cmd/benes" },
      "without a packaged build the launcher runs the packaged module",
    );

    const packagedRoot = fs.mkdtempSync(path.join(os.tmpdir(), "benes-launch-packaged-"));
    const binary = packagedBinaryPath(process.platform, packagedRoot);
    fs.writeFileSync(binary, "");
    if (process.platform !== "win32") fs.chmodSync(binary, 0o755);
    assert.deepEqual(
      resolveLauncher({ env: {}, root: packagedRoot }),
      { command: binary, args: [], source: "the packaged build" },
      "a runnable packaged build is used ahead of go run",
    );

    assert.equal(packagedBinaryPath("win32", ROOT), path.join(ROOT, "benes.exe"));
    assert.equal(packagedBinaryPath("linux", ROOT), path.join(ROOT, "benes"));
    assert.equal(
      packagedBinaryUsable("/tmp/benes", "linux", {
        existsSync: () => true,
        accessSync: () => { throw new Error("EACCES"); },
      }),
      false,
      "a packaged build without an execute bit falls back to go run",
    );
    assert.equal(packagedBinaryUsable("/tmp/benes", "linux", { existsSync: () => true, accessSync: () => {} }), true);
    assert.equal(packagedBinaryUsable("/tmp/benes", "linux", { existsSync: () => false, accessSync: () => {} }), false);
    assert.equal(launcherEnv({}).GOTOOLCHAIN, "go1.27.0");
    assert.equal(launcherEnv({ GOTOOLCHAIN: "local" }).GOTOOLCHAIN, "local");
    assert.equal(launcherEnv({ GOTOOLCHAIN: "   " }).GOTOOLCHAIN, "go1.27.0");
  });

  it("keeps macOS fallback explicitly compile-only for both Darwin architectures", () => {
    assert.match(windows, /GOOS/);
    assert.match(windows, /darwin/);
    assert.match(windows, /amd64/);
    assert.match(windows, /arm64/);
    assert.match(windows, /go test -c/);
    assert.match(windows, /CGO_ENABLED/);
  });

  it("uses the default WSL distribution unless an explicit override is supplied", () => {
    assert.match(windows, /\[string\]\$WslDistribution\s*=\s*""/);
    assert.doesNotMatch(windows, /\[string\]\$WslDistribution\s*=\s*"Ubuntu"/);
    assert.match(windows, /--distribution/);
    assert.match(windows, /--exec/);
  });

  it("bootstraps WSL from a Linux-native checkout of the Windows git common dir", () => {
    assert.match(windows, /rev-parse --path-format=absolute --git-common-dir/);
    assert.match(windows, /BENES_CI_SOURCE_GIT_DIR/);
    assert.match(linux, /BENES_CI_SOURCE_GIT_DIR/);
    assert.match(linux, /git init/);
    assert.match(linux, /git -C "\$worktree" fetch --no-tags --update-shallow/);
    assert.match(linux, /"\+\$\{expected_sha\}:refs\/heads\/benes-ci"/);
    assert.match(linux, /git -C "\$worktree" checkout --detach "\$expected_sha"/);
    assert.doesNotMatch(linux, /checkout --detach FETCH_HEAD/);
    assert.match(linux, /cd "\$worktree"/);
  });

  it("binds WSL and optional SSH macOS evidence to the exact local commit", () => {
    assert.match(windows, /ci-local-linux\.sh/);
    assert.match(windows, /ci-local-macos\.sh/);
    assert.match(windows, /BENES_CI_EXPECTED_SHA/);
    assert.match(linux, /BENES_CI_EXPECTED_SHA/);
    assert.match(macos, /BENES_CI_EXPECTED_SHA/);
    assert.match(windows, /ssh/);
    assert.match(macos, /Darwin/);
  });

  it("lets local PR CI skip OS gates that are outside the path scope", () => {
    assert.match(windows, /function Test-CiScope/);
    assert.match(windows, /\[switch\]\$SkipWindows/);
    assert.match(linux, /ci-scope\.sh/);
    assert.match(macos, /ci-scope\.sh/);
    assert.match(linux, /ci_scope_enabled/);
    assert.match(macos, /ci_scope_enabled/);
  });

  it("keeps native command output out of PowerShell function return values", () => {
    const invokeChecked = windows.match(/function Invoke-Checked \{[\s\S]*?\n\}/)?.[0] ?? "";
    assert.match(invokeChecked, /& \$filePath @argumentList \| Out-Host/);
  });

  it("decodes native command output as UTF-8 on Windows consoles", () => {
    const localPr = read("scripts/local-pr.ps1");
    for (const script of [windows, localPr]) {
      assert.match(script, /function Enable-Utf8Console/);
      assert.match(script, /\[Console\]::OutputEncoding\s*=\s*\$utf8/);
      assert.match(script, /\$script:OutputEncoding\s*=\s*\$utf8/);
      assert.match(script, /chcp\.com 65001/);
      assert.match(script, /Enable-Utf8Console/);
    }
  });

  it("round-trips UTF-8 box drawing through PowerShell native capture", {
    skip: process.platform !== "win32" ? "Windows PowerShell console encoding" : false,
  }, () => {
    const helper = windows.match(/function Enable-Utf8Console \{[\s\S]*?\n\}/)?.[0];
    assert.ok(helper, "Enable-Utf8Console must exist in ci-local.ps1");
    const probeDir = fs.mkdtempSync(path.join(os.tmpdir(), "benes-utf8-probe-"));
    const probe = path.join(probeDir, "probe.js");
    fs.writeFileSync(probe, "process.stdout.write(String.fromCharCode(0x251c, 0x2500, 0x25b6, 0x2713));\n", { flag: "wx" });
    try {
      const command = [helper, "Enable-Utf8Console", `& node -- ${probe}`].join("\r\n");
      const out = execFileSync("powershell", ["-NoProfile", "-Command", command], {
        encoding: "utf8",
        cwd: ROOT,
      });
      assert.equal(out.replace(/\r\n/g, "\n").trim(), "├─▶✓");
    } finally {
      fs.rmSync(probeDir, { recursive: true, force: true });
    }
  });

  it("runs this drift contract as part of local workflow validation", () => {
    const runner = read(".github/scripts/run-automation-tests.cjs");
    assert.match(windows, /run-automation-tests\.cjs/);
    assert.match(linux, /run-automation-tests\.cjs/);
    assert.match(ci, /run-automation-tests\.cjs/);
    assert.match(runner, /\.test\.cjs/);
    const listed = require("./run-automation-tests.cjs")
      .listAutomationTests()
      .map((file) => path.basename(file));
    assert.ok(listed.includes("local-ci-contract.test.cjs"));
    assert.ok(listed.includes("local-ci-linux-bootstrap.test.cjs"));
    assert.ok(listed.includes("npm-pack-json.test.cjs"));
  });

  it("pins one-command PR CI to unchanged remote head and base SHAs", () => {
    const localPrPath = path.join(ROOT, "scripts", "local-pr.ps1");
    assert.equal(fs.existsSync(localPrPath), true, "scripts/local-pr.ps1 must exist");

    const localPr = fs.readFileSync(localPrPath, "utf8");
    const packageJson = JSON.parse(read("package.json"));

    assert.match(localPr, /Position = 0/);
    assert.match(localPr, /\[int\]\$PR/);
    assert.match(localPr, /gh api/);
    assert.match(localPr, /refs\/pull\/\{0\}\/head/);
    assert.match(localPr, /-f \$Number/);
    assert.match(localPr, /refs\/heads\/main:refs\/remotes\/origin\/main/);
    assert.match(localPr, /head\.sha/);
    assert.match(localPr, /base\.sha/);
    assert.match(localPr, /worktree add --detach/);
    assert.match(localPr, /ci-pr-scope\.ts/);
    assert.match(localPr, /BENES_CI_SCOPE/);
    assert.match(localPr, /\[switch\]\$Full/);
    assert.match(localPr, /scripts[\\/]ci-local\.ps1/);
    assert.match(localPr, /head moved during verification/);
    assert.match(localPr, /base moved during verification/);
    assert.equal(
      packageJson.scripts["ci:pr"],
      "powershell -NoProfile -ExecutionPolicy Bypass -File ./scripts/local-pr.ps1",
    );
  });

  it("disables Git auto-maintenance for every PR metadata fetch", () => {
    const localPr = read("scripts/local-pr.ps1");
    assert.match(localPr, /function Invoke-GitFetchNoMaintenance/);
    assert.match(localPr, /git -c gc\.auto=0 -c maintenance\.auto=false -C \$Root fetch/);
    assert.doesNotMatch(localPr, /& git -C \$Root fetch/);

    const guardedFetchCalls = localPr.match(/Invoke-GitFetchNoMaintenance -Root \$Root -FetchArgs/g) ?? [];
    assert.ok(guardedFetchCalls.length >= 3, "all PR/base/baseline fetches must use the guarded fetch helper");
  });

  it("retries transient Windows worktree cleanup without hiding the original gate result", () => {
    const localPr = read("scripts/local-pr.ps1");
    for (const script of [windows, localPr]) {
      assert.match(script, /function Remove-DetachedWorktree/);
      assert.match(script, /for \(\$attempt = 1; \$attempt -le 5; \$attempt\+\+\)/);
      assert.match(script, /Start-Sleep -Milliseconds/);
      assert.match(script, /worktree prune --expire now/);
      assert.match(script, /gc\.auto=0/);
      assert.match(script, /maintenance\.auto=false/);
    }
  });
});
