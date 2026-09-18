import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function readSrc(...parts) {
  return readFileSync(path.join(guiRoot, ...parts), "utf8");
}

test("finite Diagnostics time ranges hydrate the complete retained scope", () => {
  const source = readSrc("src", "pages", "use-diagnostics-workspace.ts");
  assert.match(source, /DIAGNOSTICS_MAX_LIMIT/);
  assert.match(source, /filters\.timeRange === "all"\s*\?\s*limit\s*:\s*DIAGNOSTICS_MAX_LIMIT/);
  assert.match(source, /fetchList\(\s*apiBase,\s*EMPTY_DIAGNOSTICS_FILTERS,\s*sessionId,\s*\{\s*limit:\s*DIAGNOSTICS_MAX_LIMIT\s*\}/s);
  assert.match(source, /setOptionRows/);
  assert.match(source, /optionRows/);
});

test("Diagnostics routing renders requested provider as well as resolved provider", () => {
  const source = readSrc("src", "pages", "diagnostics-detail.tsx");
  assert.match(source, /routing\.requestedProvider/);
  assert.match(source, /logs\.col\.provider/);
  assert.match(source, /logs\.detail\.route\.resolvedProvider/);
});
