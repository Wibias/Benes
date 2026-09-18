/**
 * Corrected current-family ownership.
 *
 * A product review of the global-layer rewrite found twelve class families that the dashboard
 * still emits with no style owner at all: the shell had kept its own copy of some of them and the
 * rest had lost the only rule that dressed them. Each family below must be styled by exactly one
 * shipped sheet, and that sheet has to be the one whose product surface emits the class.
 *
 * The regression is one-directional on purpose. It does not police stylesheets that are already
 * duplicated across boards; it only stops a current family from losing its owner again, and stops
 * a second sheet from claiming a family that already has a home.
 */
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import postcss from "postcss";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

/** Every shipped sheet, so "owned exactly once" can be checked against all of them. */
const SHEETS = [
  "src/styles.css",
  "src/styles-apikeys-workspace.css",
  "src/styles-combos-workspace.css",
  "src/styles-dashboard-workspace.css",
  "src/styles-models-workspace.css",
  "src/styles-providers.css",
  "src/styles-routing.css",
  "src/styles-sessions-workspace.css",
  "src/styles-storage-workspace.css",
  "src/styles-subagents.css",
  "src/styles-usage-workspace.css",
  "src/styles/add-provider.css",
  "src/styles/provider-confirm.css",
  "src/styles/provider-workspace-detail.css",
  "src/styles/provider-workspace-rail.css",
  "src/styles/provider-workspace-shell.css",
  "src/styles/codex-account-pool.css",
];

/** The family, the sheet that owns it, and the source that emits it. */
const OWNERSHIP = [
  { sheet: "src/styles.css", classes: ["benes-stepper__btn"], emittedBy: "src/components/NumberStepper.tsx" },
  { sheet: "src/styles.css", classes: ["sidebar-orb--starred", "sidebar-orb--update"], emittedBy: "src/components/sidebar-github-row.tsx" },
  { sheet: "src/styles.css", classes: ["model-label"], emittedBy: "src/model-display.ts" },
  { sheet: "src/styles.css", classes: ["select-dropdown-beside", "select-dropdown-right"], emittedBy: "src/select-policy.ts" },
  { sheet: "src/styles.css", classes: ["data-surface-skeleton", "data-surface-status"], emittedBy: "src/components/data-surface.tsx" },
  { sheet: "src/styles.css", classes: ["leading-body"], emittedBy: "src/pages/models-provider-hint-surface.ts" },
  { sheet: "src/styles/codex-account-pool.css", classes: ["codex-request-user-input-feedback"], emittedBy: "src/components/codex-request-user-input-sections.tsx" },
  { sheet: "src/styles/codex-account-pool.css", classes: ["account-pool-strategy-card__error"], emittedBy: "src/components/CodexPoolStrategySetting.tsx" },
  { sheet: "src/styles-apikeys-workspace.css", classes: ["api-test-note"], emittedBy: "src/api-access/model-catalog-view.ts" },
];

const CLASS = /.(-?[_a-zA-Z][_a-zA-Z0-9-]*)/g;

async function cssSheets(): Promise<Map<string, string>> {
  const entries = await Promise.all(SHEETS.map(async (sheet) => [sheet, await readFile(path.join(guiRoot, sheet), "utf8")] as const));
  return new Map(entries);
}

async function emittedSources(): Promise<Map<string, string>> {
  const wanted = new Set(OWNERSHIP.map((entry) => entry.emittedBy));
  return new Map(await Promise.all([...wanted].map(async (file) => [file, await readFile(path.join(guiRoot, file), "utf8")] as const)));
}

/** Does any selector in this sheet's text name the class at all? */
function declares(text: string, cls: string): boolean {
  const names = text.match(CLASS) ?? [];
  return names.some((name) => name.slice(1) === cls);
}

test("each corrected class family is emitted by the product source named for it", async () => {
  const sources = await emittedSources();
  const missing: string[] = [];
  for (const entry of OWNERSHIP) {
    const source = sources.get(entry.emittedBy) ?? "";
    for (const cls of entry.classes) {
      if (!declares(source, cls)) missing.push(cls + " not emitted by " + entry.emittedBy);
    }
  }
  assert.deepEqual(missing, []);
});

test("each corrected class family has exactly one shipped style owner", async () => {
  const sheets = await cssSheets();
  const problems: string[] = [];
  for (const entry of OWNERSHIP) {
    for (const cls of entry.classes) {
      const owners = [...sheets].filter(([, text]) => declares(text, cls)).map(([sheet]) => sheet);
      if (owners.length === 0) problems.push(cls + " has no style owner");
      else if (owners.length > 1) problems.push(cls + " is styled by " + owners.join(", "));
      else if (owners[0] !== entry.sheet) problems.push(cls + " is owned by " + owners[0] + ", expected " + entry.sheet);
    }
  }
  assert.deepEqual(problems, []);
});

test("the .benes-stepper__btn rule reaches the arrows outside a stepper wrap", async () => {
  const sheet = await readFile(path.join(guiRoot, "src/styles.css"), "utf8");
  const root = postcss.parse(sheet, { from: undefined });
  const bases: string[] = [];
  root.walkRules((rule) => {
    for (const selector of rule.selectors ?? [rule.selector]) {
      if (selector.trim() === ".benes-stepper__btn") bases.push(rule.toString());
    }
  });
  assert.equal(bases.length, 1, "the arrow base rule must exist once and stand on its own");
  const [rule] = bases;
  for (const declaration of ["width: var(--control-sm)", "height: var(--control-sm)", "border: 1px solid var(--border)", "background: var(--surface)", "cursor: pointer"]) {
    assert.ok(rule.includes(declaration), "arrow base rule is missing " + declaration);
  }
});

