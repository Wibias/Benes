import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

import {
  diagnosticsFromOxlintJson,
  summarizeStructuralDiagnostics,
} from "./check-structural-baseline.ts";
import { comboMutationSucceeded } from "../src/combo-workspace-data.ts";
import {
  parseComboWorkspaceModels,
  parseComboWorkspaceProviders,
} from "../src/components/combo-workspace-utils.ts";
import {
  otherComboIdentity,
  followComboAfterSave,
} from "../src/components/combo-workspace-identity.ts";
import { newComboTarget } from "../src/combo-workspace-data.ts";
import {
  comboFirstModelId,
  comboModelChoices,
  comboModelIds,
  comboMoveTarget,
  comboPatchTarget,
  comboPickerProviders,
  comboProviderChoices,
  comboRemoveTarget,
  comboTargetRowClass,
  comboTargetRows,
  combosOverviewCopy,
} from "../src/components/combo-workspace-utils.ts";
import {
  parseObservationDto,
  parseVerdictDto,
} from "../src/lab/lab-records.ts";
import { catalogValue } from "../src/i18n/catalogs.ts";
import type { Locale } from "../src/i18n/locale-registry.ts";
import type { TKey } from "../src/i18n/en.ts";
import {
  MODELS_CHANGE_SYNC_DEBOUNCE_MS,
  createTrailingDebounce,
} from "../src/pages/models-change-sync.ts";
import {
  catalogFooterCounts,
  filterCatalogGroups,
  gpt56Family,
  modelAdvertisedMax,
  modelContextDisplayValue,
  providerContextSummary,
  allowedContextPresets,
  providerAllowedContextPresets,
  catalogContextSelectOptions,
  modelContextChoices,
  contextMaxOptionLabel,
  contextSelectValue,
  isAdvertisedContextSelection,
  providerContextBulkWindows,
  staleCatalogPresetOverrides,
  CONTEXT_MAX_VALUE,
  CONTEXT_MIXED_VALUE,
} from "../src/pages/models-catalog-filter.ts";
import {
  firstModelForProvider,
  mergeRoutingModels,
  modelOptionsForProvider,
  parseRoutingModels,
  routingDryRunEvidence,
  selectedProfileAfterLoad,
} from "../src/pages/routing-profile-decode.ts";
import { readLabCatalog } from "../src/lab/lab-catalog.ts";
import {
  contextWindowLabel,
  evidenceAgeLabel,
  optionalRequirementLabel,
  overviewCandidateRows,
  overviewCompatibilityRows,
  overviewRequirementRows,
  overviewUnknownEvidenceRows,
  profileMatchesQuery,
  profileSubtitle,
  profileTitle,
  uniqueCandidateProviders,
  usdCostLabel,
  weightPercentLabel,
} from "../src/pages/routing-profile-summary.ts";
import {
  costIncompleteNote,
  fmtUsd,
  dryRunEvalResultKind,
  fmtCount,
  fmtMs,
  fmtRate,
  friendlyEnumLabel,
  parseRoutingAnalytics,
  presentAnalyticsPercentiles,
  presentScoreComponents,
} from "../src/pages/routing-profiles-format.ts";
import { parseRoutingProfiles } from "../src/routing-profile/profile-codec.ts";
import {
  applyProviderModelContextWindows,
  mergeProviderCatalogMeta,
  overlayPendingModelContextWindows,
  remainingModelContextWindowPatches,
} from "../src/models-groups.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");
const oxlintBin = path.join(guiRoot, "node_modules", "oxlint", "bin", "oxlint");
const configPath = path.join(guiRoot, ".oxlintrc.json");
const targetFiles = [
  path.join(guiRoot, "src", "components", "ComboWorkspace.tsx"),
  path.join(guiRoot, "src", "components", "combo-workspace-detail-panel.tsx"),
  path.join(guiRoot, "src", "components", "combo-workspace-identity.ts"),
  path.join(guiRoot, "src", "components", "combo-workspace-rail.tsx"),
  path.join(guiRoot, "src", "components", "combo-workspace-controls.tsx"),
  path.join(guiRoot, "src", "components", "combo-workspace-dialogs.tsx"),
  path.join(guiRoot, "src", "components", "combo-workspace-overview-panel.tsx"),
  path.join(guiRoot, "src", "components", "combo-workspace-utils.ts"),
  path.join(guiRoot, "src", "components", "combo-workspace-types.ts"),
  path.join(guiRoot, "src", "pages", "Combos.tsx"),
  path.join(guiRoot, "src", "pages", "CompatibilityMatrix.tsx"),
  path.join(guiRoot, "src", "pages", "Models.tsx"),
  path.join(guiRoot, "src", "pages", "RoutingProfiles.tsx"),
  path.join(guiRoot, "src", "lab", "evidence-vocabulary.ts"),
  path.join(guiRoot, "src", "lab", "lab-records.ts"),
  path.join(guiRoot, "src", "lab", "lab-pages.ts"),
  path.join(guiRoot, "src", "lab", "lab-client.ts"),
  path.join(guiRoot, "src", "lab", "evidence-matrix.ts"),
  path.join(guiRoot, "src", "pages", "compatibility-matrix-labels.ts"),
  path.join(guiRoot, "src", "pages", "compatibility-matrix-sections.tsx"),
  path.join(guiRoot, "src", "pages", "compatibility-matrix-view.ts"),
  path.join(guiRoot, "src", "pages", "models-catalog-controls-data.ts"),
  path.join(guiRoot, "src", "pages", "models-catalog-controls.tsx"),
  path.join(guiRoot, "src", "pages", "models-catalog-settings.tsx"),
  path.join(guiRoot, "src", "pages", "models-catalog-filter.ts"),
  path.join(guiRoot, "src", "pages", "models-new-policy-control.tsx"),
  path.join(guiRoot, "src", "pages", "models-catalog-modals.tsx"),
  path.join(guiRoot, "src", "pages", "models-modal-shell.tsx"),
  path.join(guiRoot, "src", "pages", "models-catalog-panel.tsx"),
  path.join(guiRoot, "src", "pages", "models-page-shell.tsx"),
  path.join(guiRoot, "src", "pages", "control-board.tsx"),
  path.join(guiRoot, "src", "pages", "control-board-state.ts"),
  path.join(guiRoot, "src", "pages", "control-board-settings.ts"),
  path.join(guiRoot, "src", "pages", "routing-profile-summary.ts"),
  path.join(guiRoot, "src", "pages", "control-tab.ts"),
  path.join(guiRoot, "src", "pages", "control-tab-strip.tsx"),
  path.join(guiRoot, "src", "pages", "models-provider-group-data.ts"),
  path.join(guiRoot, "src", "pages", "models-provider-group.tsx"),
  path.join(guiRoot, "src", "pages", "routing-profile-decode.ts"),
  path.join(guiRoot, "src", "pages", "routing-profile-summary.ts"),
  path.join(guiRoot, "src", "pages", "routing-profiles-format.ts"),
  path.join(guiRoot, "src", "pages", "routing-profiles-form.tsx"),
  path.join(guiRoot, "src", "pages", "routing-profiles-sections.tsx"),
  path.join(guiRoot, "src", "routing-profile", "profile-model.ts"),
  path.join(guiRoot, "src", "routing-profile", "profile-codec.ts"),
  path.join(guiRoot, "src", "routing-profile", "profile-response.ts"),
];

function structuralDiagnostics() {
  const result = spawnSync(
    process.execPath,
    [oxlintBin, "-c", configPath, "--format=json", ...targetFiles],
    { cwd: guiRoot, encoding: "utf8" },
  );
  if (result.error) throw result.error;
  const stdout = String(result.stdout ?? "").trim();
  assert.ok(
    stdout,
    `Oxlint models/routing/combos scan produced no output:\n${String(result.stderr ?? "")}`,
  );
  return summarizeStructuralDiagnostics(
    diagnosticsFromOxlintJson(JSON.parse(stdout), guiRoot),
  );
}

const DIGEST_A = "a".repeat(64);
const DIGEST_B = "b".repeat(64);

function verdictDto(overrides = {}) {
  return {
    projectionKey: "pk-1",
    subjectId: "subject-1",
    evidenceLayer: "protocol_conformance",
    suiteId: "suite-a",
    suiteVersion: "1",
    suiteManifestDigest: DIGEST_A,
    projectionSpecVersion: "v1",
    verdict: "VERIFIED",
    asOf: 1,
    scenarioManifestDigests: [DIGEST_B],
    claimSourceDigest: null,
    contributingEventIds: ["e1"],
    contradictingEventIds: [],
    notes: [],
    ...overrides,
  };
}

function observationDto(overrides = {}) {
  return {
    eventId: "evt-1",
    subjectId: "subject-1",
    evidenceLayer: "live_route_compatibility",
    suiteId: "suite-a",
    suiteVersion: "1",
    suiteManifestDigest: DIGEST_A,
    scenarioId: "scenario-1",
    scenarioVersion: "1",
    scenarioManifestDigest: DIGEST_B,
    outcome: "pass",
    completedAt: 10,
    executionMode: "live",
    excluded: false,
    exclusionReason: null,
    ...overrides,
  };
}

test("batch models/routing/combos files contain no structural debt", () => {
  assert.deepEqual(structuralDiagnostics(), []);
});

test("verdict parser keeps extra fields and rejects incomplete rows", () => {
  const extra = verdictDto({ futureServerField: true });
  const parsed = parseVerdictDto(extra);
  assert.ok(parsed);
  assert.equal(parsed.verdict, "VERIFIED");
  assert.equal(parsed.futureServerField, true);
  assert.equal(parseVerdictDto(verdictDto({ projectionKey: "" })), null);
  assert.equal(parseVerdictDto(verdictDto({ evidenceLayer: "unknown" })), null);
  assert.equal(parseVerdictDto(verdictDto({ asOf: Number.NaN })), null);
  assert.equal(parseVerdictDto(verdictDto({ notes: ["ok", 2] })), null);
  assert.equal(parseVerdictDto(null), null);
});

test("observation parser allows empty identity strings and rejects bad layers", () => {
  const parsed = parseObservationDto(
    observationDto({ eventId: "", subjectId: "" }),
  );
  assert.ok(parsed);
  assert.equal(parsed.eventId, "");
  assert.equal(
    parseObservationDto(observationDto({ evidenceLayer: "nope" })),
    null,
  );
  assert.equal(parseObservationDto(observationDto({ excluded: "no" })), null);
  assert.equal(
    parseObservationDto(observationDto({ completedAt: Infinity })),
    null,
  );
});

test("routing model parser skips combo/policy/disabled duplicates", () => {
  const models = parseRoutingModels({
    models: [
      { provider: "openai", id: "gpt-4o" },
      { provider: " openai ", id: " gpt-4o " },
      { provider: "combo", id: "free" },
      { provider: "policy", id: "fast" },
      { provider: "openai", id: "o1", disabled: true },
      { provider: "", id: "missing" },
      "skip",
    ],
  });
  assert.deepEqual(models, [{ provider: "openai", id: "gpt-4o" }]);
  assert.deepEqual(
    parseRoutingModels([{ provider: "anthropic", id: "claude" }]),
    [{ provider: "anthropic", id: "claude" }],
  );
});

test("mergeRoutingModels fills provider catalogs and sorts alphabetically", () => {
  const models = mergeRoutingModels(
    [
      { provider: "openai", id: "gpt-5.4" },
      { provider: "openai", id: "hidden", disabled: true },
      { provider: "command-code", id: "deepseek/deepseek-v4-flash", disabled: true },
    ],
    {
      "command-code": {
        models: [
          { id: "deepseek/deepseek-v4-flash" },
          { id: "MiniMaxAI/MiniMax-M2.5" },
          { id: "Qwen/Qwen3.6-Plus" },
        ],
      },
      openai: { models: [{ id: "gpt-5.5" }, { id: "gpt-5.4" }] },
      offline: { disabled: true, models: [{ id: "skip-me" }] },
    },
  );
  assert.deepEqual(
    models.map((model) => `${model.provider}/${model.id}`),
    [
      "command-code/MiniMaxAI/MiniMax-M2.5",
      "command-code/Qwen/Qwen3.6-Plus",
      "openai/gpt-5.4",
      "openai/gpt-5.5",
    ],
  );
  assert.equal(firstModelForProvider(models, "command-code"), "MiniMaxAI/MiniMax-M2.5");
  assert.deepEqual(
    modelOptionsForProvider(models, "openai").map((model) => model.id),
    ["gpt-5.4", "gpt-5.5"],
  );
});

test("routing profile parser normalizes alias and drops malformed compatibility", () => {
  const profiles = parseRoutingProfiles({
    profiles: [
      {
        id: "fast",
        model: "policy/fast",
        revision: "1",
        candidates: [{ provider: "openai", model: "gpt-4o" }],
        require: {},
        optimize: { latency: 1, health: 0, cost: 0, quota: 0 },
        limits: {},
        unknownEvidence: {
          capability: "exclude",
          health: "penalize",
          quota: "penalize",
          cost: "penalize",
        },
        compatibility: {
          requiredSuites: [
            { suiteId: "s", evidenceLayer: "protocol_conformance" },
          ],
        },
      },
      { id: "broken" },
    ],
  });
  assert.equal(profiles.length, 1);
  assert.equal(profiles[0]?.alias, null);
  assert.equal(profiles[0]?.compatibility?.requiredSuites[0]?.suiteId, "s");
  assert.equal(selectedProfileAfterLoad(profiles, null)?.id, "fast");
  assert.equal(
    selectedProfileAfterLoad(profiles, "missing", "fast")?.id,
    "fast",
  );
});

test("routing dry-run evidence only includes finite positive context", () => {
  assert.deepEqual(
    routingDryRunEvidence({
      context: " 2048 ",
      tools: true,
      image: false,
      structured: true,
    }),
    {
      contextWindow: 2048,
      toolsRequired: true,
      structuredOutputRequired: true,
    },
  );
  assert.deepEqual(
    routingDryRunEvidence({
      context: "nope",
      tools: false,
      image: true,
      structured: false,
    }),
    { imageInputRequired: true },
  );
  assert.equal(readLabCatalog({ scenarios: "no" }), null);
});

test("combo workspace load catalogs combo ids and backfills defaults", () => {
  const providers = {
    openai: {
      adapter: "openai-responses",
      baseUrl: "https://example.invalid",
      defaultModel: "gpt-4o-mini",
    },
    skipped: {
      adapter: "openai-completions",
      baseUrl: "https://example.invalid",
      disabled: true,
      defaultModel: "hidden",
    },
  };
  const parsed = parseComboWorkspaceModels(
    [
      { provider: "combo", id: "free" },
      {
        provider: "openai",
        id: "gpt-4o",
        reasoningEfforts: ["low", 1],
        inputModalities: [" image ", ""],
      },
      { provider: "openai", id: "disabled", disabled: true },
    ],
    providers,
  );
  assert.deepEqual(parsed.cataloguedComboIds, ["free"]);
  assert.deepEqual(parsed.models.map((model) => model.id).sort(), [
    "gpt-4o",
    "gpt-4o-mini",
  ]);
  const live = parsed.models.find((model) => model.id === "gpt-4o");
  assert.deepEqual(live?.reasoningEfforts, ["low"]);
  assert.deepEqual(live?.inputModalities, ["image"]);
  assert.equal(parseComboWorkspaceModels({ models: [] }, {}).models.length, 0);
  assert.equal(
    parseComboWorkspaceProviders(providers, { openai: providers.openai }).find(
      (row) => row.name === "skipped",
    )?.hiddenFromPicker,
    true,
  );
  assert.deepEqual(comboMutationSucceeded({ success: true }), { ok: true });
  assert.deepEqual(comboMutationSucceeded({ success: false, error: "nope" }), {
    ok: false,
    error: "nope",
  });
});

test("combo identity helpers skip the edited row and follow renames", () => {
  const combos = [
    {
      id: "a",
      alias: "alias-a",
      model: "combo/a",
      nativeAlias: false,
      displayName: null,
      strategy: "failover",
      defaultEffort: null,
      targets: [],
    },
    {
      id: "b",
      alias: null,
      model: "combo/b",
      nativeAlias: false,
      displayName: null,
      strategy: "failover",
      defaultEffort: null,
      targets: [],
    },
  ];
  assert.deepEqual(otherComboIdentity(combos, "a"), {
    ids: ["b"],
    aliases: [],
  });
  assert.deepEqual(followComboAfterSave(combos[1], "a"), {
    selectedId: "b",
    localBaseline: null,
  });
  assert.deepEqual(followComboAfterSave(combos[0], "a"), {
    selectedId: "a",
    localBaseline: combos[0],
  });
});

test("catalog filters match provider, query, and visibility independently", () => {
  const groups = [
    { provider: "openai", rows: [{ id: "gpt-5.4" }, { id: "gpt-mini" }] },
    { provider: "anthropic", rows: [{ id: "claude" }] },
    { provider: "empty", rows: [] },
  ];
  const isVisible = (_provider, model) => model.id !== "gpt-mini";
  const label = (provider) => (provider === "openai" ? "OpenAI" : provider);

  assert.equal(
    filterCatalogGroups(groups, {
      provider: null,
      query: "",
      visibility: "all",
      providerLabel: label,
      isVisible,
    }).length,
    3,
  );

  assert.deepEqual(
    filterCatalogGroups(groups, {
      provider: "openai",
      query: "",
      visibility: "all",
      providerLabel: label,
      isVisible,
    }).map((group) => group.provider),
    ["openai"],
  );

  const searched = filterCatalogGroups(groups, {
    provider: null,
    query: "gpt-5.4",
    visibility: "all",
    providerLabel: label,
    isVisible,
  });
  assert.deepEqual(
    searched.map((group) => [group.provider, group.rows.map((row) => row.id)]),
    [["openai", ["gpt-5.4"]]],
  );

  const byBrand = filterCatalogGroups(groups, {
    provider: null,
    query: "openai",
    visibility: "all",
    providerLabel: label,
    isVisible,
  });
  assert.equal(byBrand.length, 1);
  assert.equal(byBrand[0]?.rows.length, 2);

  const visibleOnly = filterCatalogGroups(groups, {
    provider: null,
    query: "",
    visibility: "visible",
    providerLabel: label,
    isVisible,
  });
  assert.deepEqual(
    visibleOnly.map((group) => [
      group.provider,
      group.rows.map((row) => row.id),
    ]),
    [
      ["openai", ["gpt-5.4"]],
      ["anthropic", ["claude"]],
    ],
  );

  const hiddenOnly = filterCatalogGroups(groups, {
    provider: null,
    query: "",
    visibility: "hidden",
    providerLabel: label,
    isVisible,
  });
  assert.deepEqual(
    hiddenOnly.map((group) => [
      group.provider,
      group.rows.map((row) => row.id),
    ]),
    [["openai", ["gpt-mini"]]],
  );

  assert.deepEqual(catalogFooterCounts(groups, isVisible), {
    providers: 3,
    models: 3,
    visible: 2,
  });
  assert.equal(
    modelContextDisplayValue({ advertised: 128000, providerCap: 350000 }),
    128000,
  );
  assert.equal(
    modelContextDisplayValue({
      override: 64000,
      advertised: 128000,
      providerCap: 350000,
    }),
    64000,
  );
  assert.equal(modelContextDisplayValue({ providerCap: 350000 }), 350000);
  assert.equal(
    modelContextDisplayValue({
      override: 64000,
      advertised: 1050000,
      providerCap: 128000,
    }),
    64000,
  );
  assert.equal(
    modelAdvertisedMax({
      advertised: 272000,
      providerCap: 128000,
      nativeFloor: 1050000,
    }),
    1050000,
  );
  assert.equal(
    modelAdvertisedMax({ advertised: 64000, providerCap: 128000 }),
    64000,
  );
  assert.equal(gpt56Family("gpt-5.6-sol"), true);
  assert.equal(gpt56Family("gpt-5.4"), false);
  assert.deepEqual(
    staleCatalogPresetOverrides(
      {
        "gpt-5.4": 64000,
        "gpt-5.6-sol": 64000,
      },
      { "gpt-5.4": 272000, "gpt-5.6-sol": 1050000 },
    ),
    { "gpt-5.4": null, "gpt-5.6-sol": null },
  );
  assert.equal(
    staleCatalogPresetOverrides(
      { "gpt-5.6-sol": 64000 },
      { "gpt-5.4": 272000, "gpt-5.6-sol": 1050000 },
    ),
    null,
  );
});

test("provider context summary is Mixed unless every model shares one window", () => {
  assert.deepEqual(providerContextSummary([]), { kind: "empty" });
  assert.deepEqual(providerContextSummary([128000, 128000, 128000]), {
    kind: "value",
    tokens: 128000,
  });
  assert.deepEqual(providerContextSummary([128000, 256000, 128000]), {
    kind: "mixed",
  });
});

test("provider-level context bulk write applies one value to every eligible model", () => {
  assert.equal(
    providerContextBulkWindows(CONTEXT_MIXED_VALUE, ["a", "b"]),
    null,
  );
  assert.deepEqual(providerContextBulkWindows("128000", ["sol", "luna"]), {
    sol: 128000,
    luna: 128000,
  });
  assert.deepEqual(
    providerContextBulkWindows(CONTEXT_MAX_VALUE, ["sol", "luna"]),
    { sol: null, luna: null },
  );
  assert.equal(providerContextBulkWindows("nope", ["sol"]), null);
});

test("context dropdowns list only presets at or below the advertised max", () => {
  assert.deepEqual(allowedContextPresets(128000), [32000, 64000, 128000]);
  assert.deepEqual(
    allowedContextPresets(922000),
    [32000, 64000, 128000, 256000],
  );
  assert.deepEqual(
    providerAllowedContextPresets([128000, 256000]),
    [32000, 64000, 128000],
  );
  assert.deepEqual(
    modelContextChoices({ advertisedMax: 1050000, standard: 272000 }),
    [32000, 64000, 128000, 256000, 272000],
  );
  assert.deepEqual(
    modelContextChoices({ advertisedMax: 272000 }),
    [32000, 64000, 128000, 256000],
  );
  assert.equal(contextMaxOptionLabel(1050000, "Max"), "1.05M");
  assert.equal(
    contextSelectValue({ display: 1050000, advertisedMax: 1050000 }),
    CONTEXT_MAX_VALUE,
  );
  assert.equal(
    contextSelectValue({
      display: 272000,
      advertisedMax: 1050000,
      override: 272000,
    }),
    "272000",
  );
  assert.equal(isAdvertisedContextSelection("max", 1050000), true);
  assert.equal(isAdvertisedContextSelection("1050000", 1050000), true);
  const mixed = catalogContextSelectOptions({
    presets: [32000, 64000, 128000],
    mixed: true,
    mixedLabel: "Mixed",
    maxLabel: "Max",
  });
  assert.equal(mixed[0]?.value, CONTEXT_MIXED_VALUE);
  assert.equal(mixed.at(-1)?.value, CONTEXT_MAX_VALUE);
  const current = catalogContextSelectOptions({
    presets: [32000, 64000, 128000],
    current: 272000,
    mixedLabel: "Mixed",
    maxLabel: "Max",
  });
  assert.equal(current[0]?.value, "272000");
  assert.equal(current[0]?.label, "272k");
  const available = catalogContextSelectOptions({
    presets: [32000, 64000, 128000, 256000],
    current: 1050000,
    mixedLabel: "Mixed",
    maxLabel: "Max",
  });
  assert.equal(available[0]?.value, "1050000");
  assert.equal(available[0]?.label, "1.05M");
});

test("catalog meta merge keeps saved model context windows from GET /api/config", () => {
  const listed = [{ name: "openai", hasApiKey: false }];
  const merged = mergeProviderCatalogMeta(listed, {
    openai: {
      authMode: "forward",
      liveModels: true,
      modelContextWindows: {
        "gpt-5.4": 32000,
        "gpt-5.5": 64000,
        skip: "nope",
        "": 128000,
      },
    },
  });
  assert.equal(merged[0]?.authMode, "forward");
  assert.equal(merged[0]?.liveModels, true);
  assert.deepEqual(merged[0]?.modelContextWindows, {
    "gpt-5.4": 32000,
    "gpt-5.5": 64000,
  });
  assert.equal(
    mergeProviderCatalogMeta(listed, { openai: { authMode: "forward" } })[0]
      ?.modelContextWindows,
    undefined,
  );
});

test("context window patches apply locally and survive a stale catalog poll", () => {
  const listed = [
    {
      name: "openai",
      modelContextWindows: { "gpt-5.4": 64000, "gpt-5.5": 64000 },
    },
  ];
  const patched = applyProviderModelContextWindows(listed, "openai", {
    "gpt-5.4": 32000,
  });
  assert.deepEqual(patched[0]?.modelContextWindows, {
    "gpt-5.4": 32000,
    "gpt-5.5": 64000,
  });
  assert.deepEqual(
    applyProviderModelContextWindows(patched, "openai", { "gpt-5.4": null })[0]
      ?.modelContextWindows,
    { "gpt-5.5": 64000 },
  );

  const pending = [{ provider: "openai", windows: { "gpt-5.4": 32000 } }];
  const stale = remainingModelContextWindowPatches(listed, pending);
  assert.equal(stale.length, 1);
  assert.deepEqual(
    overlayPendingModelContextWindows(listed, stale)[0]?.modelContextWindows,
    { "gpt-5.4": 32000, "gpt-5.5": 64000 },
  );
  assert.deepEqual(remainingModelContextWindowPatches(patched, pending), []);
});

test("routing profile summary uses live DTO values without invented copy", () => {
  const profile = {
    id: "balanced",
    alias: "Balanced",
    icon: null,
    revision: "1",
    model: "policy/balanced",
    candidates: [
      { provider: "openai", model: "gpt-4o" },
      { provider: "openai", model: "o1" },
      { provider: "anthropic", model: "claude" },
    ],
    require: {
      tools: true,
      imageInput: undefined,
      structuredOutput: false,
      minContextWindow: 128000,
      reasoningEffort: "high",
      serviceTier: "priority",
      minQuotaHeadroom: 0.2,
      localOnly: true,
      encryptedCodexTasks: false,
    },
    optimize: { latency: 0.55, health: 0.25, cost: 0.1, quota: 0.1 },
    limits: { maxEstimatedCostUsd: 0.25, onUnknownCost: "exclude" as const },
    unknownEvidence: {
      capability: "exclude" as const,
      health: "penalize" as const,
      quota: "penalize" as const,
      cost: "allow" as const,
    },
    compatibility: {
      requiredSuites: [{ suiteId: "codex", evidenceLayer: "protocol_conformance" as const }],
      minStatus: "VERIFIED" as const,
      maxEvidenceAgeMs: 7 * 86_400_000,
      unknownEvidence: "exclude" as const,
      degradedEvidence: "penalize" as const,
    },
  };
  assert.equal(profileTitle(profile), "Balanced");
  assert.equal(profileSubtitle(profile), "policy/balanced");
  assert.deepEqual(uniqueCandidateProviders(profile), ["openai", "anthropic"]);
  assert.equal(profileMatchesQuery(profile, "claude"), true);
  assert.equal(profileMatchesQuery(profile, "gemini"), false);
  assert.equal(weightPercentLabel(0.4), "40 %");
  assert.equal(contextWindowLabel(128000, "—"), "128k");
  assert.equal(usdCostLabel(0.5, "—"), "$0.50");
  assert.equal(
    evidenceAgeLabel(7 * 86_400_000, "—", (n) => `${n} days`),
    "7 days",
  );
  assert.equal(
    optionalRequirementLabel(undefined, {
      optional: "Optional",
      required: "Required",
      off: "Off",
    }),
    "Optional",
  );

  const requirementLabels = {
    tools: "Tools",
    image: "Image",
    structured: "Structured",
    minContext: "Minimum context",
    reasoningEffort: "Reasoning effort",
    serviceTier: "Service tier",
    minQuotaHeadroom: "Quota headroom",
    localOnly: "Local only",
    remoteAllowed: "Remote allowed",
    encryptedCodexTasks: "Encrypted Codex tasks",
    optional: "Optional",
    required: "Required",
    off: "Off",
    unavailable: "—",
  };
  const requirements = overviewRequirementRows(profile, requirementLabels);
  assert.deepEqual(
    requirements.map((row) => [row.key, row.value]),
    [
      ["tools", "Required"],
      ["structuredOutput", "Off"],
      ["minContextWindow", "128k"],
      ["reasoningEffort", "High"],
      ["serviceTier", "Priority"],
      ["minQuotaHeadroom", "20%"],
      ["localOnly", "Required"],
      ["encryptedCodexTasks", "Off"],
    ],
  );
  assert.equal(
    requirements.some((row) => row.key === "imageInput" || row.key === "remoteAllowed"),
    false,
  );
  assert.deepEqual(
    overviewRequirementRows({ ...profile, require: {} }, requirementLabels),
    [],
  );

  assert.deepEqual(overviewCandidateRows(profile), [
    { index: 1, provider: "openai", model: "gpt-4o" },
    { index: 2, provider: "openai", model: "o1" },
    { index: 3, provider: "anthropic", model: "claude" },
  ]);

  const compatibilityLabels = {
    gates: "Compatibility gates",
    suites: "Suites",
    minStatus: "Min status",
    maxAge: "Max age",
    unknown: "Unknown",
    degraded: "Degraded",
    allow: "Allow",
    warn: "Warn",
    skip: "Skip",
    probed: "Probed",
    verified: "Verified",
    off: "Off",
    unavailable: "—",
    days: (n: number) => `${n} days`,
  };
  assert.deepEqual(
    overviewCompatibilityRows({ ...profile, compatibility: undefined }, compatibilityLabels),
    [{ key: "compatibility", label: "Compatibility gates", value: "Off" }],
  );
  assert.deepEqual(
    overviewCompatibilityRows(profile, compatibilityLabels).map((row) => [row.key, row.value]),
    [
      ["requiredSuites", "codex"],
      ["minStatus", "Verified"],
      ["maxEvidenceAgeMs", "7 days"],
      ["compatUnknownEvidence", "Skip"],
      ["degradedEvidence", "Warn"],
    ],
  );

  const unknown = overviewUnknownEvidenceRows(profile, {
    capability: "Capability",
    health: "Health",
    quota: "Quota",
    cost: "Cost",
    allow: "Allow",
    warn: "Warn",
    skip: "Skip",
    unavailable: "—",
  });
  assert.deepEqual(
    unknown.map((row) => [row.key, row.value]),
    [
      ["unknownCapability", "Skip"],
      ["unknownHealth", "Warn"],
      ["unknownQuota", "Warn"],
      ["unknownCost", "Allow"],
    ],
  );
});

test("overviewRequirementRows keeps configured false and zero by presence", () => {
  const requirementLabels = {
    tools: "Tools",
    image: "Image",
    structured: "Structured",
    minContext: "Minimum context",
    reasoningEffort: "Reasoning effort",
    serviceTier: "Service tier",
    minQuotaHeadroom: "Quota headroom",
    localOnly: "Local only",
    remoteAllowed: "Remote allowed",
    encryptedCodexTasks: "Encrypted Codex tasks",
    optional: "Optional",
    required: "Required",
    off: "Off",
    unavailable: "—",
  };
  const profile = {
    id: "presence-check",
    alias: null,
    icon: null,
    revision: "1",
    model: "policy/presence",
    candidates: [{ provider: "openai", model: "gpt-4o" }],
    require: {
      tools: false,
      imageInput: false,
      structuredOutput: false,
      localOnly: false,
      remoteAllowed: false,
      encryptedCodexTasks: false,
      minQuotaHeadroom: 0,
    },
    optimize: { latency: 0.25, health: 0.25, cost: 0.25, quota: 0.25 },
    limits: {},
    unknownEvidence: {
      capability: "allow" as const,
      health: "allow" as const,
      quota: "allow" as const,
      cost: "allow" as const,
    },
  };
  const rows = overviewRequirementRows(profile, requirementLabels);
  assert.deepEqual(
    rows.map((row) => [row.key, row.value]),
    [
      ["tools", "Off"],
      ["imageInput", "Off"],
      ["structuredOutput", "Off"],
      ["minQuotaHeadroom", "0%"],
      ["localOnly", "Off"],
      ["remoteAllowed", "Off"],
      ["encryptedCodexTasks", "Off"],
    ],
  );
});

test("routing profile list order is alphabetical identity, not a default marker", () => {
  const profiles = parseRoutingProfiles({
    profiles: [
      {
        id: "zeta",
        alias: null,
        icon: null,
        model: "policy/zeta",
        revision: "1",
        candidates: [{ provider: "openai", model: "gpt-4o" }],
        require: {},
        optimize: { latency: 0.55, health: 0.25, cost: 0.1, quota: 0.1 },
        limits: {},
        unknownEvidence: {
          capability: "exclude",
          health: "penalize",
          quota: "penalize",
          cost: "penalize",
        },
      },
      {
        id: "alpha",
        alias: null,
        icon: null,
        model: "policy/alpha",
        revision: "1",
        candidates: [{ provider: "openai", model: "gpt-4o" }],
        require: {},
        optimize: { latency: 0.55, health: 0.25, cost: 0.1, quota: 0.1 },
        limits: {},
        unknownEvidence: {
          capability: "exclude",
          health: "penalize",
          quota: "penalize",
          cost: "penalize",
        },
      },
    ],
  });
  // Client preserves API array order; API sorts ids alphabetically. Neither encodes default.
  assert.deepEqual(profiles.map((profile) => profile.id), ["zeta", "alpha"]);
  assert.equal("default" in (profiles[0] as object), false);
  assert.equal(profiles.every((profile) => !("isDefault" in profile)), true);
});


test("routing format helpers: rates, durations, counts, enums, eval result", () => {
  assert.equal(fmtRate(0.987, "-"), "98.7%");
  assert.equal(fmtRate(0.042, "-"), "4.2%");
  assert.equal(fmtRate(0.978, "-"), "97.8%");
  assert.equal(fmtRate(1, "-"), "100%");
  assert.equal(fmtRate(0.5, "-"), "50%");
  assert.equal(fmtRate(null, "-"), "-");
  assert.equal(fmtMs(420, "-", { ms: "ms", s: "s" }), "420ms");
  assert.equal(fmtMs(900, "-", { ms: "ms", s: "s" }), "900ms");
  assert.equal(fmtMs(1100, "-", { ms: "ms", s: "s" }), "1.1s");
  assert.equal(fmtMs(1200, "-", { ms: "ms", s: "s" }), "1.2s");
  assert.equal(fmtMs(10000, "-", { ms: "ms", s: "s" }), "10s");
  assert.equal(fmtMs(undefined, "-", { ms: "ms", s: "s" }), "-");
  assert.equal(fmtCount(12483, "-", "en-US"), "12,483");
  assert.equal(fmtCount(0, "-", "en-US"), "0");
  assert.equal(friendlyEnumLabel("high"), "High");
  assert.equal(friendlyEnumLabel("priority"), "Priority");
  assert.equal(friendlyEnumLabel(null, "-"), "-");
  assert.equal(dryRunEvalResultKind(0, 0, true), "selected");
  assert.equal(dryRunEvalResultKind(1, 0, true), "eligible");
  assert.equal(dryRunEvalResultKind(2, 0, false), "excluded");
  assert.deepEqual(
    presentScoreComponents({ latency: 0.42, health: 0.21, cost: 0.11, quota: 0.1, other: 9 }),
    [
      { key: "latency", value: 0.42 },
      { key: "health", value: 0.21 },
      { key: "cost", value: 0.11 },
      { key: "quota", value: 0.1 },
    ],
  );
  assert.deepEqual(presentScoreComponents(undefined), []);
});


test("fmtUsd keeps USD unit and three-decimal precision", () => {
  assert.equal(fmtUsd(0.018, "—"), "$0.018");
  assert.equal(fmtUsd(0.25, "—"), "$0.250");
  assert.equal(fmtUsd(0.021, "—"), "$0.021");
  assert.equal(fmtUsd(undefined, "—"), "—");
  assert.equal(fmtUsd(Number.NaN, "—"), "—");
});
test("cost incomplete note is quiet and omitted when complete", () => {
  assert.equal(
    costIncompleteNote(true, "Cost estimate incomplete"),
    "Cost estimate incomplete",
  );
  assert.equal(costIncompleteNote(false, "Cost estimate incomplete"), null);
  assert.equal(costIncompleteNote(undefined, "Cost estimate incomplete"), null);
});

test("routing analytics percentiles omit missing samples instead of inventing zeros", () => {
  assert.deepEqual(presentAnalyticsPercentiles({ sampleCount: 0, p50: 0 }), []);
  assert.deepEqual(presentAnalyticsPercentiles({ sampleCount: 12, p50: 1200, p99: 7100 }), [
    { key: "p50", value: 1200 },
    { key: "p99", value: 7100 },
  ]);
  assert.deepEqual(presentAnalyticsPercentiles({ sampleCount: 3, p50: Number.NaN, p95: 10 }), [
    { key: "p95", value: 10 },
  ]);
  const parsed = parseRoutingAnalytics({
    totalRequests: 2,
    successRate: null,
    fallbackRate: 0.5,
    confidence: "low",
    historyTruncated: false,
    cooldownTriggeringFailures: 1,
    durationMs: { sampleCount: 2, p50: 100 },
    firstOutputMs: { sampleCount: 0, coverage: null },
    breakdown: [
      { provider: "openai", model: "gpt", requests: 2, successRate: null },
    ],
  });
  assert.equal(parsed?.successRate, null);
  assert.equal(parsed?.durationMs.p95, undefined);
  assert.equal(parsed?.breakdown[0]?.p50DurationMs, undefined);
  assert.equal(parsed?.historyTruncated, false);
});

test("models catalog motion keeps modal overlay as CSS transitions", () => {
  const css = fs.readFileSync(
    path.join(guiRoot, "src", "styles-models-workspace.css"),
    "utf8",
  );
  const shell = fs.readFileSync(
    path.join(guiRoot, "src", "pages", "models-modal-shell.tsx"),
    "utf8",
  );
  const modals = fs.readFileSync(
    path.join(guiRoot, "src", "pages", "models-catalog-modals.tsx"),
    "utf8",
  );
  assert.match(css, /transition-behavior:\s*allow-discrete/);
  assert.match(css, /scale\(0\.97\)/);
  assert.match(css, /@starting-style/);
  assert.doesNotMatch(css, /scale\(0\)/);
  assert.doesNotMatch(css, /models-catalog-overflow/);
  assert.match(shell, /data-open=\{open \? "true" : "false"\}/);
  assert.match(modals, /ModelsModalShell/);
  assert.doesNotMatch(modals, /if \(!open\) return null/);
});

test("models catalog settings are flat under the toolbar without overflow chrome", () => {
  const css = fs.readFileSync(
    path.join(guiRoot, "src", "styles-models-workspace.css"),
    "utf8",
  );
  const panel = fs.readFileSync(
    path.join(guiRoot, "src", "pages", "models-catalog-panel.tsx"),
    "utf8",
  );
  const toolbar = fs.readFileSync(
    path.join(guiRoot, "src", "pages", "models-catalog-controls.tsx"),
    "utf8",
  );
  const settings = fs.readFileSync(
    path.join(guiRoot, "src", "pages", "models-catalog-settings.tsx"),
    "utf8",
  );
  assert.match(
    css,
    /\.models-catalog-settings\s*\{[^}]*border-bottom:\s*1px solid var\(--border\)/,
  );
  assert.doesNotMatch(css, /\.models-catalog-settings\s*\{[^}]*background:/);
  assert.doesNotMatch(
    toolbar,
    /models-catalog-overflow|IconMore|models\.advanced/,
  );
  assert.match(panel, /\{settings\}/);
  assert.match(settings, /models\.settings\.newModels/);
  assert.match(settings, /models\.settings\.defaultContextCap/);
  assert.match(settings, /models\.settings\.applyExisting/);
});

test("models change sync debounce coalesces bursts into one trailing run", () => {
  assert.equal(MODELS_CHANGE_SYNC_DEBOUNCE_MS, 5_000);
  const modelsPage = fs.readFileSync(
    path.join(guiRoot, "src", "pages", "Models.tsx"),
    "utf8",
  );
  assert.match(
    modelsPage,
    /MODELS_CHANGE_SYNC_DEBOUNCE_MS|useModelsChangeWrites/,
  );
  assert.match(modelsPage, /useModelsChangeWrites/);
  assert.match(
    fs.readFileSync(
      path.join(guiRoot, "src", "pages", "use-models-change-writes.ts"),
      "utf8",
    ),
    /createTrailingDebounce|scheduleCatalogSync/,
  );

  let now = 0;
  /** @type {Array<{ id: number, at: number, fn: () => void }>} */
  const timers = [];
  let nextId = 1;
  const debounce = createTrailingDebounce(5_000, {
    setTimeout(fn, ms) {
      const id = nextId++;
      timers.push({ id, at: now + ms, fn: /** @type {() => void} */ (fn) });
      return /** @type {ReturnType<typeof setTimeout>} */ (
        /** @type {unknown} */ (id)
      );
    },
    clearTimeout(id) {
      const index = timers.findIndex((row) => row.id === id);
      if (index >= 0) timers.splice(index, 1);
    },
  });

  /** @type {number[]} */
  const runs = [];
  debounce.schedule(() => runs.push(1));
  now = 2_000;
  debounce.schedule(() => runs.push(2));
  now = 4_000;
  debounce.schedule(() => runs.push(3));
  assert.equal(runs.length, 0);
  assert.equal(debounce.pending(), true);

  now = 9_000;
  for (const row of [...timers]) {
    if (row.at <= now) {
      const index = timers.indexOf(row);
      if (index >= 0) timers.splice(index, 1);
      row.fn();
    }
  }
  assert.deepEqual(runs, [3]);
  assert.equal(debounce.pending(), false);
});


test("cws.legacy.* keys are translated in non-en catalogs (no wholesale English fallback)", () => {
  const keys = [
    "cws.legacy.blurb",
    "cws.legacy.storedStrategy",
    "cws.legacy.runtimeBehaviour",
    "cws.legacy.notExecutable",
    "cws.legacy.weights",
    "cws.legacy.imageDisabled",
    "cws.legacy.editRoundRobin",
  ] as const satisfies readonly TKey[];
  const locales: Locale[] = ["de", "fr", "ja", "ko", "ru", "tr", "zh", "zh-TW"];
  for (const locale of locales) {
    const identical = keys.filter((key) => catalogValue(locale, key) === catalogValue("en", key));
    assert.equal(
      identical.length,
      0,
      `${locale} still English-identical for: ${identical.join(", ")}`,
    );
  }
});

test("combo overview shows sparse legacy weights without inventing 1", () => {
  const overview = fs.readFileSync(
    path.join(guiRoot, "src", "components", "combo-workspace-selected-overview.tsx"),
    "utf8",
  );
  assert.match(overview, /formatLegacySparseWeights/);
  assert.doesNotMatch(overview, /weight \?\? 1/);
});

test("combo detail baseline sync preserves dirty drafts explicitly", () => {
  const panel = fs.readFileSync(
    path.join(guiRoot, "src", "components", "combo-workspace-detail-panel.tsx"),
    "utf8",
  );
  const data = fs.readFileSync(
    path.join(guiRoot, "src", "combo-workspace-data.ts"),
    "utf8",
  );
  assert.match(data, /shouldAdoptComboBaselineOnSync/);
  assert.match(panel, /shouldAdoptComboBaselineOnSync/);
  assert.match(panel, /dirtyRef/);
  // Must not clear dirty merely because the baseline sync key changed.
  assert.doesNotMatch(
    panel,
    /useEffect\([\s\S]*?onDirtyChange\(false\)[\s\S]*?\}, \[syncKey\]\)/,
  );
  // Explicit dirty state — not only draftEquals(draft, baseline) for Save/nav.
  assert.match(panel, /const \[dirty, setDirty\] = useState\(false\)/);
});
test("combo provider picker drops disabled and hidden rows without reordering the input", () => {
  const providers = [
    { name: "zeta", disabled: true },
    { name: "beta", hiddenFromPicker: true },
    { name: "gamma" },
    { name: "alpha" },
  ];
  assert.deepEqual(comboPickerProviders(providers).map((row) => row.name), ["alpha", "gamma"]);
  assert.deepEqual(providers.map((row) => row.name), ["zeta", "beta", "gamma", "alpha"]);
});

test("combo provider picker keeps the row a target already stores", () => {
  const providers = [
    { name: "alpha" },
    { name: "legacy", disabled: true },
    { name: "shadow", hiddenFromPicker: true },
  ];
  const names = (current: string) => comboProviderChoices(providers, current).map((row) => row.name);
  assert.deepEqual(names("alpha"), ["alpha"]);
  assert.deepEqual(names("legacy"), ["alpha", "legacy"]);
  assert.deepEqual(names("shadow"), ["alpha", "shadow"]);
  assert.deepEqual(names("missing"), ["alpha"]);
  assert.deepEqual(names(""), ["alpha"]);
});

test("combo model ids match the provider exactly and drop blank or duplicate ids", () => {
  const models = [
    { provider: "openai", id: "gpt-b" },
    { provider: "openai", id: "gpt-a" },
    { provider: "openai", id: "gpt-a" },
    { provider: "openai", id: "" },
    { provider: "anthropic", id: "claude" },
  ];
  assert.deepEqual(comboModelIds(models, "openai", []), ["gpt-a", "gpt-b"]);
  assert.deepEqual(comboModelIds(models, "anthropic", []), ["claude"]);
  assert.deepEqual(comboModelIds(models, "", []), []);
  assert.deepEqual(comboModelIds(models, "missing", []), []);
});

test("ChatGPT forward rows reuse the openai catalog only for accepted provider ids", () => {
  const models = [{ provider: "openai", id: "gpt-5" }];
  const forward = {
    name: "chatgpt",
    authMode: "forward",
    adapter: "openai-responses",
    baseUrl: "https://chatgpt.com/backend-api/codex/",
  };
  // The chatgpt passthrough preset publishes no catalog of its own.
  assert.deepEqual(comboModelIds(models, "chatgpt", [forward]), ["gpt-5"]);
  assert.deepEqual(comboModelIds(models, "ChatGPT", [{ name: "ChatGPT" }]), ["gpt-5"]);
  // A capitalised OpenAI id needs the full forward metadata to reach the lowercase rows.
  assert.deepEqual(
    comboModelIds(models, "OpenAI", [{ name: "OpenAI", authMode: "forward", adapter: "openai-responses" }]),
    ["gpt-5"],
  );
  assert.deepEqual(comboModelIds(models, "OpenAI", [{ name: "OpenAI" }]), []);
  assert.deepEqual(
    comboModelIds(models, "OpenAI", [{ name: "OpenAI", authMode: "forward", adapter: "openai-responses", baseUrl: "https://api.openai.com/v1" }]),
    [],
  );
  assert.deepEqual(
    comboModelIds(models, "OpenAI", [{ name: "OpenAI", authMode: "oauth", adapter: "openai-responses" }]),
    [],
  );
  // No other provider id ever borrows the openai catalog, whatever its metadata.
  assert.deepEqual(
    comboModelIds(models, "acme", [{ name: "acme", authMode: "forward", adapter: "openai-responses" }]),
    [],
  );
});

test("combo model choices keep a stored id the live catalog no longer lists", () => {
  const models = [{ provider: "openai", id: "gpt-a" }];
  const stale = newComboTarget({ provider: "openai", model: "retired" });
  assert.deepEqual(comboModelChoices(models, stale, []), ["retired", "gpt-a"]);
  assert.deepEqual(
    comboModelChoices(models, newComboTarget({ provider: "openai", model: "gpt-a" }), []),
    ["gpt-a"],
  );
  assert.deepEqual(comboModelChoices(models, newComboTarget(), []), []);
});

test("combo first model for a provider is the alphabetical head", () => {
  const models = [
    { provider: "openai", id: "gpt-z" },
    { provider: "openai", id: "gpt-a" },
  ];
  assert.equal(comboFirstModelId(models, "openai", []), "gpt-a");
  assert.equal(comboFirstModelId(models, "missing", []), "");
});

test("combo target reorder and removal no-op by reference and never empty the list", () => {
  const targets = [
    newComboTarget({ provider: "a", model: "one" }),
    newComboTarget({ provider: "b", model: "two" }),
    newComboTarget({ provider: "c", model: "three" }),
  ];
  assert.deepEqual(comboMoveTarget(targets, 0, 2).map((row) => row.provider), ["b", "c", "a"]);
  assert.deepEqual(comboMoveTarget(targets, 2, 0).map((row) => row.provider), ["c", "a", "b"]);
  assert.equal(comboMoveTarget(targets, 1, 1), targets);
  assert.equal(comboMoveTarget(targets, -1, 1), targets);
  assert.equal(comboMoveTarget(targets, 0, 3), targets);
  assert.deepEqual(comboRemoveTarget(targets, 1).map((row) => row.provider), ["a", "c"]);
  assert.equal(comboRemoveTarget(targets, 5), targets);
  const single = [newComboTarget({ provider: "a", model: "one" })];
  assert.equal(comboRemoveTarget(single, 0), single);
  assert.deepEqual(targets.map((row) => row.provider), ["a", "b", "c"]);
});

test("combo target patch edits one row and ignores an out-of-range index", () => {
  const targets = [
    newComboTarget({ provider: "a", model: "one" }),
    newComboTarget({ provider: "b", model: "two" }),
  ];
  assert.deepEqual(comboPatchTarget(targets, 1, { model: "changed" }).map((row) => row.model), ["one", "changed"]);
  assert.deepEqual(targets.map((row) => row.model), ["one", "two"]);
  assert.equal(comboPatchTarget(targets, 2, { model: "x" }), targets);
});


test("combo target rows resolve pickers, placeholders, and reorder affordances", () => {
  const providers = [{ name: "alpha" }, { name: "legacy", disabled: true }];
  const models = [
    { provider: "alpha", id: "m1" },
    { provider: "alpha", id: "m2" },
  ];
  const targets = [
    newComboTarget({ provider: "alpha", model: "m2", clientKey: "k1" }),
    { provider: "alpha", model: "gone" },
    newComboTarget(),
  ];
  const rows = comboTargetRows(targets, providers, models);
  assert.deepEqual(rows.map((row) => row.index), [0, 1, 2]);
  assert.equal(rows[0]?.key, "k1");
  assert.equal(rows[1]?.key, "alpha:gone");
  assert.deepEqual(rows[0]?.modelChoices, ["m1", "m2"]);
  assert.deepEqual(rows[1]?.modelChoices, ["gone", "m1", "m2"]);
  assert.equal(rows[0]?.modelPlaceholder, "cws.target.pickModel");
  assert.equal(rows[2]?.modelPlaceholder, "cws.target.pickProviderFirst");
  // Disabled providers stay out of ordinary choices.
  assert.deepEqual(rows[0]?.providerChoices.map((row) => row.name), ["alpha"]);
  assert.deepEqual(rows[2]?.providerChoices.map((row) => row.name), ["alpha"]);
  assert.deepEqual(rows.map((row) => row.canMoveUp), [false, true, true]);
  assert.deepEqual(rows.map((row) => row.canMoveDown), [true, true, false]);
  assert.deepEqual(rows.map((row) => row.canRemove), [true, true, true]);
  const storedDisabled = comboTargetRows(
    [newComboTarget({ provider: "legacy", model: "" })],
    providers,
    models,
  );
  assert.deepEqual(storedDisabled[0]?.providerChoices.map((row) => row.name), ["alpha", "legacy"]);
  assert.equal(storedDisabled[0]?.modelPlaceholder, "cws.target.noModels");
  assert.deepEqual(comboTargetRows([targets[0]!], providers, models).map((row) => row.canRemove), [false]);
});

test("combo target row classes keep the failover grid and drag affordances", () => {
  assert.equal(comboTargetRowClass(false, false), "cwi-target-row cwi-target-row--failover");
  assert.equal(
    comboTargetRowClass(true, false),
    "cwi-target-row cwi-target-row--failover cwi-target-row--dragging",
  );
  assert.equal(
    comboTargetRowClass(false, true),
    "cwi-target-row cwi-target-row--failover cwi-target-row--drop",
  );
});

test("combos overview copy offers Create only on first run", () => {
  assert.deepEqual(combosOverviewCopy(true), {
    title: "cws.selectTitle",
    body: "cws.selectBody",
    footer: null,
  });
  assert.deepEqual(combosOverviewCopy(false), {
    title: "cws.emptyTitle",
    body: "cws.emptyBody",
    footer: "cws.emptyFooter",
  });
});

test("combo target editor keeps keyboard reorder and has no legacy weight field", () => {
  const source = fs.readFileSync(
    path.join(guiRoot, "src", "components", "combo-workspace-controls.tsx"),
    "utf8",
  );
  assert.match(source, /cws\.target\.moveUp/);
  assert.match(source, /cws\.target\.moveDown/);
  assert.match(source, /cws\.target\.drag/);
  assert.match(source, /disabled=\{!row\.canRemove\}/);
  assert.doesNotMatch(source, /type="number"/);
  assert.doesNotMatch(source, /cws\.target\.weight|clampedNumberInput/);
});

test("combo dialogs keep native modal semantics with one commit path", () => {
  const source = fs.readFileSync(
    path.join(guiRoot, "src", "components", "combo-workspace-dialogs.tsx"),
    "utf8",
  );
  assert.match(source, /<dialog/);
  assert.match(source, /showModal\(\)/);
  assert.match(source, /onCancel=\{handleCancel\}/);
  assert.match(source, /event\.preventDefault\(\)/);
  assert.match(source, /aria-labelledby=\{titleId\}/);
  assert.match(source, /className="modal-backdrop-dismiss"/);
  assert.match(source, /onClick=\{onDismiss\}/);
  assert.match(source, /cwi-unsaved-keep/);
  assert.match(source, /cwi-unsaved-discard/);
});

test("combo overview pane stays quiet without a KPI board", () => {
  const source = fs.readFileSync(
    path.join(guiRoot, "src", "components", "combo-workspace-overview-panel.tsx"),
    "utf8",
  );
  assert.match(source, /combos-workspace-quiet/);
  assert.match(source, /cws\.create/);
  assert.doesNotMatch(source, /cwi-attention-row/);
  assert.doesNotMatch(source, /[Kk]pi/);
});

test("provider card state owns the card's rows, context ladder, and bulk policy", async () => {
  const {
    modelsProviderCardState,
    providerCapDisplayValue,
    providerCardRows,
    widestAdvertisedWindow,
  } = await import("../src/pages/models-provider-group-data.ts");

  const rows = [
    { provider: "openai", id: "gpt-5.4", namespaced: "openai/gpt-5.4", native: false },
    { provider: "openai", id: "gpt-5.4-mini", namespaced: "openai/gpt-5.4-mini", native: false, contextWindow: 400_000 },
    { provider: "openai", id: "stopped", namespaced: "openai/stopped", native: false },
  ];

  assert.equal(widestAdvertisedWindow(rows), 400_000);
  assert.equal(widestAdvertisedWindow([{ id: "x", namespaced: "openai/x", provider: "openai" }]), undefined);
  assert.equal(
    providerCapDisplayValue({ capOn: true, providerCap: 64_000, nativeProviderGroup: false, rows }),
    64_000,
    "a dialled-in cap is what the next enable uses",
  );
  assert.equal(
    providerCapDisplayValue({ capOn: false, providerCap: 64_000, nativeProviderGroup: true, rows }),
    1_050_000,
    "the native GPT-5.6 group offers its own default window",
  );
  assert.equal(
    providerCapDisplayValue({ capOn: false, providerCap: 64_000, nativeProviderGroup: false, rows }),
    400_000,
    "other groups offer the widest window their rows advertise",
  );

  assert.deepEqual(
    providerCardRows({ rows, query: "MINI", pageSize: 1, isHidden: model => model.id === "stopped" }),
    { shown: [rows[1]], matched: 1, remaining: 0 },
  );
  const paged = providerCardRows({ rows, query: "", pageSize: 2, isHidden: model => model.id === "stopped" });
  assert.deepEqual(paged.shown.map(model => model.id), ["gpt-5.4", "gpt-5.4-mini"]);
  assert.equal(paged.matched, 3);
  assert.equal(paged.remaining, 1);

  const group = {
    provider: "openai",
    rows,
    native: false,
    nativeProviderGroup: false,
    liveModels: true,
    catalogProbe: true,
    configuredModels: [],
    discovery: { status: "failed", reason: "provider" },
  };
  const input = {
    collapsed: new Set(["openai"]),
    selectedModelMap: {},
    disabled: new Set(["openai/stopped"]),
    contextCaps: {},
    contextCapValue: 128_000,
    search: {},
    limit: {},
  };

  const card = modelsProviderCardState(group, input);
  assert.deepEqual(card.visible.map(model => model.id), ["gpt-5.4", "gpt-5.4-mini", "stopped"]);
  assert.equal(card.activeCount, 2);
  assert.equal(card.hasRows, true);
  assert.equal(card.allOn, false);
  assert.equal(card.allOff, false);
  assert.equal(card.isCollapsed, true);
  assert.equal(card.shown, 60);
  assert.equal(card.remaining, 0);
  assert.equal(card.capOn, false);
  assert.equal(card.capDisplayValue, 400_000);
  assert.equal(card.capOptionSet.has(100_000), true);
  assert.equal(card.discoveryFailure?.status, "failed");

  const capped = modelsProviderCardState(group, { ...input, contextCaps: { openai: 32_000 } });
  assert.equal(capped.capOn, true);
  assert.equal(capped.providerCap, 32_000);
  assert.equal(capped.capDisplayValue, 32_000);

  const native = modelsProviderCardState({ ...group, nativeProviderGroup: true }, input);
  assert.equal(native.capOptionSet.has(1_050_000), true);
  assert.equal(native.capDisplayValue, 1_050_000);

  const emptied = modelsProviderCardState({ ...group, rows: [] }, input);
  assert.equal(emptied.hasRows, false);
  assert.equal(emptied.allOn, true);
  assert.equal(emptied.allOff, true);

  const quiet = modelsProviderCardState({ ...group, liveModels: false }, input);
  assert.equal(quiet.discoveryFailure, undefined);

  const limited = modelsProviderCardState(group, { ...input, limit: { openai: 1 } });
  assert.deepEqual(limited.visible.map(model => model.id), ["gpt-5.4"]);
  assert.equal(limited.shown, 1);
  assert.equal(limited.remaining, 2);
});
