/**
 * Provider workspace state ownership.
 *
 * The shell redesign split one sheet into a frame, a rail and a detail sheet, and the
 * review of that split named four product states whose presentation has to survive it:
 * the zero-provider board, a provider whose mark has no icon asset, the Overview
 * auth-warning callout, and the page width above the board's cap.
 *
 * Each state is pinned twice: that current Benes source still renders the markup, and
 * that exactly one sheet presents it. A second owner is the regression this guards.
 */
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import postcss from "postcss";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

const SHELL = "src/styles/provider-workspace-shell.css";
const RAIL = "src/styles/provider-workspace-rail.css";
const DETAIL = "src/styles/provider-workspace-detail.css";
const PAGE = "src/styles-providers.css";
/** The overview dashboard's hardening sheet, which owns the flat zero-state treatment. */
const HARDENING = "src/styles/providers-workspace-hardening.css";

/** Sheets the redesign owns. The page and the hardening sheet are separate surfaces. */
const WORKSPACE_SHEETS = [SHELL, RAIL, DETAIL];
const ALL_SHEETS = [...WORKSPACE_SHEETS, PAGE, HARDENING];

const CLASS = /\.(-?[_a-zA-Z][_a-zA-Z0-9-]*)/g;

const sheetText = new Map<string, string>();

async function readSheet(sheet: string): Promise<string> {
  const cached = sheetText.get(sheet);
  if (cached !== undefined) return cached;
  const text = await readFile(path.join(guiRoot, sheet), "utf8");
  sheetText.set(sheet, text);
  return text;
}

function readSource(relative: string): Promise<string> {
  return readFile(path.join(guiRoot, relative), "utf8");
}

/** Every class token a sheet declares in a selector - declaration, not mention. */
async function declaredClasses(sheet: string): Promise<Set<string>> {
  const root = postcss.parse(await readSheet(sheet), { from: undefined });
  const classes = new Set<string>();
  root.walkRules(rule => {
    for (const selector of rule.selectors ?? [rule.selector]) {
      for (const match of selector.matchAll(CLASS)) classes.add(match[1]!);
    }
  });
  return classes;
}

/** The sheets that declare a selector for this class. */
async function ownersOf(cls: string): Promise<string[]> {
  const owners: string[] = [];
  for (const sheet of ALL_SHEETS) {
    if ((await declaredClasses(sheet)).has(cls)) owners.push(sheet);
  }
  return owners;
}

test("the zero-provider board renders the steps that exactly one sheet presents", async () => {
  const frames = await readSource("src/components/provider-workspace/workspace-board-frames.tsx");
  for (const cls of ["pws-empty-root--quiet", "pws-empty-hero--quiet", "pws-empty-steps", "pws-zero-rail"]) {
    assert.ok(frames.includes(cls), cls + " is rendered by the zero-provider frame");
  }
  assert.deepEqual(
    await ownersOf("pws-empty-steps"),
    [HARDENING],
    "the numbered step list is owned by one sheet",
  );
  assert.deepEqual(await ownersOf("pws-zero-rail"), [SHELL], "the empty rail column is a frame rule");
});

test("a provider whose mark has no asset falls back to the letter contract in one owner", async () => {
  const mark = await readSource("src/components/ProviderMark.tsx");
  for (const cls of ["provider-icon-fallback", "provider-icon-img--invert-dark", "provider-icon-img--light-plate"]) {
    assert.ok(mark.includes(cls), "ProviderMark emits " + cls);
  }
  const icons = await readSource("src/provider-icons.ts");
  assert.ok(icons.includes("invertDark") && icons.includes("lightPlate"), "provider-icons supplies the variants");

  const workspaceOwners = (await ownersOf("provider-icon-fallback")).filter(sheet => WORKSPACE_SHEETS.includes(sheet));
  assert.deepEqual(workspaceOwners, [SHELL], "the avatar box is declared once across the workspace sheets");
  assert.deepEqual(await ownersOf("provider-icon-img--invert-dark"), [SHELL]);
  assert.deepEqual(await ownersOf("provider-icon-img--light-plate"), [SHELL]);

  assert.deepEqual(
    await ownersOf("providers-workspace-rail-icon"),
    [RAIL],
    "the rail owns the box its ProviderMark fills",
  );
  assert.match(
    await readSheet(RAIL),
    /providers-workspace-rail-icon[\s\S]{0,400}?place-items:\s*center/,
    "the rail icon box centres its mark",
  );
});

test("the Overview auth callout is presented by the page that mounts it, in one place", async () => {
  const overview = await readSource("src/components/provider-workspace/ProviderOverview.tsx");
  assert.ok(overview.includes("pws-auth-summary pws-auth-summary--warn"), "the warning state is emitted");
  assert.ok(overview.includes("pws-auth-summary-body"), "the callout body is emitted");
  assert.ok(overview.includes("activeNeedsReauth"), "the callout is driven by the provider's reauth flag");

  for (const sheet of WORKSPACE_SHEETS) {
    assert.ok(
      !(await declaredClasses(sheet)).has("pws-auth-summary"),
      sheet + " must not claim the auth callout",
    );
  }
  assert.deepEqual(await ownersOf("pws-auth-summary"), [PAGE]);
  assert.deepEqual(await ownersOf("pws-auth-summary--warn"), [PAGE], "base and warning state share one owner");
  assert.deepEqual(await ownersOf("pws-auth-summary-body"), [PAGE]);
});

test("the page width and the narrow header belong to the page, not to the workspace sheets", async () => {
  const page = await readSheet(PAGE);
  assert.match(
    page,
    /\.main-inner\.main-inner--providers\s*\{[^}]*max-width:\s*1440px/,
    "the page that hosts the Shell declares its own width",
  );
  assert.match(
    page,
    /@media \(max-width: 360px\)[\s\S]*?\.providers-head/,
    "the page header wraps below 360px",
  );
  for (const sheet of WORKSPACE_SHEETS) {
    assert.ok(
      !/\.main-inner/.test(await readSheet(sheet)) && !/\.providers-head/.test(await readSheet(sheet)),
      sheet + " must not own page-level layout",
    );
  }
});

test("no sheet keeps a rule for a state class the workspace stopped emitting", async () => {
  const page = await readSheet(PAGE);
  for (const retired of ["pws-detail-tab--active", "pws-rail-row-wrap", "pws-detail-back-link"]) {
    assert.ok(!page.includes(retired), "the page sheet dropped the retired " + retired);
  }
  assert.ok(
    !page.includes(".providers-board .pws-detail-back"),
    "the detail sheet owns the back control",
  );
  assert.deepEqual(
    await ownersOf("providers-workspace-rail-status"),
    [RAIL],
    "the rail dot and its three bins have one owner",
  );
  assert.deepEqual(
    await ownersOf("providers-workspace-rail-row-wrap"),
    [RAIL],
    "selection is declared by the rail, not by the page",
  );
});

