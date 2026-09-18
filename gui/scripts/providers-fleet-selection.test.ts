import assert from "node:assert/strict";
import test from "node:test";

import {
  bumpCatalogSyncEpochs,
  railRowSelected,
  workspaceMainKind,
} from "../src/provider-workspace/workspace-shell.ts";

test("fleet overview and a selected provider are mutually exclusive", () => {
  assert.equal(workspaceMainKind(false, false, true), "dashboard");
  assert.equal(workspaceMainKind(false, true, true), "detail");
  assert.equal(railRowSelected(null, "openai"), false);
  assert.equal(railRowSelected("openai", "openai"), true);
  assert.equal(railRowSelected("openai", "anthropic"), false);
});

test("catalog sync bumps models and workspace epochs together", () => {
  let models = 3;
  let workspace = 7;
  bumpCatalogSyncEpochs(
    fn => { models = fn(models); },
    fn => { workspace = fn(workspace); },
  );
  assert.equal(models, 4);
  assert.equal(workspace, 8);
});

test("clearing the selected provider returns fleet overview", () => {
  const selected = "openai";
  const cleared = null;
  assert.equal(workspaceMainKind(false, Boolean(selected), true), "detail");
  assert.equal(workspaceMainKind(false, Boolean(cleared), true), "dashboard");
  assert.equal(railRowSelected(cleared, "openai"), false);
});
