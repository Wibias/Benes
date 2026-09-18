import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

import {
  diagnosticsFromOxlintJson,
  summarizeStructuralDiagnostics,
} from "./check-structural-baseline.ts";
import { mainInnerClass, nextTheme } from "../src/app-shell.ts";
import { controlProtectionLabel, controlServiceLabel, nextTrayAction } from "../src/pages/control-board-state.ts";
import {
  resolvedStarState,
  starClickMode,
  starOverrideFromResponse,
  updateAvailableFromBadge,
} from "../src/components/sidebar-github-star.ts";
import {
  filterDashboardModelGroups,
  groupDashboardModels,
  visionModelChangePatch,
  visionActivationFields,
  visionModelOptions,
  visionSelectOptions,
  VISION_DESCRIBE_BACKEND,
} from "../src/pages/dashboard-model-views.ts";
import { mergeSidecarSetting } from "../src/pages/dashboard-sidecar-merge.ts";
import { parseStartupHealthData } from "../src/pages/startup-shared.ts";
import {
  deriveCodexRuntimeNotice,
  installResultMessageKey,
  retireFailedInstallResult,
  serviceStateKey,
  shimNoKey,
  shimYesKey,
  startupHeroDetailKey,
  startupHeroTone,
  startupRoutingKey,
  startupTrayActions,
  startupTrayBadgeKind,
} from "../src/pages/startup-page-state.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");
const oxlintBin = path.join(guiRoot, "node_modules", "oxlint", "bin", "oxlint");
const configPath = path.join(guiRoot, ".oxlintrc.json");
const targetFiles = [
  path.join(guiRoot, "src", "App.tsx"),
  path.join(guiRoot, "src", "app-page.tsx"),
  path.join(guiRoot, "src", "app-routing.ts"),
  path.join(guiRoot, "src", "app-shell.ts"),
  path.join(guiRoot, "src", "app-sidebar.tsx"),
  path.join(guiRoot, "src", "use-app-chrome.ts"),
  path.join(guiRoot, "src", "components", "sidebar-github-row.tsx"),
  path.join(guiRoot, "src", "components", "sidebar-github-star.ts"),
  path.join(guiRoot, "src", "pages", "Startup.tsx"),
  path.join(guiRoot, "src", "pages", "Routing.tsx"),
  path.join(guiRoot, "src", "pages", "routing-tab.ts"),
  path.join(guiRoot, "src", "pages", "routing-tab-strip.tsx"),
  path.join(guiRoot, "src", "pages", "routing-page-shell.tsx"),
  path.join(guiRoot, "src", "pages", "control-page-shell.tsx"),
  path.join(guiRoot, "src", "pages", "control-board.tsx"),
  path.join(guiRoot, "src", "pages", "control-board-state.ts"),
  path.join(guiRoot, "src", "pages", "control-board-settings.ts"),
  path.join(guiRoot, "src", "pages", "dashboard-tab-nav.ts"),
  path.join(guiRoot, "src", "pages", "dashboard-plane-board.tsx"),
  path.join(guiRoot, "src", "pages", "dashboard-plane-sections.tsx"),
  path.join(guiRoot, "src", "pages", "dashboard-plane-data.ts"),
  path.join(guiRoot, "src", "pages", "dashboard-shared.ts"),
  path.join(guiRoot, "src", "pages", "dashboard-sidecar-merge.ts"),
  path.join(guiRoot, "src", "pages", "dashboard-model-views.ts"),
  path.join(guiRoot, "src", "pages", "startup-page-state.ts"),
  path.join(guiRoot, "src", "pages", "startup-shared.ts"),
  path.join(guiRoot, "src", "pages", "use-dashboard-data.ts"),
];

function structuralDiagnostics() {
  const missing = targetFiles.filter((file) => !existsSync(file));
  assert.deepEqual(missing, [], `cluster files missing:\n${missing.join("\n")}`);
  const result = spawnSync(
    process.execPath,
    [oxlintBin, "-c", configPath, "--format=json", ...targetFiles],
    { cwd: guiRoot, encoding: "utf8" },
  );
  if (result.error) throw result.error;
  const stdout = String(result.stdout ?? "").trim();
  assert.ok(stdout, `Oxlint dashboard-startup-shell scan produced no output:\n${String(result.stderr ?? "")}`);
  return summarizeStructuralDiagnostics(diagnosticsFromOxlintJson(JSON.parse(stdout), guiRoot));
}

function setting(overrides = {}) {
  return { model: "gpt-5.4-mini", backend: "openai", enabled: true, ...overrides };
}

function model(id, provider) {
  return { id, provider, namespaced: `${provider}/${id}` };
}

function health(overrides = {}) {
  return {
    status: "protected",
    routingKind: "benes-local",
    routingInjected: true,
    localRoutingDependency: true,
    autostartEnabled: true,
    rebootSafe: true,
    protection: "service",
    serviceInstalled: true,
    serviceViable: true,
    serviceEnabled: true,
    serviceRunning: true,
    serviceStale: false,
    serviceConflict: false,
    serviceSupported: true,
    shimInstalled: false,
    shimHealthy: false,
    shimCoverage: "none",
    platform: "linux",
    recommendedCommand: null,
    diagnosticStale: false,
    commands: {
      installService: "benes service install",
      repairService: "benes service repair",
      installShim: "benes shim install",
      restoreNative: "benes restore",
    },
    ...overrides,
  };
}

function t(key, vars) {
  if (!vars) return key;
  return `${key}:${Object.entries(vars).map(([name, value]) => `${name}=${value}`).join(",")}`;
}

test("dashboard startup shell files contain no structural debt", () => {
  assert.deepEqual(structuralDiagnostics(), []);
});

test("mergeSidecarSetting copies the writable scalars and does not mutate the current row", () => {
  const current = setting();
  const merged = mergeSidecarSetting(current, { model: "gpt-5.6", enabled: false });
  assert.equal(current.model, "gpt-5.4-mini");
  assert.equal(current.enabled, true);
  assert.deepEqual(merged, {
    model: "gpt-5.6",
    backend: "openai",
    enabled: false,
  });
});

test("mergeSidecarSetting treats null backend as a delete and leaves stored-only fields alone", () => {
  const current = setting({ backend: "vision_describe", timeoutMs: 8000 });
  assert.equal(mergeSidecarSetting(current).backend, "vision_describe");
  const cleared = mergeSidecarSetting(current, { backend: null });
  assert.equal("backend" in cleared, false);
  // `timeoutMs` is stored configuration the PUT does not accept, so an overlay cannot claim it moved.
  assert.equal(cleared.timeoutMs, 8000);
  const kept = mergeSidecarSetting(current, { model: "claude" });
  assert.equal(kept.backend, "vision_describe");
  assert.equal(kept.model, "claude");
});

test("dashboard model grouping and filtering stay stable", () => {
  const models = [
    model("b-2", "openai"),
    model("a-1", "anthropic"),
    model("b-1", "openai"),
  ];
  const grouped = groupDashboardModels(models);
  assert.deepEqual(grouped.map(([provider, rows]) => [provider, rows.map(row => row.id)]), [
    ["anthropic", ["a-1"]],
    ["openai", ["b-2", "b-1"]],
  ]);
  assert.deepEqual(
    filterDashboardModelGroups(grouped, "  B-1  ").map(([provider, rows]) => [provider, rows.map(row => row.id)]),
    [["openai", ["b-1"]]],
  );
  assert.equal(filterDashboardModelGroups(grouped, "anthropic").length, 1);
  assert.equal(filterDashboardModelGroups(grouped, "").length, 2);
});

test("vision picker maps the server's id list and keeps a stored describer visible", () => {
  const models = [model("text-only", "openai")];
  // No `visionModels` key: the response predates the field, so the provider-name list is the degrade path.
  const legacy = visionSelectOptions(models, {
    webSearch: { model: "gpt-5.6-luna" },
    vision: { model: "kept-anthropic", backend: "anthropic" },
  });
  assert.deepEqual(legacy, ["kept-anthropic", "openai/text-only"]);

  // `[]` is the server saying nothing is eligible: the catalog must not refill the picker.
  assert.deepEqual(visionModelOptions([], models, "kept-anthropic"), ["kept-anthropic"]);
  assert.deepEqual(visionModelOptions([], models, undefined), []);

  // The wire carries ids, and a current server's list is used verbatim.
  assert.deepEqual(
    visionModelOptions(["anthropic/claude-sonnet", "openai/gpt-5.6"], models, "anthropic/claude-sonnet"),
    ["anthropic/claude-sonnet", "openai/gpt-5.6"],
  );
});

test("vision model change turns the sidecar off, or on with the describe class", () => {
  assert.deepEqual(visionActivationFields("", true), { enabled: false });
  assert.deepEqual(visionActivationFields("gpt-5.4-mini", false), { enabled: true });
  assert.deepEqual(visionActivationFields("gpt-5.4-mini", true), {});
  // The provider travels in the model id, so naming the modality's class is the honest backend.
  assert.deepEqual(visionModelChangePatch("anthropic/claude-sonnet", true), {
    vision: { model: "anthropic/claude-sonnet", backend: VISION_DESCRIBE_BACKEND },
  });
  assert.deepEqual(visionModelChangePatch("anthropic/claude-sonnet", false), {
    vision: { model: "anthropic/claude-sonnet", backend: VISION_DESCRIBE_BACKEND, enabled: true },
  });
  assert.deepEqual(visionModelChangePatch("", true), { vision: { enabled: false } });
});

test("startup tray actions and badge follow installed/running/stale, not loading placeholders", () => {
  assert.deepEqual(startupTrayActions(null, true, false), {
    install: false, start: false, stop: false, uninstall: false,
  });
  assert.equal(startupTrayBadgeKind(null, true, false), "loading");
  assert.equal(startupTrayBadgeKind(null, false, true), "unavailable");

  const missing = { supported: true, installed: false, running: false, stale: false, summary: "none" };
  assert.deepEqual(startupTrayActions(missing, false, false), {
    install: true, start: false, stop: false, uninstall: false,
  });
  assert.equal(startupTrayBadgeKind(missing, false, false), "notInstalled");

  const stopped = { ...missing, installed: true };
  assert.deepEqual(startupTrayActions(stopped, false, false), {
    install: false, start: true, stop: false, uninstall: true,
  });
  const running = { ...stopped, running: true };
  assert.deepEqual(startupTrayActions(running, false, false), {
    install: false, start: false, stop: true, uninstall: true,
  });
  assert.equal(startupTrayBadgeKind(running, false, false), "running");
  const stale = { ...running, stale: true };
  assert.deepEqual(startupTrayActions(stale, false, false), {
    install: false, start: false, stop: false, uninstall: true,
  });
  assert.equal(startupTrayBadgeKind(stale, false, false), "stale");
});

test("startup runtime notice, install retirement, and hero/detail keys preserve current copy", () => {
  assert.deepEqual(deriveCodexRuntimeNotice(undefined, t), { warning: null, fix: null });
  const clamp = deriveCodexRuntimeNotice({
    version: "0.1.0",
    catalogClamp: { active: true, removedEfforts: ["xhigh"], runtimeVersion: "0.1.0" },
  }, t, "linux");
  assert.match(clamp.warning ?? "", /clampHiddenWithEfforts/);
  assert.equal(clamp.fix, "benes sync");
  const clampNewer = deriveCodexRuntimeNotice({
    version: "0.1.0",
    newerAvailable: { path: "/opt/codex" },
    catalogClamp: { active: true, removedEfforts: ["xhigh"], runtimeVersion: "0.1.0" },
  }, t, "linux");
  assert.equal(clampNewer.fix, "benes doctor --fix-codex-runtime && benes sync");
  const winNewer = deriveCodexRuntimeNotice({
    version: "0.1.0",
    newerAvailable: { path: "C:\\codex" },
  }, t, "win32");
  assert.match(winNewer.warning ?? "", /olderBinary/);
  assert.equal(winNewer.fix, "benes doctor --fix-codex-runtime; benes sync");

  const failedShim = { kind: "error", action: "install-shim", forLocalRouting: true, detail: "nope" };
  assert.equal(retireFailedInstallResult(failedShim, health({ status: "native" })), null);
  assert.equal(retireFailedInstallResult(failedShim, health({ status: "at-risk", shimInstalled: true, shimHealthy: true })), null);
  assert.equal(retireFailedInstallResult(failedShim, health({ status: "at-risk" }))?.detail, "nope");
  assert.equal(
    retireFailedInstallResult({ kind: "error", action: "install-service", forLocalRouting: false }, health({ serviceViable: true })),
    null,
  );

  assert.equal(startupHeroTone(true, "protected"), "risk");
  assert.equal(startupHeroTone(false, "protected"), "safe");
  assert.equal(startupRoutingKey("custom-remote"), "startup.routing.customRemote");
  assert.equal(startupHeroDetailKey(true, health()), "startup.staleData");
  assert.equal(serviceStateKey(health({ serviceViable: false, serviceConflict: true })), "startup.conflict");
  assert.equal(shimYesKey(health({ shimCoverage: "cli-only" })), "startup.cliOnly");
  assert.equal(shimNoKey(health({ shimInstalled: true, shimHealthy: true, autostartEnabled: false })), "startup.installedDisabled");
  assert.equal(installResultMessageKey({ kind: "success", action: "install-service", repair: true }), "startup.serviceRepaired");
});

test("sidebar star override and app chrome class names stay derived, not effect-cleared", () => {
  assert.equal(resolvedStarState("not-starred", { state: "starred", basedOn: "not-starred" }), "starred");
  assert.equal(resolvedStarState("starred", { state: "starred", basedOn: "not-starred" }), "starred");
  assert.equal(starClickMode("starred", false), "ignore");
  assert.equal(starClickMode("unauthenticated", false), "open-repo");
  assert.equal(starClickMode("not-starred", false), "post");
  assert.deepEqual(starOverrideFromResponse({ ok: true }, "not-starred"), {
    override: { state: "starred", basedOn: "not-starred" },
    openRepo: false,
  });
  assert.equal(starOverrideFromResponse({ state: "unauthenticated" }, "not-starred").openRepo, true);
  assert.equal(updateAvailableFromBadge({ updateAvailable: true, latestVersion: "2.0.0" }), true);
  assert.equal(updateAvailableFromBadge({ updateAvailable: false }), false);
  assert.equal(nextTheme("light"), "dark");
  assert.equal(nextTheme("dark"), "system");
  assert.equal(nextTheme("system"), "light");
  assert.equal(mainInnerClass("models"), "main-inner main-inner--models");
  assert.equal(mainInnerClass("routing"), "main-inner main-inner--routing");
  assert.equal(mainInnerClass("startup"), "main-inner main-inner--control");
  assert.equal(mainInnerClass("harnesses"), "main-inner main-inner--harnesses");
  assert.equal(mainInnerClass("api"), "main-inner main-inner--api");
  assert.equal(mainInnerClass("sessions"), "main-inner main-inner--sessions");
  assert.equal(mainInnerClass("dashboard"), "main-inner");
  const routing = readFileSync(path.join(guiRoot, "src", "app-routing.ts"), "utf8");
  assert.match(routing, /routing\/evaluation/);
  assert.match(routing, /routing\/analytics/);
  const tabs = readFileSync(path.join(guiRoot, "src", "pages", "routing-tab.ts"), "utf8");
  assert.match(tabs, /"evaluation"/);
  assert.match(tabs, /"analytics"/);
  const sidebar = readFileSync(path.join(guiRoot, "src", "app-sidebar.tsx"), "utf8");
  const systemAt = sidebar.indexOf("nav.group.system");
  const controlAt = sidebar.indexOf('tkey: "nav.control"');
  const routingAt = sidebar.indexOf('tkey: "nav.routing"');
  assert.ok(controlAt > systemAt, "Control nav item belongs under SYSTEM");
  assert.ok(routingAt > 0 && routingAt < systemAt, "Routing stays in CONFIGURATION");
  const apiAt = sidebar.indexOf('tkey: "nav.api"');
  const storageAt = sidebar.indexOf('tkey: "nav.storage"');
  assert.ok(apiAt > systemAt && apiAt < storageAt, "API nav item belongs under SYSTEM between Control and Storage");
  assert.equal(sidebar.includes('id: "integrations"'), false, "Integrations is not a sidebar destination");
  assert.match(routing, /api\/clients/);
  assert.match(routing, /integrations\/keys/);
  assert.match(routing, /replaceTo: "api"/);
  const clients = readFileSync(path.join(guiRoot, "src", "api-access", "export-clients.ts"), "utf8");
  assert.match(clients, /"opencode", labelKey: "api\.clientConfig\.clientOpencode"/);
  assert.equal(controlProtectionLabel(health()), "control.protected");
  assert.equal(controlServiceLabel(health()), "control.running");
  assert.equal(nextTrayAction(null), "install");
  assert.equal(nextTrayAction({ supported: true, installed: true, running: true, stale: false, summary: "ok" }), "stop");
  const stub = parseStartupHealthData({ ok: true, proxy: "running", shim: { installed: true, summary: "ok" } });
  assert.equal(stub?.routingKind, "benes-local");
  assert.equal(stub?.commands.restoreNative, "benes restore");
  assert.equal(parseStartupHealthData({ nope: true }), null);
});
