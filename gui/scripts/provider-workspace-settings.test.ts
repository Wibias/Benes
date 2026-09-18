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
  configDiscoveryPatch,
  configGeneralPatch,
  configOverrideRule,
  configPacingMissingRule,
  configPacingPayload,
  pacingFromItem,
} from "../src/provider-workspace/config-pacing.ts";
import {
  connectionTestOutcome,
  detailStatusPillKind,
  nextDetailTabIndex,
  overviewReauthAccountId,
} from "../src/provider-workspace/connection-test.ts";
import {
  CUSTOM_MODEL_CHIP_RENDER_CAP,
  customModelInvalid,
  knownCustomModelIds,
  modelsEmptyBase,
  parseCustomModelIds,
  showingConfiguredModelsFallback,
  visibleModelChips,
} from "../src/provider-workspace/custom-models.ts";
import {
  numberDraft,
  pacingSignature,
  positiveInteger,
  positiveRpm,
  settingsFormDirty,
  settingsOverrideRule,
  settingsPacingDraft,
  settingsPlainBaseUrlLocked,
  settingsSavePatch,
  settingsSupportsApiKeyTransport,
  settingsChoicesStatus,
  settingsHasEndpointPicker,
  settingsInitialAuthMode,
  settingsOpenAiView,
} from "../src/provider-workspace/settings-form.ts";
import {
  configurationDraftFields,
  configurationModelOptions,
  configurationOverrideDraft,
  configurationPacingContext,
} from "../src/provider-workspace/config-draft.ts";
import {
  accountsFocusAdjustment,
  clampedDetailTab,
  detailAccessCredentialPresent,
  pendingLeaveBack,
  pendingLeaveTab,
  scopedAccountsFocusToken,
  closeDetailsOverflow,
  closeOverflowDetails,
} from "../src/provider-workspace/detail-tabs.ts";
import {
  duplicateProviderDisplayNames,
  eventsForProvider,
  filterWorkspaceSections,
  freshQuotaReportsFromResponse,
  itemMatchesWorkspaceFilters,
  mergeProviderValues,
  nextRailTabbableName,
  parseQuotaReport,
  providerHasLiveModels,
  quotaReportsFromCache,
  quotaReportRows,
  sectionsFromWorkspace,
  selectedItemApiLane,
  usageModelsFromResponse,
  usageTotalsFromResponse,
  workspaceFilterActive,
  workspaceKpis,
  workspaceMainKind,
  workspaceRailGroups,
} from "../src/provider-workspace/workspace-shell.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");
const oxlintBin = path.join(guiRoot, "node_modules", "oxlint", "bin", "oxlint");
const configPath = path.join(guiRoot, ".oxlintrc.json");
const targetFiles = [
  path.join(guiRoot, "src", "provider-workspace", "settings-form.ts"),
  path.join(guiRoot, "src", "provider-workspace", "config-pacing.ts"),
  path.join(guiRoot, "src", "provider-workspace", "config-draft.ts"),
  path.join(guiRoot, "src", "provider-workspace", "custom-models.ts"),
  path.join(guiRoot, "src", "provider-workspace", "connection-test.ts"),
  path.join(guiRoot, "src", "provider-workspace", "detail-tabs.ts"),
  path.join(guiRoot, "src", "provider-workspace", "workspace-shell.ts"),
  path.join(guiRoot, "src", "components", "provider-workspace", "ProviderConfiguration.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "ProviderDetails.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "ProviderWorkspaceShell.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "config-sections.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "details-header.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "details-leave-dialogs.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "details-overflow-menu.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "details-tab-list.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "details-tab-panels.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "shell-rail.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "use-configuration-draft.ts"),
  path.join(guiRoot, "src", "components", "provider-workspace", "use-detail-tabs.ts"),
  path.join(guiRoot, "src", "components", "provider-workspace", "workspace-board-frames.tsx"),
  path.join(guiRoot, "src", "components", "provider-workspace", "workspace-detail-slot.ts"),
  path.join(guiRoot, "src", "components", "provider-workspace", "workspace-main-pane.tsx"),
];

function structuralDiagnostics() {
  const result = spawnSync(
    process.execPath,
    [oxlintBin, "-c", configPath, "--format=json", ...targetFiles],
    { cwd: guiRoot, encoding: "utf8" },
  );
  if (result.error) throw result.error;
  const stdout = String(result.stdout ?? "").trim();
  assert.ok(stdout, `Oxlint provider-workspace settings scan produced no output:\n${String(result.stderr ?? "")}`);
  return summarizeStructuralDiagnostics(diagnosticsFromOxlintJson(JSON.parse(stdout), guiRoot));
}

test("batch-four settings/shell files contain no structural debt", () => {
  assert.deepEqual(structuralDiagnostics(), []);
});

test("settings parse helpers keep blank, fractional rpm, and integer delay rules", () => {
  assert.equal(numberDraft(undefined), "");
  assert.equal(numberDraft(0), "0");
  assert.equal(positiveRpm(""), undefined);
  assert.equal(positiveRpm("   "), undefined);
  assert.equal(positiveRpm("0"), undefined);
  assert.equal(positiveRpm(String(1 / 60)), 1 / 60);
  assert.equal(positiveRpm("1.5"), 1.5);
  assert.equal(positiveInteger("1.5"), undefined);
  assert.equal(positiveInteger("2"), 2);
  assert.equal(positiveInteger("0"), undefined);
});

test("settings dirty and save patch preserve liveModels omission and pacing-only PATCHes", () => {
  const baseDirty = {
    adapter: "openai-responses",
    baseUrl: "https://api.example",
    defaultModel: "",
    authMode: "key",
    apiKeyTransport: "x-api-key",
    note: "",
    allowPrivateNetwork: false,
    liveModels: true,
    savedLiveModels: true,
    itemAdapter: "openai-responses",
    itemBaseUrl: "https://api.example",
    itemDefaultModel: undefined,
    itemAuthMode: "key",
    itemKeyOptional: undefined,
    itemApiKeyTransport: undefined,
    itemNote: undefined,
    itemAllowPrivateNetwork: undefined,
  };
  assert.equal(settingsFormDirty(baseDirty), false);
  assert.equal(settingsFormDirty({ ...baseDirty, authMode: "local", itemAuthMode: undefined, itemKeyOptional: true }), false);
  assert.equal(settingsFormDirty({ ...baseDirty, apiKeyTransport: "bearer" }), false);
  assert.equal(settingsInitialAuthMode(undefined, true), "local");
  assert.equal(settingsInitialAuthMode(undefined, undefined), "key");
  assert.equal(settingsChoicesStatus(true), "loading");
  assert.equal(settingsChoicesStatus(false), "idle");
  assert.equal(settingsHasEndpointPicker("ready", 1), true);
  assert.equal(settingsHasEndpointPicker("ready", 0), false);
  assert.equal(settingsOpenAiView("anthropic", "ready").canonical, false);
  assert.equal(settingsOpenAiView("openai", "ready").canonical, true);
  assert.equal(settingsOpenAiView("openai", "invalid").canonical, false);
  assert.equal(settingsFormDirty({
    ...baseDirty,
    adapter: "anthropic",
    itemAdapter: "anthropic",
    apiKeyTransport: "bearer",
  }), true);
  assert.equal(settingsSupportsApiKeyTransport(" anthropic ", "key"), true);
  assert.equal(settingsPlainBaseUrlLocked(true, "loading"), true);
  assert.equal(settingsPlainBaseUrlLocked(true, "error"), false);

  const pacing = settingsPacingDraft({ enabled: true, rpm: "38", delay: "", models: {} });
  assert.deepEqual(pacing, { enabled: true, requestsPerMinute: 38 });
  assert.notEqual(pacingSignature(pacing), pacingSignature(undefined));

  assert.deepEqual(settingsSavePatch({
    adapter: "",
    nextBaseUrl: "https://x",
    defaultModel: "",
    authMode: "key",
    apiKeyTransport: "x-api-key",
    note: "",
    allowPrivateNetwork: false,
    liveModels: true,
    liveModelDiscoverySupported: true,
    itemLiveModels: undefined,
    itemApiKeyTransport: undefined,
    pacingEnabled: false,
    pacingDraft: { enabled: false },
    dirty: true,
    pacingDirty: false,
  }), { ok: false, messageKey: "pws.adapterBaseRequired" });

  assert.deepEqual(settingsSavePatch({
    adapter: "openai-responses",
    nextBaseUrl: "https://x",
    defaultModel: "",
    authMode: "key",
    apiKeyTransport: "x-api-key",
    note: "",
    allowPrivateNetwork: false,
    liveModels: true,
    liveModelDiscoverySupported: true,
    itemLiveModels: undefined,
    itemApiKeyTransport: undefined,
    pacingEnabled: true,
    pacingDraft: { enabled: true },
    dirty: false,
    pacingDirty: true,
  }), { ok: false, messageKey: "pws.pacingRuleRequired" });

  assert.deepEqual(settingsSavePatch({
    adapter: "openai-responses",
    nextBaseUrl: "https://x",
    defaultModel: "gpt",
    authMode: "key",
    apiKeyTransport: "x-api-key",
    note: " n ",
    allowPrivateNetwork: true,
    liveModels: true,
    liveModelDiscoverySupported: true,
    itemLiveModels: undefined,
    itemApiKeyTransport: undefined,
    pacingEnabled: false,
    pacingDraft: { enabled: false },
    dirty: false,
    pacingDirty: true,
  }), { ok: true, patch: { requestPacing: { enabled: false } } });

  const full = settingsSavePatch({
    adapter: " anthropic ",
    nextBaseUrl: "https://x",
    defaultModel: " claude ",
    authMode: "key",
    apiKeyTransport: "bearer",
    note: " n ",
    allowPrivateNetwork: false,
    liveModels: false,
    liveModelDiscoverySupported: true,
    itemLiveModels: undefined,
    itemApiKeyTransport: "x-api-key",
    pacingEnabled: false,
    pacingDraft: { enabled: false },
    dirty: true,
    pacingDirty: false,
  });
  assert.equal(full.ok, true);
  if (full.ok) {
    assert.equal(full.patch.adapter, "anthropic");
    assert.equal(full.patch.defaultModel, "claude");
    assert.equal(full.patch.note, "n");
    assert.equal(full.patch.liveModels, false);
    assert.equal(full.patch.apiKeyTransport, "bearer");
    assert.equal(full.patch.requestPacing, undefined);
  }

  const omittedLive = settingsSavePatch({
    adapter: "openai-responses",
    nextBaseUrl: "https://x",
    defaultModel: "",
    authMode: "key",
    apiKeyTransport: "x-api-key",
    note: "",
    allowPrivateNetwork: false,
    liveModels: true,
    liveModelDiscoverySupported: true,
    itemLiveModels: undefined,
    itemApiKeyTransport: undefined,
    pacingEnabled: false,
    pacingDraft: { enabled: false },
    dirty: true,
    pacingDirty: false,
  });
  assert.equal(omittedLive.ok, true);
  if (omittedLive.ok) assert.equal("liveModels" in omittedLive.patch, false);

  const clearTransport = settingsSavePatch({
    adapter: "openai-responses",
    nextBaseUrl: "https://x",
    defaultModel: "",
    authMode: "key",
    apiKeyTransport: "bearer",
    note: "",
    allowPrivateNetwork: false,
    liveModels: true,
    liveModelDiscoverySupported: false,
    itemLiveModels: false,
    itemApiKeyTransport: "bearer",
    pacingEnabled: false,
    pacingDraft: { enabled: false },
    dirty: true,
    pacingDirty: false,
  });
  assert.equal(clearTransport.ok, true);
  if (clearTransport.ok) {
    assert.equal(clearTransport.patch.apiKeyTransport, "");
    assert.equal("liveModels" in clearTransport.patch, false);
  }

  assert.deepEqual(settingsOverrideRule(" m ", "10", ""), {
    modelId: "m",
    rule: { requestsPerMinute: 10 },
  });
  assert.equal(settingsOverrideRule("m", "", ""), null);
});

test("configuration pacing treats omitted enabled with a limit as on and uses Number() payloads", () => {
  assert.deepEqual(pacingFromItem({}), { enabled: false, rpm: undefined, minMs: undefined, models: {} });
  assert.equal(pacingFromItem({ requestPacing: { enabled: false, requestsPerMinute: 10 } }).enabled, false);
  assert.equal(pacingFromItem({ requestPacing: { requestsPerMinute: 10 } }).enabled, true);
  assert.equal(pacingFromItem({ requestPacingMs: 1600 }).minMs, 1600);
  assert.equal(pacingFromItem({ requestPacing: { minIntervalMs: 9 }, requestPacingMs: 1600 }).minMs, 9);
  assert.deepEqual(configPacingPayload({ enabled: true, rpm: "1.5", delay: "1.5", models: {} }), {
    enabled: true,
    requestsPerMinute: 1.5,
  });
  assert.equal(configPacingMissingRule({ enabled: true }), true);
  assert.equal(configPacingMissingRule({ enabled: true, models: { a: { requestsPerMinute: 1 } } }), false);
  assert.deepEqual(configOverrideRule("0", "2"), { minIntervalMs: 2 });
  assert.equal(configOverrideRule("0", "1.5"), null);
  assert.deepEqual(configGeneralPatch({
    openaiLogical: true,
    adapter: "x",
    itemAdapter: "openai-responses",
    baseUrl: "https://y",
    itemBaseUrl: "https://api.openai.com/v1",
    defaultModel: " gpt ",
    note: " n ",
  }), { defaultModel: "gpt", note: "n" });
  assert.deepEqual(configGeneralPatch({
    openaiLogical: false,
    adapter: "  ",
    itemAdapter: "openai-chat",
    baseUrl: "",
    itemBaseUrl: "https://kept",
    defaultModel: "",
    note: "",
  }), { adapter: "openai-chat", baseUrl: "https://kept", defaultModel: "", note: "" });
  assert.deepEqual(configDiscoveryPatch({
    allowPrivateNetwork: true,
    discoverySupported: true,
    liveModels: true,
    savedLiveModels: true,
  }), { allowPrivateNetwork: true });
  assert.deepEqual(configDiscoveryPatch({
    allowPrivateNetwork: false,
    discoverySupported: true,
    liveModels: false,
    savedLiveModels: true,
  }), { allowPrivateNetwork: false, liveModels: false });

  const openaiItem = {
    name: "openai",
    adapter: "openai-responses",
    baseUrl: "https://chatgpt.com/backend-api/codex",
    authMode: "forward",
    liveModels: undefined,
  };
  const apiLane = { adapter: "openai-responses", baseUrl: "https://api.openai.com/v1", authMode: "key", requestPacing: { requestsPerMinute: 12 } };
  const overlay = configurationPacingContext(openaiItem, apiLane);
  assert.equal(overlay.openaiLogical, true);
  assert.equal(overlay.pacingName, "openai-apikey");
  assert.equal(overlay.pacingSource.name, "openai-apikey");
  const noLane = configurationPacingContext(openaiItem, undefined);
  assert.equal(noLane.pacingName, "openai");
  assert.equal(noLane.pacingSource.name, "openai");
  assert.equal(overlay.savedLiveModels, overlay.discoverySupported);
  const resetDraft = configurationDraftFields(openaiItem, pacingFromItem(overlay.pacingSource), overlay.discoverySupported);
  const syncDraft = configurationDraftFields(openaiItem, pacingFromItem(openaiItem), overlay.discoverySupported);
  assert.equal(resetDraft.pacingRpm, "12");
  assert.equal(syncDraft.pacingRpm, "");
  assert.deepEqual(configurationModelOptions(["b"], "a", " c "), ["a", "b", "c"]);
  assert.equal(configurationOverrideDraft(" ", "", ""), false);
  assert.equal(configurationOverrideDraft("m", "", ""), true);
});

test("custom model decode and invalidation keep empty-string ids and encoded collisions", () => {
  assert.equal(parseCustomModelIds({}, "openai"), null);
  assert.deepEqual(parseCustomModelIds([null, { provider: "openai", modelId: "" }, { provider: "x", modelId: "a" }], "openai"), [""]);
  const known = knownCustomModelIds({
    availableModels: ["openai-gpt-5.5"],
    customModelIds: [],
    configuredModels: [],
  });
  assert.equal(customModelInvalid({
    customModelsReady: true,
    trimmedCustomModelId: "openai/gpt-5.5",
    availableModels: ["openai-gpt-5.5"],
    customModelIds: [],
    configuredModels: [],
    knownModelIds: known,
  }), true);
  assert.equal(customModelInvalid({
    customModelsReady: false,
    trimmedCustomModelId: "fresh",
    availableModels: [],
    customModelIds: [],
    configuredModels: [],
    knownModelIds: [],
  }), true);
  assert.equal(modelsEmptyBase({ availableModels: [], configuredModels: [], customModelIds: [], defaultModel: "x" }), false);
  assert.equal(showingConfiguredModelsFallback(0, 2), true);
  assert.equal(showingConfiguredModelsFallback(0, 0), false);
  const chips = visibleModelChips(Array.from({ length: CUSTOM_MODEL_CHIP_RENDER_CAP + 1 }, (_, i) => i));
  assert.equal(chips.capped, true);
  assert.equal(chips.visible.length, CUSTOM_MODEL_CHIP_RENDER_CAP);
});

test("connection test message precedence keeps server text ahead of applicable/ok keys", () => {
  assert.deepEqual(connectionTestOutcome(true, { ok: false, message: "upstream" }), { ok: false, text: "upstream" });
  assert.deepEqual(connectionTestOutcome(false, { error: "boom", applicable: false }), { ok: false, text: "boom" });
  assert.deepEqual(connectionTestOutcome(true, { applicable: false }), { ok: true, messageKey: "pws.connectionNotApplicable" });
  assert.deepEqual(connectionTestOutcome(true, { ok: false }), { ok: false, messageKey: "pws.connectionFailed" });
  assert.deepEqual(connectionTestOutcome(true, {}), { ok: true, messageKey: "pws.connectionOk" });
  assert.deepEqual(connectionTestOutcome(true, null), { ok: true, messageKey: "pws.connectionOk" });
  assert.equal(nextDetailTabIndex("ArrowRight", 2, 3), 0);
  assert.equal(nextDetailTabIndex("ArrowLeft", 0, 3), 2);
  assert.equal(nextDetailTabIndex("Home", 2, 3), 0);
  assert.equal(nextDetailTabIndex("End", 0, 3), 2);
  assert.equal(nextDetailTabIndex("Enter", 0, 3), null);
  assert.equal(detailStatusPillKind("ready"), "ready");
  assert.equal(detailStatusPillKind("disabled"), "disabled");
  assert.equal(detailStatusPillKind("needsSetup"), "attention");
  assert.equal(detailStatusPillKind("needs-setup"), "attention");
  assert.equal(detailAccessCredentialPresent({ adapter: "x" }, []), true);
  assert.equal(detailAccessCredentialPresent(undefined, [{ id: "k" }]), true);
  assert.equal(detailAccessCredentialPresent(undefined, []), false);
  assert.equal(scopedAccountsFocusToken("openai", "openai", 4), 4);
  assert.equal(scopedAccountsFocusToken("other", "openai", 4), 0);
  assert.equal(clampedDetailTab(false, "access"), "overview");
  assert.equal(clampedDetailTab(true, "access"), "access");
  assert.deepEqual(accountsFocusAdjustment(2, 2), null);
  assert.deepEqual(accountsFocusAdjustment(3, 2), { seen: 3, tab: "access" });
  assert.deepEqual(accountsFocusAdjustment(0, 3), { seen: 0, tab: null });
  assert.deepEqual(pendingLeaveTab("overview", true), { pending: "overview" });
  assert.deepEqual(pendingLeaveTab("configuration", true), { tab: "configuration" });
  assert.equal(pendingLeaveBack(true), "pending");
  assert.equal(pendingLeaveBack(false), "leave");
  const overflow = { open: true };
  closeOverflowDetails(overflow);
  assert.equal(overflow.open, false);
  closeOverflowDetails(null);
  const nested = { open: true };
  closeDetailsOverflow({ currentTarget: { closest: () => nested } });
  assert.equal(nested.open, false);
  closeDetailsOverflow({ currentTarget: { closest: () => null } });
  assert.equal(overviewReauthAccountId([
    { id: "idle", active: true },
    { id: "stale", active: false, needsReauth: true },
    { id: "active-stale", active: true, needsReauth: true },
  ]), "active-stale");
  assert.equal(overviewReauthAccountId([{ id: "stale", active: false, needsReauth: true }]), "stale");
});

function access() {
  return { methods: [] };
}

test("workspace quota decode keeps extra keys and rejects missing quota or non-finite updatedAt", () => {
  assert.equal(parseQuotaReport(null), null);
  assert.equal(parseQuotaReport({ updatedAt: 1 }), null);
  assert.equal(parseQuotaReport({ updatedAt: Number.NaN, quota: {} }), null);
  assert.equal(parseQuotaReport({ updatedAt: 1, quota: null, label: 1 }), null);
  assert.deepEqual(parseQuotaReport({ updatedAt: 1, quota: null, extra: true }), { updatedAt: 1, quota: null });
  assert.deepEqual(quotaReportRows({ reports: [{ provider: "a" }] }), [{ provider: "a" }]);
  assert.deepEqual(quotaReportRows([{ provider: "a" }]), [{ provider: "a" }]);
  assert.deepEqual(quotaReportRows({}), []);
  const reports = freshQuotaReportsFromResponse({
    reports: [
      { provider: "openai", updatedAt: 2, quota: { weekly: 1 } },
      { provider: "  ", updatedAt: 2, quota: {} },
      { updatedAt: 2, quota: {} },
    ],
  });
  assert.deepEqual(Object.keys(reports), ["openai"]);
  assert.equal(quotaReportsFromCache({ openai: { updatedAt: 2, quota: {} } })?.openai?.updatedAt, 2);
  assert.equal(quotaReportsFromCache({}), null);
});

test("workspace sections, filters, and openai overlays preserve lifecycle and merge order", () => {
  const workspace = {
    summary: { totalProviders: 2, healthy: 1, attention: 1, disabled: 0, exposedModels: 0 },
    providers: [
      {
        id: "openai",
        connections: [],
        hidden: [],
        lifecycle: "healthy",
        modelCount: 1,
        access: access(),
        disabled: false,
        lastValidated: 9,
        downstream: { harnesses: 1, routes: 2, subagents: 3 },
      },
      {
        id: "missing",
        connections: [],
        hidden: [],
        lifecycle: "attention",
        modelCount: 0,
        access: access(),
        disabled: false,
        lastValidated: null,
        downstream: { harnesses: null, routes: 0, subagents: 0 },
      },
      {
        id: "local",
        connections: [],
        hidden: [],
        lifecycle: "attention",
        modelCount: 0,
        access: access(),
        disabled: false,
        lastValidated: null,
        downstream: { harnesses: null, routes: 0, subagents: 0 },
      },
    ],
    attention: [],
    availability: { modelsAvailable: 0, modelsUnavailable: 0, staleProviderCatalogues: null, lastModelSync: null },
    downstream: { harnessCount: null, routeCount: 0, subAgentModelCount: 0, affectedRouteCount: 0 },
    recentEvents: [{ provider: "openai", type: "ok", severity: "ok", timestamp: 1 }],
  };
  const providers = {
    openai: { adapter: "openai-responses", baseUrl: "https://chatgpt.com/backend-api/codex", authMode: "forward" },
    local: { adapter: "openai-chat", baseUrl: "http://127.0.0.1:11434", authMode: "local" },
  };
  const sections = sectionsFromWorkspace(workspace, providers, { openai: true });
  assert.equal(sections.ready[0]?.name, "openai");
  assert.equal(sections.ready[0]?.activeNeedsReauth, true);
  assert.equal(sections.ready[0]?.tier, "accounts");
  assert.equal(sections.needsSetup[0]?.name, "local");
  assert.equal(sections.needsSetup[0]?.tier, undefined);

  const allOn = {
    statusFilter: { ready: true, needsSetup: true, disabled: true },
    pricingFilter: { free: true, paid: true },
    typeFilter: { cloud: true, local: true, selfHosted: true, login: true },
    sortMode: "az",
  };
  assert.equal(workspaceFilterActive(allOn), false);
  assert.equal(workspaceFilterActive({ ...allOn, sortMode: "za" }), true);
  assert.equal(itemMatchesWorkspaceFilters(sections.ready[0], "openai", allOn.pricingFilter, allOn.typeFilter), true);
  assert.equal(itemMatchesWorkspaceFilters(sections.ready[0], "chat", allOn.pricingFilter, allOn.typeFilter), false);
  assert.equal(itemMatchesWorkspaceFilters(sections.ready[0], "nope", allOn.pricingFilter, allOn.typeFilter), false);
  const filtered = filterWorkspaceSections(sections, "", { ...allOn.statusFilter, ready: false }, allOn.pricingFilter, allOn.typeFilter, "az");
  assert.deepEqual(filtered.ready, []);
  assert.equal(filtered.needsSetup.length, 1);

  assert.deepEqual(mergeProviderValues({ openai: ["a"], "openai-apikey": ["a", "b"] }, "openai", { "openai-apikey": providers.local }), ["a", "b"]);
  assert.deepEqual(mergeProviderValues({ openai: ["a"], "openai-apikey": ["b"] }, "openai", {}), ["a"]);
  assert.equal(providerHasLiveModels("openai", { openai: 0, "openai-apikey": 2 }), true);
  assert.equal(providerHasLiveModels("local", { "openai-apikey": 2 }), false);
  assert.equal(selectedItemApiLane("openai", { "openai-apikey": providers.local })?.adapter, "openai-chat");
  assert.equal(selectedItemApiLane("local", { "openai-apikey": providers.local }), undefined);
  assert.equal(eventsForProvider(workspace.recentEvents, "openai").length, 1);
  assert.deepEqual(usageTotalsFromResponse([{ provider: "openai", requests: 1 }]), { openai: { requests: 1, totalTokens: undefined } });
  assert.equal(usageModelsFromResponse([{
    provider: "openai",
    model: "gpt",
    requests: 1,
    totalTokens: 2,
    inputTokens: 1,
    outputTokens: 1,
    shareRatio: 1,
    estimatedCostUsd: 0,
  }]).openai?.[0]?.estimatedCostUsd, 0);
  assert.equal(nextRailTabbableName(["a", "b"], "b", "a"), "b");
  assert.equal(nextRailTabbableName(["a", "b"], "gone", "a"), "a");
  assert.equal(nextRailTabbableName(["a"], null, null), "a");
  assert.deepEqual([...duplicateProviderDisplayNames([
    { name: "openai", adapter: "x", baseUrl: "y" },
    { name: "openai-apikey", adapter: "x", baseUrl: "y" },
  ], name => name === "openai" || name === "openai-apikey" ? "OpenAI" : name)], ["OpenAI"]);
  const groups = workspaceRailGroups([
    { name: "a", adapter: "x", baseUrl: "y", workspaceLifecycle: "healthy" },
    { name: "b", adapter: "x", baseUrl: "y", workspaceLifecycle: "disabled" },
  ]);
  assert.equal(groups[0]?.items[0]?.name, "a");
  assert.equal(groups[2]?.items[0]?.name, "b");
  assert.equal(workspaceKpis(null), null);
  assert.deepEqual(workspaceKpis(workspace), {
    total: 2, healthy: 1, attention: 1, disabled: 0, models: 0,
  });
  assert.equal(workspaceMainKind(true, true, true), "json");
  assert.equal(workspaceMainKind(false, true, true), "detail");
  assert.equal(workspaceMainKind(false, false, true), "dashboard");
  assert.equal(workspaceMainKind(false, false, false), "none");
});
