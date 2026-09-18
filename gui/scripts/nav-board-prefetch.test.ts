import assert from "node:assert/strict";
import test from "node:test";

import { mergeDataSurfaceSeed } from "../src/data-surface-seed.ts";
import {
  combosWorkspaceCacheKey,
  harnessBoardResourceKey,
  harnessBoardSessionKey,
  labMatrixResourceKey,
  fabricTasksResourceKey,
  paintedGuidanceEnabled,
  providersConfigCacheKey,
  providersWorkspaceCacheKey,
  subagentsDelegationSessionKey,
  subagentsModelsResourceKey,
} from "../src/nav-board-resources.ts";

test("session cache wins over a dummy initial board so a revisit does not overlay empty live state", () => {
  const cached = [{ id: "codex", installed: true, applied: true }];
  const emptyOverlay = [{ id: "codex", installed: false, applied: false }];
  assert.equal(mergeDataSurfaceSeed(cached, emptyOverlay), cached);
  assert.equal(mergeDataSurfaceSeed(undefined, emptyOverlay), emptyOverlay);
  assert.equal(mergeDataSurfaceSeed(undefined, undefined), undefined);
});

test("guidance paints from the last known value, not the optimistic on default", () => {
  assert.equal(paintedGuidanceEnabled(undefined), false);
  assert.equal(paintedGuidanceEnabled(false), false);
  assert.equal(paintedGuidanceEnabled(true), true);
});

test("nav board cache keys stay aligned between prefetch and the tab that reads them", () => {
  const api = "http://127.0.0.1:23100";
  assert.equal(harnessBoardResourceKey(api), "harnesses:http://127.0.0.1:23100");
  assert.equal(harnessBoardSessionKey(api), "benes.harnesses.v1:http://127.0.0.1:23100");
  assert.equal(subagentsModelsResourceKey(api), "benes.subagents.v1:http://127.0.0.1:23100");
  assert.equal(subagentsDelegationSessionKey(api), "benes.subagents.delegation.v1:http://127.0.0.1:23100");
  assert.equal(providersConfigCacheKey(api), "benes.providers.config.v1:http://127.0.0.1:23100");
  assert.equal(providersWorkspaceCacheKey(api), "benes.providers.workspace.v2:http://127.0.0.1:23100");
  assert.equal(combosWorkspaceCacheKey(api), "benes.combos.workspace.v1:http://127.0.0.1:23100");
  assert.equal(labMatrixResourceKey(api), "lab-matrix:http://127.0.0.1:23100:{}");
  assert.equal(fabricTasksResourceKey(api), "fabric-tasks:http://127.0.0.1:23100");
});
