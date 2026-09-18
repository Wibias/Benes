import assert from "node:assert/strict";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

import {
  diagnosticsFromOxlintJson,
  summarizeStructuralDiagnostics,
} from "./check-structural-baseline.ts";
import { isCodexRestartResponse } from "../src/lib/codex-restart-contract.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");
const oxlintBin = path.join(guiRoot, "node_modules", "oxlint", "bin", "oxlint");
const configPath = path.join(guiRoot, ".oxlintrc.json");
const targetFiles = [
  path.join(guiRoot, "src", "combo-workspace-data.ts"),
  path.join(guiRoot, "src", "lib", "codex-restart-contract.ts"),
];
const retiredFunctions = new Set([
  "parseComboList",
  "validateComboDraft",
  "isCodexRestartResponse",
]);

function structuralDiagnostics() {
  const result = spawnSync(
    process.execPath,
    [oxlintBin, "-c", configPath, "--format=json", ...targetFiles],
    { cwd: guiRoot, encoding: "utf8" },
  );
  if (result.error) throw result.error;
  const stdout = String(result.stdout ?? "").trim();
  assert.ok(stdout, `Oxlint target scan produced no output:\n${String(result.stderr ?? "")}`);
  const diagnostics = diagnosticsFromOxlintJson(JSON.parse(stdout), guiRoot);
  return summarizeStructuralDiagnostics(diagnostics);
}

function namedFunction(message) {
  const match = message.match(/function `([^`]+)` has a complexity/);
  return match?.[1] ?? null;
}

function restartResponse(overrides = {}) {
  return {
    success: true,
    synced: false,
    stateBefore: "fresh",
    requested: [101],
    stopped: [101],
    surviving: [],
    failed: [],
    code: "stopped",
    ...overrides,
  };
}

test("batch-one data boundaries stay within structural policy", () => {
  const remaining = structuralDiagnostics().filter((diagnostic) => {
    const name = namedFunction(diagnostic.message);
    return name !== null && retiredFunctions.has(name);
  });
  assert.deepEqual(remaining, []);
});

test("Codex restart contract accepts successful and partial-stop responses", () => {
  assert.equal(isCodexRestartResponse(restartResponse()), true);
  assert.equal(isCodexRestartResponse(restartResponse({
    success: false,
    requested: [101, 202],
    stopped: [101],
    surviving: [202],
    code: "partially_stopped",
  })), true);
});

test("Codex restart contract rejects malformed pid lists", () => {
  assert.equal(isCodexRestartResponse(restartResponse({ requested: [0] })), false);
  assert.equal(isCodexRestartResponse(restartResponse({ failed: [1.5] })), false);
});

test("Codex restart contract preserves success and outcome invariants", () => {
  assert.equal(isCodexRestartResponse(restartResponse({
    success: false,
    code: "partially_stopped",
    stopped: [],
    surviving: [],
    failed: [],
  })), false);
  assert.equal(isCodexRestartResponse(restartResponse({ surviving: [202] })), false);
  assert.equal(isCodexRestartResponse(restartResponse({
    code: "nothing_running",
    requested: [],
    stopped: [101],
  })), false);
});
