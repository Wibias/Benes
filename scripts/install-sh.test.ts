import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test, { after, describe } from "node:test";
import { fileURLToPath } from "node:url";

/**
 * Behavioural contract for scripts/install.sh.
 *
 * The installer runs against a PATH that holds nothing but generated shims, so
 * "this tool is missing" is a truthful state instead of a guess, and no real
 * global package is ever installed. The shims use shell builtins only.
 */

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..");
const INSTALLER = toPosix(join(REPO, "scripts", "install.sh"));
const NPM_PREFIX = "/fake/npm/prefix";

/**
 * A Windows `bash` on PATH is usually the WSL launcher, which cannot see a
 * Windows PATH or a Windows temp directory. Prefer the MSYS shell shipped with
 * Git for Windows; on POSIX hosts the ambient bash is already the right one.
 */
function resolveBash(): string | null {
  if (process.platform !== "win32") return "bash";
  const candidates = [
    process.env.BENES_TEST_BASH,
    process.env.ProgramFiles ? join(process.env.ProgramFiles, "Git", "bin", "bash.exe") : null,
    process.env["ProgramFiles(x86)"] ? join(process.env["ProgramFiles(x86)"], "Git", "bin", "bash.exe") : null,
    process.env.LOCALAPPDATA ? join(process.env.LOCALAPPDATA, "Programs", "Git", "bin", "bash.exe") : null,
  ];
  for (const candidate of candidates) {
    if (candidate && existsSync(candidate)) return candidate;
  }
  return null;
}

function toPosix(path: string): string {
  return path
    .replace(/\\/g, "/")
    .replace(/^([A-Za-z]):/, (_match, drive: string) => `/${drive.toLowerCase()}`);
}

const BASH = resolveBash();
const SKIP = BASH ? false : "needs a POSIX bash; on Windows install Git for Windows or set BENES_TEST_BASH";

const roots: string[] = [];
after(() => {
  for (const root of roots) rmSync(root, { recursive: true, force: true });
});

function scratch(): string {
  const root = mkdtempSync(join(tmpdir(), "benes-install-sh-"));
  roots.push(root);
  return root;
}

function shim(bin: string, name: string, body: string[]): void {
  const path = join(bin, name);
  writeFileSync(path, `#!/bin/sh\n${body.join("\n")}\n`, "utf8");
  chmodSync(path, 0o755);
}

type Toolchain = {
  /** null leaves node off PATH entirely. */
  nodeVersion: string | null;
  /** null leaves npm off PATH entirely. */
  npm: { prefix?: string; exit?: number } | null;
  go: boolean;
  /** null leaves the launcher off PATH entirely. */
  benes: { helpExit: number } | null;
};

type Run = { status: number | null; stdout: string; stderr: string; trace: string[] };

function install(toolchain: Toolchain): Run {
  const root = scratch();
  const bin = join(root, "bin");
  const home = join(root, "home");
  mkdirSync(bin, { recursive: true });
  mkdirSync(home, { recursive: true });
  const tracePath = join(root, "trace.log");

  if (toolchain.nodeVersion !== null) {
    const major = toolchain.nodeVersion.split(".")[0];
    shim(bin, "node", [
      'case "$1" in',
      `  -p) printf '%s\\n' "${major}" ;;`,
      `  --version) printf 'v%s\\n' "${toolchain.nodeVersion}" ;;`,
      "  *) exit 0 ;;",
      "esac",
    ]);
  }

  if (toolchain.npm) {
    shim(bin, "npm", [
      'if [ -n "${BENES_TEST_TRACE:-}" ]; then printf \'npm %s\\n\' "$*" >> "$BENES_TEST_TRACE"; fi',
      'case "$1" in',
      `  prefix) printf '%s\\n' "${toolchain.npm.prefix ?? NPM_PREFIX}" ;;`,
      `  install) exit ${toolchain.npm.exit ?? 0} ;;`,
      "  *) exit 0 ;;",
      "esac",
    ]);
  }

  if (toolchain.go) {
    shim(bin, "go", ["printf 'go version go1.27.0 linux/amd64\\n'"]);
  }

  if (toolchain.benes) {
    shim(bin, "benes", [
      'if [ -n "${BENES_TEST_TRACE:-}" ]; then printf \'benes %s\\n\' "$*" >> "$BENES_TEST_TRACE"; fi',
      `if [ "$1" = "help" ]; then exit ${toolchain.benes.helpExit}; fi`,
      "exit 0",
    ]);
  }

  const result = spawnSync(BASH as string, [INSTALLER], {
    cwd: REPO,
    encoding: "utf8",
    env: {
      ...process.env,
      PATH: toPosix(bin),
      HOME: toPosix(home),
      BENES_TEST_TRACE: toPosix(tracePath),
    },
  });

  return {
    status: result.status,
    stdout: result.stdout ?? "",
    stderr: result.stderr ?? "",
    trace: existsSync(tracePath)
      ? readFileSync(tracePath, "utf8").split("\n").map((line) => line.trim()).filter(Boolean)
      : [],
  };
}

const READY: Toolchain = {
  nodeVersion: "18.19.0",
  npm: {},
  go: true,
  benes: { helpExit: 0 },
};

describe("scripts/install.sh", { skip: SKIP }, () => {
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
    assert.match(run.stderr, /npm is required to install the published @wibias\/benes package\./);
    assert.deepEqual(run.trace, []);
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
    assert.equal(run.trace.includes("npm install -g @wibias/benes"), true);
  });

  test("accepts a current supported major", () => {
    const run = install({ ...READY, nodeVersion: "24.15.0" });
    assert.equal(run.status, 0, run.stderr);
    assert.match(run.stdout, /Using Node v24\.15\.0/);
  });

  test("propagates an npm install failure instead of reporting success", () => {
    const run = install({ ...READY, npm: { exit: 3 } });
    assert.equal(run.status, 3);
    assert.doesNotMatch(run.stdout, /is installed\. Next:/);
  });

  test("fails when the launcher never reaches PATH and points at the npm global bin", () => {
    const run = install({ ...READY, benes: null });
    assert.equal(run.status, 1);
    assert.match(
      run.stderr,
      /@wibias\/benes is installed but benes is not on PATH\. Add the npm global bin directory, then open a new shell: \/fake\/npm\/prefix\/bin/,
    );
  });

  test("honours a custom npm global prefix in that failure", () => {
    const run = install({ ...READY, npm: { prefix: "/custom/prefix" }, benes: null });
    assert.match(run.stderr, /\/custom\/prefix\/bin/);
  });

  test("fails when the launcher exists but its help entry point fails", () => {
    const run = install({ ...READY, benes: { helpExit: 5 } });
    assert.equal(run.status, 1);
    assert.match(run.stderr, /benes is on PATH but 'benes help' failed\. Check the global npm install\./);
  });

  test("completes the documented handoff on a clean toolchain", () => {
    const run = install(READY);
    assert.equal(run.status, 0, run.stderr);
    assert.equal(run.stderr, "");
    assert.match(run.stdout, /^Installing benes\.\.\.$/m);
    assert.match(run.stdout, /^Using Node v18\.19\.0$/m);
    assert.match(run.stdout, /^Using go version go1\.27\.0 linux\/amd64$/m);
    assert.match(run.stdout, /^benes is installed\. Next: benes init$/m);
  });

  test("passes the package name as its own argv entry, never through a shell string", () => {
    const run = install(READY);
    assert.deepEqual(run.trace, ["npm install -g @wibias/benes", "benes help"]);
  });
});
