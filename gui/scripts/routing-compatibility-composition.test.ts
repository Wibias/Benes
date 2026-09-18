/**
 * Routing and Compatibility composition (#288).
 *
 * Behavioural pins for the decisions this child moved out of its pages and into owners: the
 * tab registry's movement rule, the Lab catalog read, what one read of the profile sources
 * leaves behind, and the names the Compatibility surface prints. Nothing here asserts how a
 * file is written; the one ownership fact checked at source level is that the retired label
 * maps are gone.
 */
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { translate } from "../src/i18n/catalogs.ts";
import { readLabCatalog } from "../src/lab/lab-catalog.ts";
import { EVIDENCE_LAYERS, PRODUCT_EVIDENCE_LAYERS } from "../src/lab/evidence-vocabulary.ts";
import { LAYER_TEXT, verdictText } from "../src/pages/compatibility-matrix-labels.ts";
import { filtersAreActive, labFailureMessage } from "../src/pages/compatibility-matrix-view.ts";
import { nextRoutingTab, ROUTING_TABS } from "../src/pages/routing-tab.ts";
import {
  draftCoercedToModels,
  dryRunSurvivesReload,
  editorAfterLoad,
  routingProfilesData,
  EMPTY_ROUTING_EDITOR,
  type RoutingProfileSources,
} from "../src/pages/routing-profiles-load.ts";
import { newRoutingProfileDraft, type ModelOption } from "../src/routing-profile/profile-model.ts";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

test("the routing tabs move by the registry's order, wrapping at both ends", () => {
  assert.deepEqual([...ROUTING_TABS], ["profiles", "compatibility", "combos"]);
  assert.equal(nextRoutingTab("profiles", "ArrowLeft"), "combos");
  assert.equal(nextRoutingTab("combos", "ArrowRight"), "profiles");
  assert.equal(nextRoutingTab("profiles", "ArrowRight"), "compatibility");
  assert.equal(nextRoutingTab("combos", "Home"), "profiles");
  assert.equal(nextRoutingTab("profiles", "End"), "combos");
  // Anything the tabs pattern does not move on is not this strip's key to consume.
  assert.equal(nextRoutingTab("profiles", "Enter"), null);
  assert.equal(nextRoutingTab("profiles", "Tab"), null);
  assert.equal(nextRoutingTab("profiles", "ArrowUp"), null);
});

test("the Lab catalog keeps one row per layer and suite, in layer order", () => {
  const catalog = readLabCatalog({
    scenarios: [
      { suiteId: "beta", evidenceLayer: "protocol_conformance" },
      { suiteId: "alpha", evidenceLayer: "live_route_compatibility" },
      // A repeat of the first row is the same suite, not a second one.
      { suiteId: "beta", evidenceLayer: "protocol_conformance" },
      // A layer Benes does not produce, a blank suite id, and a non-object are not suites.
      { suiteId: "gamma", evidenceLayer: "task_effectiveness" },
      { suiteId: "   ", evidenceLayer: "protocol_conformance" },
      "suite",
    ],
  });
  assert.deepEqual(catalog?.map(suite => suite.key), [
    "live_route_compatibility:alpha",
    "protocol_conformance:beta",
  ]);
  assert.deepEqual(catalog?.map(suite => suite.evidenceLayer), [
    "live_route_compatibility",
    "protocol_conformance",
  ]);
});

test("a catalog the Lab answered badly is not an empty catalog", () => {
  assert.equal(readLabCatalog(null), null);
  assert.equal(readLabCatalog([]), null);
  assert.equal(readLabCatalog({ scenarios: "no" }), null);
  assert.equal(readLabCatalog({}), null);
  assert.deepEqual(readLabCatalog({ scenarios: [] }), []);
});

function sources(overrides: Partial<RoutingProfileSources> = {}): RoutingProfileSources {
  return {
    profiles: [],
    analytics: null,
    providers: {},
    modelsPayload: [],
    catalogSuites: [],
    ...overrides,
  };
}

test("one read leaves the editor's inputs as its sources describe them", () => {
  const data = routingProfilesData(sources({
    providers: {
      zeta: { models: ["z1"] },
      alpha: { disabled: true, models: ["a1"] },
      beta: { defaultModel: "b1", models: [{ id: "b2" }] },
    },
    modelsPayload: [{ provider: "zeta", id: "zz" }],
    catalogSuites: [],
  }));
  // A disabled provider is not an input; the rest are read in name order.
  assert.deepEqual(data.providerNames, ["beta", "zeta"]);
  assert.deepEqual(data.models.map(model => `${model.provider}/${model.id}`), [
    "beta/b2",
    "zeta/z1",
    "zeta/zz",
  ]);
  // An announced default model is not a candidate: only the catalogs offer those.
  assert.equal(data.models.some(model => model.id === "b1"), false);
  assert.equal(data.catalogError, false);
  // A catalog the Lab answered badly is reported, not silently treated as empty.
  assert.equal(routingProfilesData(sources({ catalogSuites: null })).catalogError, true);
  assert.deepEqual(routingProfilesData(sources({ catalogSuites: null })).catalogSuites, []);
});

test("an open draft survives a background reload and is replaced by a settled one", () => {
  const models: ModelOption[] = [{ provider: "openai", id: "gpt-5.4" }];
  const open = { ...EMPTY_ROUTING_EDITOR, draft: newRoutingProfileDraft("openai", "gpt-5.4"), editing: true };
  const kept = editorAfterLoad({
    editor: open,
    models,
    settledOn: null,
    replacesEditor: false,
  });
  assert.equal(kept.editing, true);
  assert.equal(kept.draft, open.draft);
  assert.equal(kept.selected, null);

  const profile = {
    id: "p1",
    alias: null,
    icon: null,
    model: "openai/gpt-5.4",
    revision: "r1",
    candidates: [{ provider: "openai", model: "gpt-5.4" }],
    require: {},
    optimize: { latency: 1, health: 0, cost: 0, quota: 0 },
    limits: {},
    unknownEvidence: { capability: "exclude", health: "penalize", quota: "penalize", cost: "penalize" },
  } as never;
  const settled = editorAfterLoad({
    editor: open,
    models,
    settledOn: profile,
    replacesEditor: true,
  });
  assert.equal(settled.editing, false);
  assert.equal(settled.selected, profile);
  assert.deepEqual(settled.draft?.candidates.map(candidate => candidate.model), ["gpt-5.4"]);
});

test("a candidate the catalog no longer offers is moved, and a dead provider is left alone", () => {
  const draft = newRoutingProfileDraft("openai", "gone");
  draft.candidates = [
    { ...draft.candidates[0]!, provider: "openai", model: "gone" },
    { ...draft.candidates[0]!, provider: "dead", model: "gone" },
  ];
  const coerced = draftCoercedToModels(draft, [
    { provider: "openai", id: "beta" },
    { provider: "openai", id: "alpha" },
  ]);
  assert.deepEqual(coerced?.candidates.map(candidate => candidate.model), ["alpha", "gone"]);
  // Nothing to choose from is not a reason to rewrite the profile.
  assert.equal(draftCoercedToModels(draft, []), draft);
});

test("a dry run stops describing a profile whose revision moved on", () => {
  assert.equal(dryRunSurvivesReload({ id: "p1", revision: "r1" }, { id: "p1", revision: "r1" }), true);
  assert.equal(dryRunSurvivesReload({ id: "p1", revision: "r1" }, { id: "p1", revision: "r2" }), false);
  assert.equal(dryRunSurvivesReload({ id: "p1", revision: "r1" }, { id: "p2", revision: "r1" }), false);
  assert.equal(dryRunSurvivesReload(null, { id: "p1", revision: "r1" }), false);
  assert.equal(dryRunSurvivesReload({ id: "p1", revision: "r1" }, null), false);
});

test("the filter bar says whether it is narrowing the board", () => {
  const none = { layer: "" as const, verdict: "" as const, subjectQuery: "", suiteId: "" };
  assert.equal(filtersAreActive(none), false);
  assert.equal(filtersAreActive({ ...none, subjectQuery: "   " }), false);
  assert.equal(filtersAreActive({ ...none, subjectQuery: "gpt-4" }), true);
  assert.equal(filtersAreActive({ ...none, layer: "protocol_conformance" }), true);
  assert.equal(filtersAreActive({ ...none, verdict: "VERIFIED" }), true);
  assert.equal(filtersAreActive({ ...none, suiteId: "s1" }), true);
});

test("a transport failure reads as the page's own sentence, and a refusal does not", () => {
  const own = "Could not load compatibility data";
  assert.equal(labFailureMessage(new Error("Failed to fetch"), own), own);
  assert.equal(labFailureMessage(new Error("NetworkError when attempting to fetch"), own), own);
  assert.equal(labFailureMessage(new Error("network error"), own), own);
  assert.equal(labFailureMessage(new Error("   "), own), own);
  assert.equal(labFailureMessage("not an error", own), own);
  // What the listener said about its own refusal is more use than the page's guess.
  assert.equal(labFailureMessage(new Error("lab projection is not available"), own), "lab projection is not available");
});

test("every layer and verdict is named from the owner's own vocabulary", () => {
  assert.deepEqual(Object.keys(LAYER_TEXT).sort(), [...EVIDENCE_LAYERS].sort());
  for (const layer of PRODUCT_EVIDENCE_LAYERS) {
    const text = LAYER_TEXT[layer];
    assert.notEqual(translate("en", text.name), text.name);
    assert.notEqual(translate("en", text.column), text.column);
  }
  // The schema's own external layer is nameable too, even though no board offers it.
  assert.notEqual(translate("en", LAYER_TEXT.task_effectiveness.name), LAYER_TEXT.task_effectiveness.name);
  for (const verdict of ["UNKNOWN", "CLAIMED", "PROBED", "VERIFIED", "DEGRADED", "BLOCKED", "UNSUPPORTED"] as const) {
    const key = verdictText(verdict);
    assert.notEqual(translate("en", key), key, verdict);
  }
});

test("the retired label maps are gone and nothing reads them", () => {
  const labels = readFileSync(path.join(guiRoot, "src", "pages", "compatibility-matrix-labels.ts"), "utf8");
  for (const retired of ["LAYER_LABEL", "LAYER_COLUMN", "VERDICT_LABEL", "ARTIFACT_STATUS_LABEL"]) {
    assert.equal(labels.includes(retired), false, retired);
  }
});
