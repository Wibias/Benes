import assert from "node:assert/strict";
import test from "node:test";
import { npmInvocation, runCommand } from "./node-runtime.ts";

test("npmInvocation runs the parent npm CLI through the current Node executable", () => {
  const invocation = npmInvocation(["run", "lint"], {
    env: { npm_execpath: "C:\\Program Files\\nodejs\\node_modules\\npm\\bin\\npm-cli.js" },
    execPath: "C:\\Program Files\\nodejs\\node.exe",
    platform: "win32",
  });

  assert.deepEqual(invocation, {
    command: "C:\\Program Files\\nodejs\\node.exe",
    args: [
      "C:\\Program Files\\nodejs\\node_modules\\npm\\bin\\npm-cli.js",
      "run",
      "lint",
    ],
  });
});

test("npmInvocation falls back to cmd.exe for direct Windows script execution", () => {
  const invocation = npmInvocation(["run", "lint"], {
    env: { ComSpec: "C:\\Windows\\System32\\cmd.exe" },
    execPath: "C:\\Program Files\\nodejs\\node.exe",
    platform: "win32",
  });

  assert.deepEqual(invocation, {
    command: "C:\\Windows\\System32\\cmd.exe",
    args: ["/d", "/s", "/c", "npm", "run", "lint"],
  });
});

test("npmInvocation keeps direct npm execution on non-Windows platforms", () => {
  const invocation = npmInvocation(["run", "build:gui"], {
    env: {},
    execPath: "/usr/bin/node",
    platform: "linux",
  });

  assert.deepEqual(invocation, {
    command: "npm",
    args: ["run", "build:gui"],
  });
});

test("runCommand passes an explicit env object to the child process", async () => {
  const env = { ...process.env, BENES_RUNTIME_ENV_PROBE: "present" };
  delete env.GH_TOKEN;
  const result = await runCommand(
    [process.execPath, "-e", "process.stdout.write(JSON.stringify({ probe: process.env.BENES_RUNTIME_ENV_PROBE, gh: process.env.GH_TOKEN ?? null }))"],
    { env },
  );
  assert.equal(result.exitCode, 0, result.stderr);
  assert.deepEqual(JSON.parse(result.stdout), { probe: "present", gh: null });
});
