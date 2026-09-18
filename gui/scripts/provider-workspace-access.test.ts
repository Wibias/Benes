import assert from "node:assert/strict";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

import {
  diagnosticsFromOxlintJson,
  summarizeStructuralDiagnostics,
} from "./check-structural-baseline.ts";
import {
  accessEmptyHintKey,
  credentialSelectionDisplay,
  credentialSelectionFacts,
  oauthAccountInteraction,
  oauthSessionLoggedIn,
  overviewConnected,
  overviewDefaultAccessKind,
  providerEventSeverity,
  overviewLastValidatedAt,
  overviewListedModels,
  recentAccessEvents,
  recentOverviewEvents,
  accessCredentialLane,
  accessShowsCredentialLaneSwitch,
  accessShowsSelectionOrder,
} from "../src/provider-workspace/access-presentation.ts";
import {
  anthropicPoolSavedSnapshot,
  anthropicPoolSnapshotFromPayload,
  anthropicPoolToggleDisabled,
  anthropicPoolView,
  parseAnthropicThresholdDraft,
} from "../src/provider-workspace/anthropic-pool-state.ts";
import {
  COCKPIT_IMPORT_MAX_BYTES,
  cockpitImportFileAccepted,
  cockpitImportOutcomeFromResponse,
  runCockpitImport,
  safeCockpitImportResult,
} from "../src/provider-workspace/cockpit-import.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");
const oxlintBin = path.join(guiRoot, "node_modules", "oxlint", "bin", "oxlint");
const configPath = path.join(guiRoot, ".oxlintrc.json");
const targetFiles = [
  path.join(guiRoot, "src", "provider-workspace", "access-presentation.ts"),
  path.join(guiRoot, "src", "provider-workspace", "anthropic-pool-state.ts"),
  path.join(guiRoot, "src", "provider-workspace", "cockpit-import.ts"),
  path.join(guiRoot, "src", "components", "provider-workspace", "AnthropicAccountPoolSettings.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "ProviderAccess.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "ProviderAuthPanel.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "auth-panel-keys.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "auth-panel-oauth.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "ProviderOverview.tsx"),
];

function structuralDiagnostics() {
  const result = spawnSync(
    process.execPath,
    [oxlintBin, "-c", configPath, "--format=json", ...targetFiles],
    { cwd: guiRoot, encoding: "utf8" },
  );
  if (result.error) throw result.error;
  const stdout = String(result.stdout ?? "").trim();
  assert.ok(stdout, `Oxlint provider-workspace access scan produced no output:\n${String(result.stderr ?? "")}`);
  return summarizeStructuralDiagnostics(diagnosticsFromOxlintJson(JSON.parse(stdout), guiRoot));
}

test("batch-three access/auth/quota files contain no structural debt", () => {
  assert.deepEqual(structuralDiagnostics(), []);
});

function cockpitResult(overrides = {}) {
  return {
    totalCount: 1,
    importedCount: 1,
    updatedCount: 0,
    failedCount: 0,
    unsupportedCount: 0,
    results: [{ index: 0, status: "imported", code: "imported" }],
    ...overrides,
  };
}

test("cockpit import decode keeps valid counts and rejects extra or mismatched keys", () => {
  assert.deepEqual(safeCockpitImportResult(cockpitResult()), {
    importedCount: 1,
    updatedCount: 0,
    failedCount: 0,
    unsupportedCount: 0,
  });
  assert.equal(safeCockpitImportResult(cockpitResult({ extra: true })), null);
  assert.equal(safeCockpitImportResult(Object.create({ totalCount: 1 })), null);
  assert.equal(safeCockpitImportResult(cockpitResult({
    results: [{ index: 0, status: "imported", code: "imported", detail: "x" }],
  })), null);
  assert.equal(safeCockpitImportResult(cockpitResult({
    importedCount: 0,
    failedCount: 1,
    results: [{ index: 0, status: "failed", code: "invalid_record" }],
  }))?.failedCount, 1);
  assert.equal(safeCockpitImportResult(cockpitResult({
    importedCount: 0,
    failedCount: 1,
    results: [{ index: 0, status: "failed", code: "imported" }],
  })), null);
  assert.equal(safeCockpitImportResult(cockpitResult({
    totalCount: 2,
    importedCount: 1,
    results: [{ index: 0, status: "imported", code: "imported" }],
  })), null);
});

test("cockpit import file and response policy preserve invalid vs failed precedence", () => {
  assert.equal(cockpitImportFileAccepted({ name: "accounts.JSON", size: COCKPIT_IMPORT_MAX_BYTES }), true);
  assert.equal(cockpitImportFileAccepted({ name: "accounts.json", size: COCKPIT_IMPORT_MAX_BYTES + 1 }), false);
  assert.equal(cockpitImportFileAccepted({ name: "accounts.txt", size: 12 }), false);
  assert.deepEqual(
    cockpitImportOutcomeFromResponse(false, cockpitResult()),
    { status: "failed" },
  );
  assert.deepEqual(
    cockpitImportOutcomeFromResponse(true, cockpitResult({ extra: true })),
    { status: "failed" },
  );
  assert.deepEqual(
    cockpitImportOutcomeFromResponse(true, cockpitResult()),
    {
      status: "complete",
      result: { importedCount: 1, updatedCount: 0, failedCount: 0, unsupportedCount: 0 },
    },
  );
});

test("cockpit import orchestration preserves invalid, failed, complete, and refresh isolation", async () => {
  const events = [];
  const session = {
    busy: false,
    begin() { events.push("begin"); },
    finish() { events.push("finish"); },
    setInvalid() { events.push("invalid"); },
    setFailed() { events.push("failed"); },
    setComplete(result) { events.push(["complete", result]); },
    async postDocument() { return { ok: true, payload: cockpitResult() }; },
    async refreshAccounts() { events.push("refresh"); },
  };

  await runCockpitImport(undefined, session);
  assert.deepEqual(events, []);

  await runCockpitImport({ name: "x.txt", size: 4, text: async () => "{}" }, session);
  assert.deepEqual(events, ["begin", "invalid", "finish"]);

  events.length = 0;
  await runCockpitImport({
    name: "x.json",
    size: 4,
    text: async () => { throw new Error("read"); },
  }, session);
  assert.deepEqual(events, ["begin", "invalid", "finish"]);

  events.length = 0;
  await runCockpitImport({
    name: "x.json",
    size: 4,
    text: async () => "{",
  }, session);
  assert.deepEqual(events, ["begin", "invalid", "finish"]);

  events.length = 0;
  session.postDocument = async () => ({ ok: true, payload: cockpitResult() });
  session.refreshAccounts = async () => { throw new Error("refresh"); };
  await runCockpitImport({ name: "x.json", size: 4, text: async () => "{}" }, session);
  assert.deepEqual(events, ["begin", ["complete", {
    importedCount: 1,
    updatedCount: 0,
    failedCount: 0,
    unsupportedCount: 0,
  }], "finish"]);
});

test("anthropic pool snapshot parse preserves load fallbacks and save identity", () => {
  assert.equal(anthropicPoolSnapshotFromPayload(null), null);
  assert.deepEqual(anthropicPoolSnapshotFromPayload({}), {
    enabled: false,
    threshold: 80,
    strategy: "quota",
    stickyLimit: 1,
  });
  assert.deepEqual(anthropicPoolSnapshotFromPayload({
    enabled: true,
    autoSwitchThreshold: Number.NaN,
    strategy: "round-robin",
    stickyLimit: 7,
  }), {
    enabled: true,
    threshold: Number.NaN,
    strategy: "round-robin",
    stickyLimit: 7,
  });
  assert.deepEqual(
    anthropicPoolSavedSnapshot(
      { enabled: true, threshold: 40, strategy: "quota", stickyLimit: 2 },
      { strategy: "fill-first", stickyLimit: 9 },
    ),
    { enabled: true, threshold: 40, strategy: "fill-first", stickyLimit: 9 },
  );
});

test("anthropic pool toggle and threshold drafts preserve enable gating", () => {
  assert.equal(anthropicPoolToggleDisabled({
    loading: false, saving: false, loadError: false, enabled: false, accountCount: 1,
  }), true);
  assert.equal(anthropicPoolToggleDisabled({
    loading: false, saving: false, loadError: false, enabled: true, accountCount: 1,
  }), false);
  assert.deepEqual(parseAnthropicThresholdDraft("80", 80), { kind: "unchanged", value: 80 });
  assert.deepEqual(parseAnthropicThresholdDraft("0", 80), { kind: "next", value: 0 });
  assert.deepEqual(parseAnthropicThresholdDraft("101", 80), { kind: "invalid" });
  assert.deepEqual(anthropicPoolView(null), {
    enabled: false,
    threshold: 80,
    strategy: "quota",
    stickyLimit: 1,
  });
});

test("access presentation preserves empty hints, overview facts, and oauth row gates", () => {
  assert.equal(accessEmptyHintKey("forward", true), "prov.access.forwardHint");
  assert.equal(accessEmptyHintKey("local", false), "prov.access.localHint");
  assert.equal(accessEmptyHintKey("key", true), "prov.access.localHint");
  assert.equal(accessEmptyHintKey("key", false), "prov.access.noneHint");
  assert.equal(overviewDefaultAccessKind({
    defaultMethodId: "api",
    defaultAccess: true,
    connected: false,
  }), "openai-api");
  assert.equal(overviewDefaultAccessKind({
    defaultAccess: true,
    connected: false,
  }), "chatgpt-pool");
  assert.equal(overviewDefaultAccessKind({ connected: overviewConnected({ oauthEmail: "a" }) }), "valid");
  assert.equal(overviewDefaultAccessKind({ connected: false }), "none");
  assert.deepEqual(overviewListedModels(["a", "missing"], ["a"]), {
    listed: ["a", "missing"],
    unavailableCount: 1,
  });
  assert.deepEqual(overviewListedModels([], ["a"]), { listed: ["a"], unavailableCount: 0 });
  assert.equal(overviewLastValidatedAt(undefined, 9), 9);
  assert.equal(overviewLastValidatedAt(null, 9), null);
  assert.equal(providerEventSeverity("warn").tone, "warn");
  assert.deepEqual(recentOverviewEvents([1, 2, 3, 4, 5]), [5, 4, 3, 2]);
  assert.equal(recentAccessEvents(Array.from({ length: 10 }, (_, i) => i)).length, 4);
  assert.equal(accessShowsCredentialLaneSwitch(true, true), true);
  assert.equal(accessShowsCredentialLaneSwitch(true, false), false);
  assert.equal(accessCredentialLane(true, true, "api-key"), "api-key");
  assert.equal(accessCredentialLane(true, false, "api-key"), "oauth");
  assert.equal(accessCredentialLane(false, true, "oauth"), "api-key");
  assert.equal(accessCredentialLane(false, false, "oauth"), null);
  assert.deepEqual(credentialSelectionFacts({
    mode: "direct",
    showAutoSwitch: true,
    showStickyLimit: true,
    autoSwitchThreshold: 40,
    stickyLimit: 2,
  }), {
    poolMode: false,
    showStrategy: false,
    showAutoSwitch: false,
    showStickyLimit: false,
    stickyDisplay: 2,
    thresholdLabel: "40%",
  });
  const poolFacts = credentialSelectionFacts({
    mode: "pool",
    showAutoSwitch: true,
    showStickyLimit: true,
    autoSwitchThreshold: 40,
    stickyLimit: 2,
  });
  assert.deepEqual(credentialSelectionDisplay(poolFacts, "quota", false), {
    mode: "—",
    strategy: "—",
    autoSwitch: "—",
    sticky: "—",
  });
  assert.deepEqual(credentialSelectionDisplay(poolFacts, "quota", true), {
    mode: "pool",
    strategy: "quota",
    autoSwitch: "40%",
    sticky: 2,
  });
  assert.equal(accessShowsSelectionOrder(0), false);
  assert.equal(accessShowsSelectionOrder(1), false);
  assert.equal(accessShowsSelectionOrder(2), true);
  assert.equal(oauthSessionLoggedIn(0, { loggedIn: true }), true);
  const interaction = oauthAccountInteraction({
    id: "a",
    active: false,
    needsReauth: true,
    health: { status: "healthy" },
  }, null);
  assert.equal(interaction.showReauth, true);
  assert.equal(interaction.canActivate, false);
});
