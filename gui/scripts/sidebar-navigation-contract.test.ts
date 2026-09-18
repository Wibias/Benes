/**
 * Sidebar information-architecture contract.
 *
 * `app-sidebar.tsx` is a TSX module, and this repository has no DOM renderer, so the checks
 * split in two: the navigation model is read out of the source *as a structure* with the
 * TypeScript parser (not by probing strings), and the rendering contract is asserted on the
 * exact attribute expressions the markup carries. Layout and interaction evidence for the
 * same surface is captured by the screenshots attached to the change.
 */
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { createRequire } from "node:module";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const require = createRequire(import.meta.url);
const ts = require("typescript") as typeof import("typescript");

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function readSource(relativePath: string): string {
  return readFileSync(path.join(guiRoot, relativePath), "utf8");
}

function parseSource(relativePath: string): ts.SourceFile {
  const tsx = relativePath.endsWith(".tsx");
  return ts.createSourceFile(
    relativePath,
    readSource(relativePath),
    ts.ScriptTarget.ESNext,
    true,
    tsx ? ts.ScriptKind.TSX : ts.ScriptKind.TS,
  );
}

function findVariableInitializer(source: ts.SourceFile, name: string): ts.Expression | null {
  let found: ts.Expression | null = null;
  const visit = (node: ts.Node): void => {
    if (found !== null) return;
    if (
      ts.isVariableDeclaration(node)
      && ts.isIdentifier(node.name)
      && node.name.text === name
      && node.initializer !== undefined
    ) {
      found = node.initializer;
      return;
    }
    node.forEachChild(visit);
  };
  source.forEachChild(visit);
  return found;
}

/** Reduce a literal-only initializer to plain values, or `undefined` when it is not literal. */
function literalValue(node: ts.Node): unknown {
  if (ts.isStringLiteral(node)) return node.text;
  if (node.kind === ts.SyntaxKind.TrueKeyword) return true;
  if (node.kind === ts.SyntaxKind.FalseKeyword) return false;
  if (ts.isArrayLiteralExpression(node)) return node.elements.map(element => literalValue(element));
  if (ts.isObjectLiteralExpression(node)) {
    const entries: Record<string, unknown> = {};
    for (const property of node.properties) {
      if (!ts.isPropertyAssignment(property)) continue;
      const key = ts.isIdentifier(property.name) || ts.isStringLiteral(property.name)
        ? property.name.text
        : null;
      if (key === null) continue;
      entries[key] = literalValue(property.initializer);
    }
    return entries;
  }
  return undefined;
}

interface DestinationShape {
  id: string;
  tkey: string;
  gate?: string;
}

interface SectionShape {
  label?: string;
  items: DestinationShape[];
}

function readNavigation(): SectionShape[] {
  const initializer = findVariableInitializer(parseSource("src/app-sidebar.tsx"), "SIDEBAR_NAV_SECTIONS");
  assert.ok(initializer, "app-sidebar.tsx must declare SIDEBAR_NAV_SECTIONS");
  const sections = literalValue(initializer);
  assert.ok(Array.isArray(sections), "SIDEBAR_NAV_SECTIONS must be an array literal");
  return sections as SectionShape[];
}

/** The `Page` union, so a destination cannot name a page the router does not have. */
function readPageUnion(): Set<string> {
  const source = parseSource("src/app-routing.ts");
  const pages = new Set<string>();
  const visit = (node: ts.Node): void => {
    if (
      ts.isTypeAliasDeclaration(node)
      && node.name.text === "Page"
      && ts.isUnionTypeNode(node.type)
    ) {
      for (const member of node.type.types) {
        if (ts.isLiteralTypeNode(member) && ts.isStringLiteral(member.literal)) {
          pages.add(member.literal.text);
        }
      }
      return;
    }
    node.forEachChild(visit);
  };
  source.forEachChild(visit);
  return pages;
}

const CONFIGURATION = "nav.group.configuration";
const OBSERVATION = "nav.group.observation";
const SYSTEM = "nav.group.system";

test("the sidebar renders four blocks in product order", () => {
  const sections = readNavigation();
  assert.deepEqual(
    sections.map(section => section.label),
    [undefined, CONFIGURATION, OBSERVATION, SYSTEM],
  );
});

test("every block holds its destinations in order", () => {
  const sections = readNavigation();
  assert.deepEqual(
    sections.map(section => section.items.map(item => item.id)),
    [
      ["dashboard"],
      ["harnesses", "subagents", "routing", "models", "providers"],
      ["sessions", "usage", "logs", "tasks"],
      ["startup", "api", "storage"],
    ],
  );
});

test("Tasks is the only gated destination and it is gated on Fabric", () => {
  const gated = readNavigation()
    .flatMap(section => section.items)
    .filter(item => item.gate !== undefined);
  assert.deepEqual(gated.map(item => [item.id, item.gate]), [["tasks", "fabric"]]);
  const observation = readNavigation().find(section => section.label === OBSERVATION);
  assert.equal(observation?.items.at(-1)?.id, "tasks");
});

test("every destination names a page the router actually has", () => {
  const pages = readPageUnion();
  assert.ok(pages.size > 0, "the Page union must be readable");
  for (const item of readNavigation().flatMap(section => section.items)) {
    assert.equal(pages.has(item.id), true, `${item.id} is not a Page`);
  }
});

test("every destination carries its own label key", () => {
  for (const item of readNavigation().flatMap(section => section.items)) {
    assert.equal(item.tkey.startsWith("nav."), true, `${item.id} lost its catalogue key`);
  }
});

test("Integrations is not a sidebar destination", () => {
  const ids = readNavigation().flatMap(section => section.items.map(item => item.id));
  assert.equal(ids.includes("integrations"), false);
  assert.equal(ids.includes("codex-auth" as never), false);
});

test("the sidebar offers destinations only, with no action deep links", () => {
  // The retired self-update dialog was the only sub-view action link; nothing replaces it.
  const sidebar = readSource("src/app-sidebar.tsx");
  assert.equal(sidebar.includes("subPath"), false);
  assert.equal(sidebar.includes("onOpenUpdate"), false);
});

test("navigation renders a semantic nav with the current page marked", () => {
  const sidebar = readSource("src/app-sidebar.tsx");
  assert.match(sidebar, /<nav>/);
  assert.match(sidebar, /aria-current=\{active \? "page" : undefined\}/);
  assert.match(sidebar, /className=\{active \? "nav-item active" : "nav-item"\}/);
  assert.match(sidebar, /data-page=\{destination\.id\}/);
});

test("the drawer and the mobile trigger keep their accessible relationships", () => {
  const sidebar = readSource("src/app-sidebar.tsx");
  assert.match(sidebar, /<aside id="app-sidebar"/);
  assert.match(sidebar, /className=\{sidebarClassName\(navOpen\)\}/);
  assert.match(sidebar, /tabIndex=\{-1\}/);
  assert.match(sidebar, /aria-expanded=\{navOpen\}/);
  assert.match(sidebar, /aria-controls="app-sidebar"/);
  assert.match(sidebar, /const menuLabel = t\(navOpen \? "nav\.closeMenu" : "nav\.openMenu"\)/);
  assert.match(sidebar, /aria-label=\{menuLabel\}/);
  assert.match(sidebar, /title=\{menuLabel\}/);
  assert.match(sidebar, /const closeLabel = t\("nav\.closeMenu"\)/);
  assert.match(sidebar, /aria-label=\{closeLabel\}/);
  assert.match(sidebar, /title=\{closeLabel\}/);
  assert.match(sidebar, /<header className="mobile-topbar" inert=\{navOpen\}>/);
});

test("icon-only chrome actions carry one name for both label and tooltip", () => {
  const sidebar = readSource("src/app-sidebar.tsx");
  assert.match(sidebar, /aria-label=\{label\}/);
  assert.match(sidebar, /title=\{label\}/);
  assert.match(sidebar, /label=\{t\(stopping \? "dash\.stopping" : "dash\.stop"\)\}/);
  assert.match(sidebar, /label=\{t\(codexRestarting \? "dash\.codexRestarting" : "dash\.codexRestart"\)\}/);
  const themeToggle = sidebar.slice(sidebar.indexOf('className="theme-toggle"'));
  assert.match(themeToggle.slice(0, 400), /aria-label=\{themeLabel\}/);
  assert.match(themeToggle.slice(0, 400), /title=\{themeLabel\}/);
});

test("the locale selector keeps its label, inline placement, and full width", () => {
  const sidebar = readSource("src/app-sidebar.tsx");
  const select = sidebar.slice(sidebar.indexOf("<Select"), sidebar.indexOf("/>", sidebar.indexOf("<Select")));
  assert.match(select, /value=\{locale\}/);
  assert.match(select, /options=\{LOCALE_OPTIONS\}/);
  assert.match(select, /label=\{t\("lang\.label"\)\}/);
  assert.match(select, /placement="right"/);
  assert.match(select, /portal=\{false\}/);
  assert.match(select, /style=\{LOCALE_SELECT_STYLE\}/);
});

test("the static locale choices and Select style are built once", () => {
  const sidebar = readSource("src/app-sidebar.tsx");
  assert.match(sidebar, /const LOCALE_OPTIONS: SelectOption\[\] = LOCALES\.map/);
  assert.match(sidebar, /const LOCALE_SELECT_STYLE = \{ flex: 1, minWidth: 0, width: "100%" \} as const;/);
});

test("the GitHub/update row is projected, not re-derived, in the sidebar", () => {
  const sidebar = readSource("src/app-sidebar.tsx");
  assert.match(sidebar, /<SidebarGithubRow apiBase=\{apiBase\} \/>/);
  assert.equal(sidebar.includes("starClickMode"), false);
  assert.equal(sidebar.includes("/api/github/star"), false);
});
