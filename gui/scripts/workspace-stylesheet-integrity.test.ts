/**
 * Workspace stylesheet integrity.
 *
 * The five workspace sheets must style markup Benes actually renders: a selector whose
 * classes no longer appear in any source is dead weight the board cannot exercise, and a
 * `var()` with no definition silently drops its declaration at computed-value time.
 *
 * These are the invariants #294 measured by hand; this keeps them from coming back.
 */
import assert from "node:assert/strict";
import { readdir, readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import postcss from "postcss";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

const WORKSPACE_SHEETS = [
  "src/styles-apikeys-workspace.css",
  "src/styles-combos-workspace.css",
  "src/styles-dashboard-workspace.css",
  "src/styles-models-workspace.css",
  "src/styles-storage-workspace.css",
];

const CLASS = /\.(-?[_a-zA-Z][_a-zA-Z0-9-]*)/g;
const MARKUP = /\.tsx?$/;
const ANY_SOURCE = /\.(tsx?|css)$/;

async function sourceFiles(dir: string, extensions: RegExp, out: string[] = []): Promise<string[]> {
  for (const entry of await readdir(dir, { withFileTypes: true })) {
    const full = path.join(dir, entry.name);
    if (entry.isDirectory()) { await sourceFiles(full, extensions, out); continue; }
    if (extensions.test(entry.name)) out.push(full);
  }
  return out;
}

async function guiSources(extensions: RegExp): Promise<Array<{ path: string; text: string }>> {
  const files = await sourceFiles(path.join(guiRoot, "src"), extensions);
  return Promise.all(files.map(async file => ({
    path: path.relative(guiRoot, file).replaceAll("\\", "/"),
    text: await readFile(file, "utf8"),
  })));
}

function mentions(sources: Array<{ text: string }>, cls: string): boolean {
  const pattern = new RegExp("(?<![\\w-])" + cls + "(?![\\w-])");
  return sources.some(source => pattern.test(source.text));
}

test("every class a workspace sheet styles is still rendered by the current source", async () => {
  const sources = await guiSources(MARKUP);
  const dead: string[] = [];
  for (const sheet of WORKSPACE_SHEETS) {
    const root = postcss.parse(await readFile(path.join(guiRoot, sheet), "utf8"), { from: undefined });
    root.walkRules(rule => {
      for (const selector of rule.selectors ?? [rule.selector]) {
        const classes = [...selector.matchAll(CLASS)].map(match => match[1]!);
        if (classes.length === 0) continue;
        if (classes.every(cls => !mentions(sources, cls))) dead.push(sheet + " :: " + selector);
      }
    });
  }
  assert.deepEqual(dead, [], "workspace selectors with no current consumer");
});

test("every custom property a workspace sheet reads is defined somewhere in the GUI", async () => {
  // Token definitions live in stylesheets as well as in component style attributes.
  const sources = await guiSources(ANY_SOURCE);
  const defined = new Set<string>();
  for (const source of sources) {
    for (const match of source.text.matchAll(/(--[a-z0-9-]+)\s*:/g)) defined.add(match[1]!);
  }
  const missing: string[] = [];
  for (const sheet of WORKSPACE_SHEETS) {
    const text = await readFile(path.join(guiRoot, sheet), "utf8");
    text.split(/\r?\n/).forEach((line, index) => {
      for (const match of line.matchAll(/var\((--[a-z0-9-]+)(\s*,[^)]*)?\)/g)) {
        if (defined.has(match[1]!) || match[2]) continue;
        missing.push(sheet + ":" + (index + 1) + " reads " + match[1]);
      }
    });
  }
  assert.deepEqual(missing, [], "declarations that read an undefined design token");
});

test("styles.css still imports every workspace sheet", async () => {
  const root = await readFile(path.join(guiRoot, "src", "styles.css"), "utf8");
  for (const sheet of WORKSPACE_SHEETS) {
    const importLine = '@import "./' + path.basename(sheet) + '";';
    assert.ok(root.includes(importLine), path.basename(sheet));
  }
});

