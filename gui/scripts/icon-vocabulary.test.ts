/**
 * Icon vocabulary contract.
 *
 * `icons.tsx` is TSX, so the tree is read with the TypeScript parser rather than imported: the
 * point of these checks is that the *name set* the rest of the dashboard imports from stays
 * exactly what it is, and that every member keeps the one shared SVG contract. The rendered
 * geometry itself is covered by the change's visual evidence.
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
const source = readFileSync(path.join(guiRoot, "src", "icons.tsx"), "utf8");
const sf = ts.createSourceFile("icons.tsx", source, ts.ScriptTarget.ESNext, true, ts.ScriptKind.TSX);

/** Exported icon components, in source order. */
function iconExports(): Array<{ name: string; factoryName: string | null }> {
  const found = [];
  for (const statement of sf.statements) {
    if (!ts.isVariableStatement(statement)) continue;
    if (!statement.modifiers?.some(modifier => modifier.kind === ts.SyntaxKind.ExportKeyword)) continue;
    const declaration = statement.declarationList.declarations[0];
    const name = declaration.name.getText(sf);
    if (!name.startsWith("Icon")) continue;
    const call = declaration.initializer;
    const factoryName = ts.isCallExpression(call) && ts.isIdentifier(call.expression)
      ? call.expression.text
      : null;
    found.push({ name, factoryName });
  }
  return found;
}

const EXPECTED_ICONS = [
  "IconActivity", "IconAlert", "IconAlertCircle", "IconArrowDown", "IconArrowLeft", "IconArrowUp",
  "IconBot", "IconBoxes", "IconCheck", "IconChevron", "IconCircle", "IconCopy", "IconCpu",
  "IconDownload", "IconExternal", "IconEye", "IconEyeOff", "IconFile", "IconFilter", "IconFolder",
  "IconGauge", "IconGithub", "IconGlobe", "IconGrid", "IconGrip", "IconHardDrive", "IconHelp",
  "IconInfo", "IconKey", "IconLink", "IconList", "IconLock", "IconMenu", "IconMonitor", "IconMoon",
  "IconMore", "IconPause", "IconPencil", "IconPlay", "IconPlus", "IconPower", "IconRefresh",
  "IconSearch", "IconServer", "IconShield", "IconShuffle", "IconStar", "IconSun", "IconTerminal",
  "IconTicket", "IconTrash", "IconUndo", "IconUpload", "IconX",
] as const;

test("the icon vocabulary is exactly the names the dashboard imports", () => {
  assert.deepEqual(iconExports().map(icon => icon.name).sort(), [...EXPECTED_ICONS].sort());
});

test("every icon is built by the shared factory", () => {
  for (const icon of iconExports()) {
    assert.equal(icon.factoryName, "strokeIcon", `${icon.name} bypasses the factory`);
  }
});

test("every icon is registered under its exported name", () => {
  const registered = [...source.matchAll(/^export const (Icon[A-Za-z0-9]+) = strokeIcon\("([^"]+)"/gm)]
    .map(match => [match[1], match[2]]);
  assert.equal(registered.length, EXPECTED_ICONS.length);
  for (const [exported, registeredName] of registered) {
    assert.equal(registeredName, exported, `${exported} registers as ${registeredName}`);
  }
});

test("the shared SVG contract is declared once and callers can override it", () => {
  const contract = source.slice(source.indexOf("const STROKE_ICON"), source.indexOf("function strokeIcon"));
  for (const declaration of [
    'viewBox: "0 0 24 24"',
    'fill: "none"',
    'stroke: "currentColor"',
    "strokeWidth: 2",
    'strokeLinecap: "round"',
    'strokeLinejoin: "round"',
  ]) {
    assert.equal(contract.includes(declaration), true, `missing ${declaration}`);
  }
  // Caller props are spread after the defaults, so an icon can still be filled or hidden.
  assert.match(source, /<svg \{\.\.\.STROKE_ICON\} \{\.\.\.props\}>/);
});

test("the vocabulary carries no icon-library dependency", () => {
  const imports = [...source.matchAll(/^import .*$/gm)].map(match => match[0]);
  assert.deepEqual(imports.length, 1);
  assert.match(imports[0]!, /^import type \{ ReactNode, SVGProps \} from "react";$/);
});
