import assert from "node:assert/strict";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

import {
  diagnosticsFromOxlintJson,
  summarizeStructuralDiagnostics,
} from "./check-structural-baseline.ts";
import { parseProviderWorkspaceAggregate } from "../src/provider-workspace/workspace.ts";
import {
  accountQuotaFromReport,
  capacityAggregationFromReport,
} from "../src/provider-workspace/report.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");
const oxlintBin = path.join(guiRoot, "node_modules", "oxlint", "bin", "oxlint");
const configPath = path.join(guiRoot, ".oxlintrc.json");
const targetFiles = [
  path.join(guiRoot, "src", "provider-workspace", "workspace.ts"),
  path.join(guiRoot, "src", "provider-workspace", "report.ts"),
];

function structuralDiagnostics() {
  const result = spawnSync(
    process.execPath,
    [oxlintBin, "-c", configPath, "--format=json", ...targetFiles],
    { cwd: guiRoot, encoding: "utf8" },
  );
  if (result.error) throw result.error;
  const stdout = String(result.stdout ?? "").trim();
  assert.ok(stdout, `Oxlint provider-data scan produced no output:\n${String(result.stderr ?? "")}`);
  const diagnostics = diagnosticsFromOxlintJson(JSON.parse(stdout), guiRoot);
  return summarizeStructuralDiagnostics(diagnostics);
}

function workspaceAggregate(overrides = {}) {
  return {
    summary: {
      totalProviders: 2,
      healthy: 1,
      attention: 1,
      disabled: 0,
      exposedModels: 7,
    },
    providers: [
      {
        id: "openai",
        connections: ["oauth"],
        hidden: [],
        lifecycle: "healthy",
        modelCount: 5,
        access: { methods: [] },
        disabled: false,
        lastValidated: 123,
        downstream: { harnesses: 1, routes: 2, subagents: 3 },
      },
      {
        id: "anthropic",
        connections: ["oauth"],
        hidden: ["legacy"],
        lifecycle: "attention",
        modelCount: 2,
        access: { methods: [] },
        disabled: false,
        lastValidated: null,
        downstream: { harnesses: null, routes: 1, subagents: 0 },
      },
    ],
    attention: [
      { provider: "anthropic", code: "reauth", severity: "warning", detail: "expired", timestamp: 456 },
    ],
    availability: {
      modelsAvailable: 7,
      modelsUnavailable: 1,
      staleProviderCatalogues: null,
      lastModelSync: 789,
    },
    downstream: {
      harnessCount: null,
      routeCount: 3,
      subAgentModelCount: 3,
      affectedRouteCount: 1,
    },
    recentEvents: [
      { provider: "openai", type: "validated", severity: "info", timestamp: 999 },
    ],
    ...overrides,
  };
}

test("batch-two provider data files contain no structural debt", () => {
  assert.deepEqual(structuralDiagnostics(), []);
});

test("workspace aggregate parser preserves valid nested data", () => {
  const parsed = parseProviderWorkspaceAggregate(workspaceAggregate());
  assert.ok(parsed);
  assert.deepEqual(parsed.summary, {
    totalProviders: 2,
    healthy: 1,
    attention: 1,
    disabled: 0,
    exposedModels: 7,
  });
  assert.equal(parsed.providers[0]?.downstream.harnesses, 1);
  assert.equal(parsed.providers[1]?.lastValidated, null);
  assert.deepEqual(parsed.attention[0], {
    provider: "anthropic",
    code: "reauth",
    severity: "warning",
    detail: "expired",
    timestamp: 456,
  });
  assert.deepEqual(parsed.recentEvents[0], {
    provider: "openai",
    type: "validated",
    severity: "info",
    timestamp: 999,
  });
});

test("workspace access remains an opaque object boundary", () => {
  const opaqueAccess = workspaceAggregate();
  opaqueAccess.providers[0].access = { futureServerField: true };
  const parsed = parseProviderWorkspaceAggregate(opaqueAccess);
  assert.ok(parsed);
  assert.deepEqual(parsed.providers[0]?.access, { futureServerField: true });

  const arrayAccess = workspaceAggregate();
  arrayAccess.providers[0].access = [];
  assert.equal(parseProviderWorkspaceAggregate(arrayAccess), null);
});

test("workspace aggregate parser rejects inconsistent and malformed rows", () => {
  const badSummary = workspaceAggregate({
    summary: { totalProviders: 3, healthy: 1, attention: 1, disabled: 0, exposedModels: 7 },
  });
  assert.equal(parseProviderWorkspaceAggregate(badSummary), null);

  const badProvider = workspaceAggregate();
  badProvider.providers[0].downstream.routes = -1;
  assert.equal(parseProviderWorkspaceAggregate(badProvider), null);

  const missingEvents = workspaceAggregate();
  delete missingEvents.recentEvents;
  assert.equal(parseProviderWorkspaceAggregate(missingEvents), null);
});

test("quota report narrowing keeps valid windows and fallback timestamp", () => {
  const quota = accountQuotaFromReport({
    updatedAt: 777,
    quota: {
      fiveHourPercent: 42,
      weeklyPercent: 15,
      fiveHourResetAt: 1000,
      customWindows: [
        { label: "burst", percent: 9, resetAt: 2000 },
        { label: "invalid" },
      ],
    },
  });
  assert.deepEqual(quota, {
    fiveHourPercent: 42,
    fiveHourResetAt: 1000,
    weeklyPercent: 15,
    customWindows: [{ label: "burst", percent: 9, resetAt: 2000 }],
    updatedAt: 777,
  });
  assert.equal(accountQuotaFromReport({ quota: { weeklyResetAt: 123 } }), null);
  assert.equal(accountQuotaFromReport({ quota: [] }), null);
});

test("capacity aggregation narrows windows, presentation, and current account", () => {
  const parsed = capacityAggregationFromReport({
    aggregation: {
      kind: "capacity-weighted-v1",
      scope: "routable-known",
      incomplete: true,
      excludedAccounts: 2,
      unknownPlanAccounts: 1,
      partialWindowAccounts: 3,
      fiveHour: { usedPercent: 25, incomplete: true, nextRecoveryAt: 100 },
      customWindows: [
        { label: "burst", usedPercent: 50, excludedAccounts: 1 },
        { label: 4, usedPercent: 20 },
      ],
      currentAccount: {
        plan: "pro",
        quota: { monthlyPercent: 70, updatedAt: 444 },
      },
    },
  });
  assert.deepEqual(parsed, {
    presentation: "aggregate",
    incomplete: true,
    excludedAccounts: 2,
    unknownPlanAccounts: 1,
    partialWindowAccounts: 3,
    fiveHour: { usedPercent: 25, incomplete: true, nextRecoveryAt: 100 },
    customWindows: [{ label: "burst", usedPercent: 50, excludedAccounts: 1 }],
    currentAccount: {
      plan: "pro",
      quota: { monthlyPercent: 70, updatedAt: 444 },
    },
  });

  assert.equal(capacityAggregationFromReport({ aggregation: { kind: "other" } }), null);
});
