import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { usageSummary30dResourceKey } from "../src/usage-summary-resource.ts";
import {
  dashboardModelGroupExpanded,
  toggleExpandedProvider,
} from "../src/pages/dashboard-model-views.ts";
import {
  DASHBOARD_TAB_IDS,
  dashboardPanelId,
  dashboardSectionAfterTabKey,
  dashboardTabButtonId,
  dashboardWorkspaceChrome,
  nextDashboardTabIndex,
} from "../src/pages/dashboard-tab-nav.ts";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function readSrc(...parts: string[]) {
  return readFileSync(path.join(guiRoot, ...parts), "utf8");
}

test("Dashboard tab keyboard wraps and Home/End jump to the ends", () => {
  assert.deepEqual(DASHBOARD_TAB_IDS, ["overview", "providers", "models"]);
  assert.equal(nextDashboardTabIndex(0, "ArrowRight", 3), 1);
  assert.equal(nextDashboardTabIndex(2, "ArrowRight", 3), 0);
  assert.equal(nextDashboardTabIndex(0, "ArrowLeft", 3), 2);
  assert.equal(nextDashboardTabIndex(1, "ArrowLeft", 3), 0);
  assert.equal(nextDashboardTabIndex(1, "Home", 3), 0);
  assert.equal(nextDashboardTabIndex(1, "End", 3), 2);
  assert.equal(nextDashboardTabIndex(0, "Enter", 3), null);
  assert.equal(nextDashboardTabIndex(0, "ArrowRight", 0), null);
  assert.equal(dashboardSectionAfterTabKey("overview", "ArrowRight"), "providers");
  assert.equal(dashboardSectionAfterTabKey("models", "ArrowRight"), "overview");
  assert.equal(dashboardSectionAfterTabKey("providers", "Home"), "overview");
  assert.equal(dashboardSectionAfterTabKey("overview", "End"), "models");
  assert.equal(dashboardSectionAfterTabKey("overview", "Enter"), null);
  assert.equal(dashboardTabButtonId("providers"), "dashboard-tab-providers");
  assert.equal(dashboardPanelId("models"), "dashboard-panel-models");
  assert.equal(dashboardWorkspaceChrome("overview").showTablist, false);
  assert.equal(dashboardWorkspaceChrome("providers").showTablist, true);
  assert.equal(dashboardWorkspaceChrome("overview").shellClass.includes("dashboard-workspace-shell--plane"), true);
  assert.equal(dashboardWorkspaceChrome("models").shellClass.includes("dashboard-workspace-shell--plane"), false);
});

test("model groups expand for a non-empty query or an explicit set member", () => {
  assert.equal(dashboardModelGroupExpanded("", new Set(), "openai"), false);
  assert.equal(dashboardModelGroupExpanded("   ", new Set(), "openai"), false);
  assert.equal(dashboardModelGroupExpanded(" gpt ", new Set(), "openai"), true);
  assert.equal(dashboardModelGroupExpanded("", new Set(["openai"]), "openai"), true);
  assert.equal(dashboardModelGroupExpanded("", new Set(["openai"]), "anthropic"), false);
  const opened = toggleExpandedProvider(new Set(), "openai");
  assert.equal(opened.has("openai"), true);
  assert.equal(toggleExpandedProvider(opened, "openai").has("openai"), false);
});

test("omitted provider defaultModel is an em dash, never an invented id", () => {
  const label = (defaultModel: string | undefined) => defaultModel ?? "—";
  assert.equal(label(undefined), "—");
  assert.equal(label("gpt-5.4-mini"), "gpt-5.4-mini");
  assert.match(readSrc("src", "pages", "dashboard-providers-section.tsx"), /defaultModel \?\? "—"/);
});

test("usage-summary-30d resource keys stay shared across Dashboard and provider surfaces", () => {
  const apiBase = "http://127.0.0.1:23100";
  assert.equal(usageSummary30dResourceKey(apiBase), "usage-summary-30d:http://127.0.0.1:23100:all");
  assert.equal(usageSummary30dResourceKey(apiBase, "all"), "usage-summary-30d:http://127.0.0.1:23100:all");
  assert.equal(usageSummary30dResourceKey(apiBase, "codex"), "usage-summary-30d:http://127.0.0.1:23100:codex");

  const keyConsumers = [
    ["src", "pages", "use-dashboard-data.ts"],
    ["src", "pages", "use-provider-registry.ts"],
    ["src", "components", "AddProviderModal.tsx"],
    // The Codex pool's usage read moved into its domain module with the rest of the pool's
    // server reads; the hook itself no longer names the key.
    ["src", "codex-account-pool-domain.ts"],
    // The Providers board's usage read moved into the board's read hooks with its other
    // listener reads; the shell composes those reads rather than naming the key itself.
    ["src", "components", "provider-workspace", "use-provider-workspace-reads.ts"],
  ];
  assert.match(
    readSrc("src", "components", "provider-workspace", "ProviderWorkspaceShell.tsx"),
    /useProviderUsageRead/,
  );
  for (const parts of keyConsumers) {
    assert.match(readSrc(...parts), /usageSummary30dResourceKey\(/);
  }
  assert.match(readSrc("src", "pages", "use-dashboard-data.ts"), /fetchDashboardUsage/);
  assert.match(readSrc("src", "pages", "dashboard-core-poll.ts"), /\/api\/usage\?range=30d/);
});

test("Dashboard Overview stays DashboardPlaneBoard and the retired update dialog stays unmounted", () => {
  const dashboard = readSrc("src", "pages", "Dashboard.tsx");
  assert.match(dashboard, /from "\.\/dashboard-plane-board"/);
  assert.match(dashboard, /<DashboardPlaneBoard \{\.\.\.d\} \/>/);
  assert.match(dashboard, /DashboardProvidersSection/);
  assert.match(dashboard, /DashboardModelsSection/);
  assert.match(dashboard, /role="tablist"/);
  assert.match(dashboard, /dashboardWorkspaceChrome/);
  assert.equal(dashboard.includes("dashboard-overview-"), false);
  assert.equal(dashboard.includes("MemoryObservabilityCard"), false);
  assert.equal(dashboard.includes("dashboard-dialogs"), false);
  assert.equal(dashboard.includes("DashboardHelpDialog"), false);
  // Self-update is retired at the listener, so the dialog that drove it is gone with it.
  assert.equal(dashboard.includes("DashboardUpdateDialog"), false);
});

test("Phase 1 deleted Overview modules, dead dialog wrappers, and the retired update flow stay absent", () => {
  const gone = [
    ["src", "pages", "dashboard-overview-section.tsx"],
    ["src", "pages", "dashboard-overview-panels.tsx"],
    ["src", "pages", "dashboard-overview-head.tsx"],
    ["src", "pages", "dashboard-overview-sections.tsx"],
    ["src", "pages", "dashboard-overview-stats.tsx"],
    ["src", "pages", "dashboard-maintenance.tsx"],
    ["src", "pages", "dashboard-sidecar-cards.tsx"],
    ["src", "components", "MemoryObservabilityCard.tsx"],
    ["src", "components", "memory-observability", "memory-card-sections.tsx"],
    ["src", "components", "memory-observability", "memory-metrics.ts"],
    ["src", "pages", "dashboard-help-dialogs.tsx"],
    ["src", "pages", "dashboard-dialogs.tsx"],
    ["src", "pages", "dashboard-provider-display.ts"],
    ["src", "pages", "dashboard-update-dialog.tsx"],
    ["src", "pages", "dashboard-update-poll.ts"],
    ["src", "pages", "dashboard-update-poll-fetch.ts"],
    ["src", "pages", "use-dashboard-update.ts"],
  ];
  for (const parts of gone) {
    assert.equal(existsSync(path.join(guiRoot, ...parts)), false, parts.join("/"));
  }
});

test("use-dashboard-data drops Overview-only mutations after #260", () => {
  const hook = readSrc("src", "pages", "use-dashboard-data.ts");
  const dead = [
    "saveSidecar",
    "saveShadowCall",
    "switchMaMode",
    "toggleCodexAutoStart",
    "runSync",
    "clearSyncFeedback",
    "saveInjection",
    "maHelpOpen",
    "effortCapHelpOpen",
    "shadowCallHelpOpen",
    "sidecarSaving",
    "injectionModel",
    "effortCap",
    "healthLoading",
    "usageLoading",
    "sidecarModels",
    "visionModels",
    "fetchDashboardSidecars",
    "fetchDashboardMaMode",
    "fetchDashboardMultiAgent",
    // The retired self-update session and the settings payload's impossible startup seed.
    "useDashboardUpdateSession",
    "startupHealthSeed",
    "seedStartupHealthFromSettings",
  ];
  for (const token of dead) {
    assert.equal(hook.includes(token), false, token);
  }
  assert.match(hook, /usageSummary30dResourceKey\(/);
  assert.match(hook, /fetchDashboardOverview/);
});
