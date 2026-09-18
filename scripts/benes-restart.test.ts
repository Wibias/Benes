import assert from "node:assert/strict";
import { spawnSync } from "node:child_process";
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from "node:fs";
import { tmpdir } from "node:os";
import { dirname, join } from "node:path";
import test, { after, describe } from "node:test";
import { fileURLToPath } from "node:url";

/**
 * Behavioural contract for scripts/benes-restart.sh.
 *
 * The helper runs end to end, but every external effect is replaced: `go`,
 * `setsid`, `nohup`, `curl` and `sleep` are shims, and the listener is
 * simulated by state files. No real listener starts and no real `go run`
 * happens. Readiness is controlled through the same files the script reads.
 *
 * The poll loop is paced by the shimmed `sleep`, so one plan step is applied
 * per readiness retry without depending on wall-clock timing. The stop phase's
 * settle delay is the one `sleep` call a plan step must ignore.
 */

const REPO = join(dirname(fileURLToPath(import.meta.url)), "..");
const RESTART = join(REPO, "scripts", "benes-restart.sh");

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

/** Windows PATH entries are `;`-separated; POSIX needs every entry converted. */
function toPosixPath(value: string): string {
  return value
    .split(";")
    .map((entry) => entry.trim())
    .filter(Boolean)
    .map(toPosix)
    .join(":");
}

const BASH = resolveBash();
const SKIP = BASH ? false : "needs a POSIX bash; on Windows install Git for Windows or set BENES_TEST_BASH";

const roots: string[] = [];
after(() => {
  for (const root of roots) rmSync(root, { recursive: true, force: true });
});

function scratch(): string {
  const root = mkdtempSync(join(tmpdir(), "benes-restart-"));
  roots.push(root);
  return root;
}

function shim(bin: string, name: string, body: string[]): void {
  const path = join(bin, name);
  writeFileSync(path, `#!/bin/sh\n${body.join("\n")}\n`, "utf8");
  chmodSync(path, 0o755);
}

/** A state change applied after the Nth failed readiness probe. */
type Step = { at: number; shell: string };

type Scenario = {
  /** Initial runtime-port.json payload; null leaves the file absent. */
  port?: string | null;
  /** Initial benes.pid content; null leaves the file absent. */
  pid?: string | null;
  /** Whether the listener answers the health probe from the first attempt. */
  healthy?: boolean;
  /** Simulated health probe latency; used to prove the readiness deadline is hard. */
  healthDelayMs?: number;
  steps?: Step[];
  stopExit?: number;
  timeout?: number;
  interval?: number;
  logLines?: number;
  /** Custom BENES_RESTART_LOG destination; defaults to a per-run scratch path. */
  logPath?: string;
};

type Run = { status: number | null; stdout: string; stderr: string; trace: string[]; retries: number; pid: string };

/** Shell for a plan step that (re)writes the runtime-port payload. */
function portStep(payload: string): string {
  return `printf '%s' '${payload}' > "$BENES_TEST_PORT_FILE"`;
}

const PORT = '{"pid":4242,"port":23100,"hostname":"127.0.0.1"}';

function restart(scenario: Scenario): Run {
  const root = scratch();
  const bin = join(root, "bin");
  const state = join(root, "home", ".benes");
  mkdirSync(bin, { recursive: true });
  mkdirSync(state, { recursive: true });

  const tracePath = join(root, "trace.log");
  const stepsPath = join(root, "steps");
  const portPath = join(state, "runtime-port.json");
  const pidPath = join(state, "benes.pid");
  const healthPath = join(root, "health-ok");
  const logPath = scenario.logPath ?? join(root, "restart.log");

  if (scenario.port !== undefined && scenario.port !== null) writeFileSync(portPath, scenario.port, "utf8");
  if (scenario.pid !== undefined && scenario.pid !== null) writeFileSync(pidPath, scenario.pid, "utf8");
  if (scenario.healthy) writeFileSync(healthPath, "1", "utf8");

  shim(bin, "go", [
    'if [ -n "${BENES_TEST_TRACE:-}" ]; then printf \'go %s\\n\' "$*" >> "$BENES_TEST_TRACE"; fi',
    'case "$3" in',
    `  stop) exit ${scenario.stopExit ?? 0} ;;`,
    "  start)",
    '    if [ -n "${BENES_TEST_PID_FILE:-}" ]; then printf \'%s\' "${BENES_TEST_PID:-0}" > "$BENES_TEST_PID_FILE"; fi',
    '    i=0; while [ "$i" -lt "${BENES_TEST_LOG_LINES:-1}" ]; do printf \'listener boot line %s\\n\' "$i"; i=$((i + 1)); done',
    "    exit 0 ;;",
    "  *) exit 0 ;;",
    "esac",
  ]);
  shim(bin, "setsid", [
    'if [ -n "${BENES_TEST_TRACE:-}" ]; then printf \'setsid %s\\n\' "$*" >> "$BENES_TEST_TRACE"; fi',
    'exec "$@"',
  ]);
  shim(bin, "nohup", [
    'if [ -n "${BENES_TEST_TRACE:-}" ]; then printf \'nohup %s\\n\' "$*" >> "$BENES_TEST_TRACE"; fi',
    'exec "$@"',
  ]);
  // The probe can deliberately block to prove the helper supervises the
  // operation instead of checking its deadline only between probes.
  shim(bin, "curl", [
    'if [ "${BENES_TEST_HEALTH_DELAY_MS:-0}" -gt 0 ]; then node -e \'setTimeout(() => {}, Number(process.argv[1]))\' "$BENES_TEST_HEALTH_DELAY_MS"; fi',
    `if [ -f "${toPosix(healthPath)}" ]; then exit 0; fi`,
    "exit 1",
  ]);
  shim(bin, "sleep", [
    'if [ "${1:-}" = "2" ]; then exit 0; fi',
    'n=0; if [ -f "${BENES_TEST_STEPS:-}" ]; then n=$(cat "$BENES_TEST_STEPS"); fi',
    'n=$((n + 1)); printf \'%s\' "$n" > "$BENES_TEST_STEPS"',
    'case "$n" in',
    ...(scenario.steps ?? []).map((step) => `  ${step.at}) ${step.shell} ;;`),
    "esac",
    "exit 0",
  ]);

  // MSYS prepends its own /usr/bin and /mingw64/bin to PATH, which would shadow
  // the curl and sleep shims. Prepend the shim directory inside the shell
  // instead of through the spawn environment so the shims really win.
  const launch = `export PATH="${toPosix(bin)}":$PATH; exec "${toPosix(RESTART)}"`;

  const result = spawnSync(BASH as string, ["-c", launch], {
    cwd: REPO,
    encoding: "utf8",
    timeout: 15_000,
    killSignal: "SIGKILL",
    env: {
      ...process.env,
      PATH: toPosixPath(process.env.PATH ?? ""),
      HOME: toPosix(join(root, "home")),
      BENES_RESTART_LOG: toPosix(logPath),
      // Bounded so a case that never becomes ready fails quickly instead of
      // spinning out the default 30s budget.
      BENES_RESTART_TIMEOUT: String(scenario.timeout ?? 3),
      BENES_RESTART_INTERVAL: String(scenario.interval ?? 0),
      BENES_TEST_TRACE: toPosix(tracePath),
      BENES_TEST_STEPS: toPosix(stepsPath),
      BENES_TEST_PORT_FILE: toPosix(portPath),
      BENES_TEST_HEALTH_OK: toPosix(healthPath),
      BENES_TEST_HEALTH_DELAY_MS: String(scenario.healthDelayMs ?? 0),
      BENES_TEST_PID_FILE: toPosix(pidPath),
      BENES_TEST_PID: "4242",
      BENES_TEST_LOG_LINES: String(scenario.logLines ?? 1),
    },
  });

  return {
    status: result.status,
    stdout: result.stdout ?? "",
    stderr: result.stderr ?? "",
    trace: existsSync(tracePath)
      ? readFileSync(tracePath, "utf8").split("\n").map((line) => line.trim()).filter(Boolean)
      : [],
    retries: existsSync(stepsPath) ? Number(readFileSync(stepsPath, "utf8")) : 0,
    pid: existsSync(pidPath) ? readFileSync(pidPath, "utf8") : "",
  };
}

describe("scripts/benes-restart.sh", { skip: SKIP }, () => {
  test("reports ready once the port is published and the listener answers", () => {
    const run = restart({ port: PORT, pid: "1111", healthy: true });
    assert.equal(run.status, 0, run.stderr);
    assert.match(run.stdout, /\[benes-restart\] ready port=23100 pid=4242/);
    assert.equal(run.pid, "4242");
  });

  test("tolerates a failing stop phase and still restarts", () => {
    const run = restart({ port: PORT, healthy: true, stopExit: 99 });
    assert.equal(run.status, 0, run.stderr);
    assert.match(run.stdout, /ready port=23100/);
  });

  test("starts the listener detached with output redirected to the restart log", () => {
    const run = restart({ port: PORT, healthy: true, logLines: 3 });
    assert.equal(run.status, 0, run.stderr);
    assert.equal(run.trace.some((line) => line === "setsid nohup go run ./cmd/benes start"), true);
    assert.equal(run.trace.some((line) => line === "nohup go run ./cmd/benes start"), true);
    assert.equal(run.trace.some((line) => line === "go run ./cmd/benes stop"), true);
    assert.match(run.stdout, /^\[benes-restart\] stop$/m);
  });

  test("honours a custom restart log destination", () => {
    const root = scratch();
    const logPath = join(root, "custom-restart.log");
    const run = restart({ port: PORT, healthy: true, logLines: 2, logPath });
    assert.equal(run.status, 0, run.stderr);
    assert.match(run.stdout, /detach start; log /);
    const log = readFileSync(logPath, "utf8");
    assert.match(log, /listener boot line 0/);
    assert.match(log, /listener boot line 1/);
  });

  test("keeps polling while the runtime-port file is still absent", () => {
    const run = restart({
      port: null,
      healthy: true,
      steps: [{ at: 2, shell: portStep(PORT) }],
    });
    assert.equal(run.status, 0, run.stderr);
    assert.match(run.stdout, /ready port=23100/);
    assert.equal(run.retries >= 2, true);
  });

  test("keeps polling while the port payload decodes to nothing", () => {
    const run = restart({
      port: '{"pid":4242}',
      healthy: true,
      steps: [{ at: 1, shell: portStep(PORT) }],
    });
    assert.equal(run.status, 0, run.stderr);
    assert.match(run.stdout, /ready port=23100/);
  });

  test("keeps polling while the port payload is invalid JSON", () => {
    const run = restart({
      port: '{"port":',
      healthy: true,
      steps: [{ at: 1, shell: portStep(PORT) }],
    });
    assert.equal(run.status, 0, run.stderr);
    assert.match(run.stdout, /ready port=23100/);
  });

  test("keeps polling while the listener exists but is still unhealthy", () => {
    const run = restart({
      port: PORT,
      healthy: false,
      steps: [{ at: 3, shell: "printf '1' > \"$BENES_TEST_HEALTH_OK\"" }],
    });
    assert.equal(run.status, 0, run.stderr);
    assert.equal(run.retries >= 3, true);
  });

  test("does not accept a decoded port as readiness while the listener never answers", () => {
    const run = restart({ port: PORT, healthy: false, timeout: 1 });
    assert.equal(run.status, 1);
    assert.match(run.stderr, /still down after 1s/);
    assert.doesNotMatch(run.stdout, /ready port/);
  });

  test("does not let a blocking health probe escape the readiness deadline", () => {
    const run = restart({ port: PORT, healthy: true, timeout: 1, healthDelayMs: 2_500 });
    assert.equal(run.status, 1);
    assert.match(run.stderr, /still down after 1s/);
    assert.doesNotMatch(run.stdout, /ready port/);
  });

  test("fails with a bounded tail of the restart log after the readiness budget", () => {
    const run = restart({ port: null, timeout: 1, logLines: 40 });
    assert.equal(run.status, 1);
    assert.match(run.stderr, /\[benes-restart\] still down after 1s/);
    assert.match(run.stderr, /listener boot line 39/);
    const tailed = run.stderr.split("\n").filter((line) => line.includes("listener boot line"));
    assert.equal(tailed.length, 15);
  });
});