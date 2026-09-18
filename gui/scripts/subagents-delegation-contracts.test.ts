import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  ULTRA_MODE_PRESET,
  deriveUltraModeState,
  normalizeMultiAgentMode,
  SUBAGENTS_TOAST_ERR_MS,
  SUBAGENTS_TOAST_OK_MS,
  ultraModeHintActive,
} from "../src/pages/subagents-delegation-contract.ts";
import { ultraSaveToastKey } from "../src/pages/subagents-ultra-mode.ts";
import { FEATURED_MAX } from "../src/components/subagents-workspace/roster.ts";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

test("FEATURED_MAX clamp keeps roster writes bounded", () => {
  const models = Array.from({ length: FEATURED_MAX + 3 }, (_, i) => `m/${i}`);
  assert.equal(models.slice(0, FEATURED_MAX).length, FEATURED_MAX);
});

test("effective Ultra V2 requires enabled && mode v2", () => {
  assert.equal(deriveUltraModeState({ enabled: true, multiAgentMode: "v2" }).multiAgentV2Enabled, true);
  assert.equal(deriveUltraModeState({ enabled: true, multiAgentMode: "default" }).multiAgentV2Enabled, false);
  assert.equal(deriveUltraModeState({ enabled: false, multiAgentMode: "v2" }).multiAgentV2Enabled, false);
  assert.equal(normalizeMultiAgentMode("v1"), "v1");
  assert.equal(normalizeMultiAgentMode("nope"), "default");
});

test("ULTRA_MODE_PRESET stays exact and hint activity is trim-based", () => {
  assert.match(ULTRA_MODE_PRESET, /Proactive multi-agent delegation is active/);
  assert.equal(ultraModeHintActive("  hi  "), true);
  assert.equal(ultraModeHintActive("   "), false);
  assert.equal(ultraModeHintActive(null), false);
});

test("ultra save toast key distinguishes mode vs hint edits", () => {
  assert.equal(ultraSaveToastKey({ multiAgentMode: "v2" }), "models.v2Applied");
  assert.equal(ultraSaveToastKey({ multiAgentModeHintText: "x" }), "sub.ultraModeSaved");
});

test("toast timings stay 4500 ok / 8000 err", () => {
  assert.equal(SUBAGENTS_TOAST_OK_MS, 4500);
  assert.equal(SUBAGENTS_TOAST_ERR_MS, 8000);
});

test("chosen persist generation race is pinned in source", () => {
  const src = readFileSync(path.join(guiRoot, "src/pages/subagents-chosen-persist.ts"), "utf8");
  assert.match(src, /bumpGeneration/);
  assert.match(src, /getGeneration\(\)/);
  assert.match(src, /gen !== options\.hooks\.getGeneration\(\)/);
  assert.match(src, /FEATURED_MAX/);
});

test("hook wires createDelegationMutationController as sole production mutation authority", () => {
  // Module-boundary check only — mutation semantics live in subagents-delegation-mutations.test.ts.
  const hook = readFileSync(path.join(guiRoot, "src/pages/use-subagent-delegation.ts"), "utf8");
  const mutations = readFileSync(path.join(guiRoot, "src/pages/subagents-delegation-mutations.ts"), "utf8");
  assert.match(hook, /createDelegationMutationController/);
  assert.match(hook, /from \"\.\/subagents-delegation-mutations\.ts\"/);
  assert.match(hook, /createProductionDelegationTransport|DelegationMutationTransport/);
  assert.match(hook, /setClientResourceData/);
  assert.match(hook, /loadDelegationSnapshot/);
  assert.equal(hook.includes("for (;;)"), false, "hook must not own the mutation loop");
  assert.equal(mutations.includes("for (;;)"), true, "controller owns the coalescing loop");
  assert.equal(mutations.includes("fetch("), false, "controller stays transport-DI pure");
  assert.match(mutations, /export function createDelegationMutationController/);
  assert.match(mutations, /DelegationMutationTransport/);
});

test("Subagents board keeps DataSurface cold/stale and refresh-all wiring", () => {
  const board = readFileSync(path.join(guiRoot, "src/pages/subagents-board.tsx"), "utf8");
  const models = readFileSync(path.join(guiRoot, "src/pages/use-subagents-models-resource.ts"), "utf8");
  assert.match(models, /useDataSurface/);
  assert.match(board, /DataSurfaceSkeleton/);
  assert.match(board, /delegation\.reload/);
  assert.match(board, /loadUltraMode/);
  assert.match(board, /showSkeleton/);
  assert.match(board, /failed-cold/);
  assert.equal(board.includes("window.location"), false);
});

test("presentation keeps Select from ui and CSS class contracts", () => {
  const slices = readFileSync(path.join(guiRoot, "src/components/subagents-workspace/delegation-slices.tsx"), "utf8");
  const panel = readFileSync(path.join(guiRoot, "src/components/subagents-workspace/subagents-delegation-panel.tsx"), "utf8");
  const agent = readFileSync(path.join(guiRoot, "src/components/subagents-workspace/SubagentsAgentInterface.tsx"), "utf8");
  assert.match(slices, /from \"\.\.\/\.\.\/ui\"/);
  assert.match(slices, /swi-delegation-row/);
  assert.match(slices, /swi-ultra-mode-textarea/);
  assert.match(panel, /swi-delegation/);
  assert.match(agent, /subagents-agent-interface/);
  assert.match(agent, /role=\"radiogroup\"/);
  assert.match(agent, /models\.v2Mode_/);
});

test("historical Subagents and DelegationSection facades stay thin re-exports", () => {
  const page = readFileSync(path.join(guiRoot, "src/pages/Subagents.tsx"), "utf8");
  const section = readFileSync(path.join(guiRoot, "src/components/subagents-workspace/SubagentDelegationSection.tsx"), "utf8");
  assert.match(page, /SubagentsBoard as default/);
  assert.match(section, /SubagentsDelegationPanel as default/);
  assert.match(section, /ULTRA_MODE_PRESET/);
});
