/**
 * #331 closure for the four `DELETE_DEAD` union-MIXED dashboard paths.
 *
 * Each one was unreachable from the current runtime graph: the Startup page sections were
 * superseded by the Control board, the provider preset control had no caller at all, the
 * context-window draft policy is owned by `models-groups.ts`, and the capacity/recovery row
 * projection lost its last renderer when the ProviderCapacityQuota surface went away. The
 * files are deleted rather than re-authored, so these assertions pin the absence and name the
 * surviving owner of each behaviour.
 */
import assert from "node:assert/strict";
import { existsSync, readFileSync, readdirSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  applyProviderModelContextWindows,
  modelContextWindowsPatchApplied,
  overlayPendingModelContextWindows,
  remainingModelContextWindowPatches,
} from "../src/models-groups.ts";
import { formatAccessQuotaReset } from "../src/provider-workspace/quota-presentation.ts";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

const DELETED_PATHS = [
  "src/pages/startup-sections.tsx",
  "src/pages/models-preset-control.tsx",
  "src/pages/models-context-policy.ts",
  "src/provider-workspace/capacity-presentation.ts",
] as const;

/** Distinctive file-name stems; none of them is a substring of a surviving module name. */
const DELETED_MODULE_STEMS = [
  "startup-sections",
  "models-preset-control",
  "models-context-policy",
  "capacity-presentation",
] as const;

function source(relativePath: string): string {
  return readFileSync(path.join(guiRoot, relativePath), "utf8");
}

function guiSources(): Array<{ path: string; source: string }> {
  const srcRoot = path.join(guiRoot, "src");
  return (readdirSync(srcRoot, { recursive: true }) as string[])
    .map(entry => entry.replace(/\\/g, "/"))
    .filter(entry => /\.[cm]?tsx?$/.test(entry))
    .map(entry => ({ path: `src/${entry}`, source: readFileSync(path.join(srcRoot, entry), "utf8") }));
}

test("the four #331 dead-source paths stay deleted", () => {
  for (const relativePath of DELETED_PATHS) {
    assert.equal(existsSync(path.join(guiRoot, relativePath)), false, relativePath);
  }
});

test("no current GUI source names a deleted module", () => {
  const referenced = guiSources()
    .filter(file => DELETED_MODULE_STEMS.some(stem => file.source.includes(stem)))
    .map(file => file.path);
  assert.deepEqual(referenced, []);
});

test("context-window write policy is owned by models-groups, not the deleted page module", () => {
  const listed = [{ name: "openai", modelContextWindows: {} }];
  const pending = [{ provider: "openai", windows: { "gpt-5.4": 64_000 } }];

  const patched = applyProviderModelContextWindows(listed, "openai", { "gpt-5.4": 64_000 });
  assert.deepEqual(patched[0]?.modelContextWindows, { "gpt-5.4": 64_000 });
  assert.deepEqual(
    overlayPendingModelContextWindows(listed, pending)[0]?.modelContextWindows,
    { "gpt-5.4": 64_000 },
  );
  assert.equal(modelContextWindowsPatchApplied(patched, "openai", { "gpt-5.4": 64_000 }), true);
  assert.deepEqual(remainingModelContextWindowPatches(patched, pending), []);
  assert.equal(modelContextWindowsPatchApplied(listed, "openai", { "gpt-5.4": 64_000 }), false);
  assert.deepEqual(remainingModelContextWindowPatches(listed, pending), pending);
});

test("quota presentation is owned by quota-presentation and still read by the Access surfaces", () => {
  const t = (key: string, vars?: Record<string, string | number>) => (vars ? `${key}:${JSON.stringify(vars)}` : key);
  const now = Date.UTC(2026, 0, 2, 3, 4, 5);
  assert.equal(formatAccessQuotaReset(now + 90 * 60_000, t as never, now), "quota.resetsInCompact:{\"wait\":\"1h\"}");
  assert.equal(formatAccessQuotaReset(now - 1, t as never, now), "");
  assert.match(source("src/components/provider-workspace/OAuthAccountsTable.tsx"), /formatAccessQuotaReset/);
  assert.match(source("src/components/provider-workspace/auth-panel-oauth.tsx"), /projectQuotaSurface/);
  assert.match(source("src/components/ProviderQuotaMeters.tsx"), /quota-presentation/);
});

test("the Models catalogue controls remain the selection surface the preset control lost", () => {
  const controls = source("src/pages/models-catalog-controls.tsx");
  assert.match(controls, /export function ModelsCatalogToolbar/);
  assert.match(controls, /export function ModelsCollapseControls/);
  const models = source("src/pages/Models.tsx");
  assert.match(models, /from "\.\/models-catalog-controls"/);
  assert.match(models, /<ModelsCatalogToolbar/);
});
