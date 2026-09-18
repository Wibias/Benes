import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test, { after, describe } from "node:test";
import { fileURLToPath } from "node:url";

/**
 * Behavioural contract for scripts/install.ps1.
 *
 * Windows PowerShell is driven against a PATH whose only interesting entries
 * are generated `.cmd` shims, so no real global package is installed. The
 * observed exit status is the process status, which is what a caller sees.
 */

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..");
const INSTALLER = join(REPO, "scripts", "install.ps1");

function resolvePowerShell(): string | null {
  if (process.platform !== "win32") return null;
  const candidates = [
    process.env.BENES_TEST_POWERSHELL,
    process.env.SystemRoot ? join(process.env.SystemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe") : null,
  ];
  for (const candidate of candidates) {
    if (candidate && existsSync(candidate)) return candidate;
  }
  return null;
}

const POWERSHELL = resolvePowerShell();
const SKIP = POWERSHELL ? false : "needs Windows PowerShell 5.1";

const roots: string[] = [];
after(() => {
  for (const root of roots) rmSync(root, { recursive: true, force: true });
});

function scratch(): string {
  const root = mkdtempSync(join(tmpdir(), "benes-install-ps1-"));
  roots.push(root);
  return root;
}

function shim(bin: string, name: string, body: string[]): void {
  writeFileSync(join(bin, name), ["@echo off", ...body, "exit /b 0", ""].join("\r\n"), "utf8");
}

type Toolchain = {
  /** null leaves node off PATH entirely. */
  nodeVersion: string | null;
  /** null leaves npm off PATH entirely. `decoy` adds a competing npm.bat. */
  npm: { prefix?: string; exit?: number; decoy?: boolean; stdout?: string } | null;
  go: boolean;
  /** null leaves the launcher off PATH entirely. `decoy` adds benes.bat. */
  benes: { helpExit: number; decoy?: boolean } | null;
};

type Run = { status: number | null; stdout: string; stderr: string; trace: string[] };

function traceLine(label: string): string {
  return `if not "%BENES_TEST_TRACE%"=="" echo ${label} %* >> "%BENES_TEST_TRACE%"`;
}

function install(toolchain: Toolchain): Run {
  const root = scratch();
  const bin = join(root, "bin");
  mkdirSync(bin, { recursive: true });
  const tracePath = join(root, "trace.log");

  if (toolchain.nodeVersion !== null) {
    // scripts/install.ps1 evaluates `node -p "process.versions.node"` and takes
    // the major itself, unlike the POSIX installer which asks node for the major.
    shim(bin, "node.cmd", [
      `if "%1"=="-p" echo ${toolchain.nodeVersion}`,
      `if "%1"=="--version" echo v${toolchain.nodeVersion}`,
    ]);
  }

  if (toolchain.npm) {
    const body = [
      traceLine("npm.cmd"),
      `if "%1"=="prefix" echo ${toolchain.npm.prefix ?? "C:\\fake\\npm\\prefix"}`,
      ...(toolchain.npm.stdout ? [`if "%1"=="install" echo ${toolchain.npm.stdout}`] : []),
      `if "%1"=="install" exit /b ${toolchain.npm.exit ?? 0}`,
    ];
    shim(bin, "npm.cmd", body);
    if (toolchain.npm.decoy) shim(bin, "npm.bat", [traceLine("npm.bat")]);
  }

  if (toolchain.go) {
    shim(bin, "go.cmd", ["echo go version go1.27.0 windows/amd64"]);
  }

  if (toolchain.benes) {
    shim(bin, "benes.cmd", [traceLine("benes.cmd"), `exit /b ${toolchain.benes.helpExit}`]);
    if (toolchain.benes.decoy) shim(bin, "benes.bat", [traceLine("benes.bat")]);
  }

  // Only the generated shims plus the system directory. The system directory is
  // needed so PowerShell can reach cmd.exe to run a .cmd shim; it never holds a
  // node/npm/go/benes candidate, so "missing tool" stays a truthful state.
  const path = [bin, process.env.SystemRoot ? join(process.env.SystemRoot, "System32") : null]
    .filter(Boolean)
    .join(";");

  const result = spawnSync(POWERSHELL as string, ["-NoProfile", "-ExecutionPolicy", "Bypass", "-File", INSTALLER], {
    cwd: REPO,
    encoding: "utf8",
    env: { ...process.env, PATH: path, BENES_TEST_TRACE: tracePath },
  });

  return {
    status: result.status,
    stdout: result.stdout ?? "",
    stderr: result.stderr ?? "",
    trace: existsSync(tracePath)
      ? readFileSync(tracePath, "utf8").split(/\r?\n/).map((line) => line.trim()).filter(Boolean)
      : [],
  };
}

const READY: Toolchain = {
  nodeVersion: "18.19.0",
  npm: {},
  go: true,
  benes: { helpExit: 0 },
};

describe("scripts/install.ps1", { skip: SKIP }, () => {
  test("refuses a missing node before it looks at the rest of the toolchain", () => {
    const run = install({ nodeVersion: null, npm: {}, go: true, benes: { helpExit: 0 } });
    assert.equal(run.status, 1);
    assert.match(run.stderr, /Node\.js 18\+ is required\. Install it from https:\/\/nodejs\.org\/ and rerun\./);
    assert.doesNotMatch(run.stderr, /npm is required/);
    assert.doesNotMatch(run.stdout, /Using Node/);
  });

  test("refuses a missing npm", () => {
    const run = install({ ...READY, npm: null });
    assert.equal(run.status, 1);
    assert.match(run.stderr, /npm is required to install the published benes package\./);
  });

  test("refuses a missing go", () => {
    const run = install({ ...READY, go: false });
    assert.equal(run.status, 1);
    assert.match(run.stderr, /Go 1\.27\.0 is required\. Install it from https:\/\/go\.dev\/dl\/ and rerun\./);
  });

  test("rejects Node below the floor and names the observed version", () => {
    const run = install({ ...READY, nodeVersion: "17.9.1" });
    assert.equal(run.status, 1);
    assert.match(run.stderr, /Node\.js 18\+ is required\. Current version: v17\.9\.1/);
    assert.deepEqual(run.trace, []);
  });

  test("accepts the floor version itself", () => {
    const run = install({ ...READY, nodeVersion: "18.0.0" });
    assert.equal(run.status, 0, run.stderr);
  });

  test("accepts a current supported major", () => {
    const run = install({ ...READY, nodeVersion: "24.15.0" });
    assert.equal(run.status, 0, run.stderr);
    assert.match(run.stdout, /Using Node v24\.15\.0/);
    assert.match(run.stdout, /Using go version go1\.27\.0 windows\/amd64/);
  });

  test("propagates the npm install exit code rather than a generic failure", () => {
    const run = install({ ...READY, npm: { exit: 3 } });
    assert.equal(run.status, 3);
    assert.match(run.stderr, /npm install -g benes failed with exit code 3/);
    assert.doesNotMatch(run.stdout, /is installed\. Next:/);
  });

  test("fails when the launcher never reaches PATH and points at the npm global prefix", () => {
    const run = install({ ...READY, benes: null });
    assert.equal(run.status, 1);
    assert.match(
      run.stderr,
      /benes is installed but not on PATH\. Add the npm global bin directory, then reopen PowerShell: C:\\fake\\npm\\prefix/,
    );
  });

  test("keeps npm install output visible while retaining the npm launcher for prefix lookup", () => {
    const run = install({
      ...READY,
      npm: { prefix: "C:\\visible\\npm\\prefix", stdout: "npm install visible output" },
      benes: null,
    });
    assert.equal(run.status, 1);
    assert.match(run.stdout, /^npm install visible output$/m);
    assert.match(
      run.stderr,
      /benes is installed but not on PATH\. Add the npm global bin directory, then reopen PowerShell: C:\\visible\\npm\\prefix/,
    );
    assert.deepEqual(
      run.trace.filter((line) => line.startsWith("npm")),
      ["npm.cmd install -g benes", "npm.cmd prefix -g"],
    );
  });

  test("propagates the launcher help exit code", () => {
    const run = install({ ...READY, benes: { helpExit: 5 } });
    assert.equal(run.status, 5);
    assert.match(run.stderr, /benes is on PATH but help failed with exit code 5\./);
  });

  test("prefers the npm.cmd command shim over a competing npm.bat", () => {
    const run = install({ ...READY, npm: { decoy: true } });
    assert.equal(run.status, 0, run.stderr);
    assert.deepEqual(
      run.trace.filter((line) => line.startsWith("npm")),
      ["npm.cmd install -g benes"],
    );
  });

  test("prefers the benes.cmd command shim over a competing benes.bat", () => {
    const run = install({ ...READY, benes: { helpExit: 0, decoy: true } });
    assert.equal(run.status, 0, run.stderr);
    assert.deepEqual(
      run.trace.filter((line) => line.startsWith("benes")),
      ["benes.cmd help"],
    );
  });

  test("completes the documented handoff on a clean toolchain", () => {
    const run = install(READY);
    assert.equal(run.status, 0, run.stderr);
    assert.match(run.stdout, /^Installing benes\.\.\.$/m);
    assert.match(run.stdout, /^Using Node v18\.19\.0$/m);
    assert.match(run.stdout, /^benes is installed\. Next: benes init$/m);
    assert.deepEqual(run.trace, ["npm.cmd install -g benes", "benes.cmd help"]);
  });
});
