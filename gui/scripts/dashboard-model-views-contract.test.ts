import assert from "node:assert/strict";
import test from "node:test";

import {
  shadowCallModelOptions,
  sidecarModelIds,
  visionModelOptions,
  visionSelectOptions,
} from "../src/pages/dashboard-model-views.ts";

function model(id: string, provider: string) {
  return { id, provider, namespaced: `${provider}/${id}` };
}

const MODELS = [
  model("b-2", "openai"),
  model("a-1", "anthropic"),
  model("grok-4", "xai"),
];

test("the legacy describer list keeps only the providers the sidecar can describe with", () => {
  // The listener publishes eligible ids; this list is the documented degrade path for a
  // response that predates the field, so it still has to name routing ids rather than raw
  // catalog names.
  assert.deepEqual(sidecarModelIds(MODELS), ["openai/b-2", "anthropic/a-1"]);
  assert.deepEqual(sidecarModelIds([]), []);
});

test("a stored describer the server no longer offers stays selectable, ahead of the list", () => {
  assert.deepEqual(visionModelOptions(undefined, MODELS, "missing-vision"), [
    "missing-vision",
    "openai/b-2",
    "anthropic/a-1",
  ]);
  assert.deepEqual(visionSelectOptions(MODELS, { webSearch: { model: "g" }, vision: { model: "missing-vision" } }), [
    "missing-vision",
    "openai/b-2",
    "anthropic/a-1",
  ]);
});

test("a stored describer already offered is not repeated", () => {
  assert.deepEqual(visionModelOptions(undefined, MODELS, "openai/b-2"), [
    "openai/b-2",
    "anthropic/a-1",
  ]);
  assert.deepEqual(visionModelOptions(["anthropic/a-1", "openai/b-2"], MODELS, "openai/b-2"), [
    "anthropic/a-1",
    "openai/b-2",
  ]);
});

test("an empty eligible list is the server's answer, not an invitation to refill from the catalog", () => {
  assert.deepEqual(visionModelOptions([], MODELS, undefined), []);
  assert.deepEqual(
    visionSelectOptions(MODELS, { webSearch: { model: "g" }, vision: { model: "" }, visionModels: [] }),
    [],
  );
});

test("with no stored describer the catalog order is untouched", () => {
  assert.deepEqual(visionModelOptions(undefined, MODELS, undefined), sidecarModelIds(MODELS));
  assert.deepEqual(visionSelectOptions(MODELS, null), ["openai/b-2", "anthropic/a-1"]);
});

test("shadow replacement options carry routing ids, an off row, and the stored model", () => {
  assert.deepEqual(shadowCallModelOptions(MODELS, undefined), [
    { value: "", label: "—" },
    { value: "openai/b-2", label: "openai/b-2" },
    { value: "anthropic/a-1", label: "anthropic/a-1" },
    { value: "xai/grok-4", label: "xai/grok-4" },
  ]);
  assert.deepEqual(shadowCallModelOptions(MODELS, "stored/model").at(-1), {
    value: "stored/model",
    label: "stored/model",
  });
});
