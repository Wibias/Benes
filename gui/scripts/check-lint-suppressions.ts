import { readFile, readdir } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const SOURCE_EXTENSIONS = new Set([".js", ".jsx", ".mjs", ".cjs", ".ts", ".tsx"]);
const DIRECTIVE_PATTERN = /(?:eslint|oxlint)-(?:disable(?:-next-line|-line)?|enable)\b[^\r\n]*|@ts-(?:ignore|nocheck|expect-error)\b[^\r\n]*/g;

function normalizePath(value) {
  return value.split(path.sep).join("/");
}

function normalizeDirective(value) {
  return value
    .replace(/\*\/.*$/, "")
    .trim()
    .replace(/\s+/g, " ");
}

export function extractSuppressions(content) {
  return [...content.matchAll(DIRECTIVE_PATTERN)]
    .map((match) => normalizeDirective(match[0]))
    .filter(Boolean);
}

async function sourceFiles(guiRoot) {
  const srcRoot = path.join(guiRoot, "src");
  const files = [];

  async function walk(directory) {
    for (const entry of await readdir(directory, { withFileTypes: true })) {
      const absolute = path.join(directory, entry.name);
      const relativeToSrc = normalizePath(path.relative(srcRoot, absolute));
      if (entry.isDirectory()) {
        if (relativeToSrc === "i18n" || relativeToSrc.startsWith("i18n/")) continue;
        await walk(absolute);
        continue;
      }
      if (!entry.isFile() || !SOURCE_EXTENSIONS.has(path.extname(entry.name))) continue;
      files.push(absolute);
    }
  }

  await walk(srcRoot);
  files.sort();
  return files;
}

export async function scanSuppressions(guiRoot) {
  const counts = new Map();
  for (const absolute of await sourceFiles(guiRoot)) {
    const relativePath = normalizePath(path.relative(guiRoot, absolute));
    const content = await readFile(absolute, "utf8");
    for (const directive of extractSuppressions(content)) {
      const key = `${relativePath}\0${directive}`;
      const previous = counts.get(key);
      counts.set(key, {
        path: relativePath,
        directive,
        count: (previous?.count ?? 0) + 1,
      });
    }
  }

  return [...counts.values()].sort((left, right) => (
    left.path.localeCompare(right.path) || left.directive.localeCompare(right.directive)
  ));
}

function suppressionKey(entry) {
  return `${entry.path}\0${entry.directive}`;
}

function normalizedEntry(entry) {
  return {
    path: normalizePath(String(entry.path)),
    directive: normalizeDirective(String(entry.directive)),
    count: Number(entry.count),
  };
}

export function compareSuppressions(actualEntries, approvedEntries) {
  const errors = [];
  const approved = new Map();
  for (const raw of approvedEntries) {
    const entry = normalizedEntry(raw);
    if (!entry.path || !entry.directive || !Number.isInteger(entry.count) || entry.count < 1) {
      errors.push(`Invalid suppression baseline entry: ${JSON.stringify(raw)}`);
      continue;
    }
    const key = suppressionKey(entry);
    if (approved.has(key)) {
      errors.push(`Duplicate suppression baseline entry: ${entry.path}: ${entry.directive}`);
      continue;
    }
    approved.set(key, entry);
  }

  const actual = new Map(actualEntries.map((raw) => {
    const entry = normalizedEntry(raw);
    return [suppressionKey(entry), entry];
  }));

  for (const [key, entry] of actual) {
    const expected = approved.get(key);
    if (!expected) {
      errors.push(`Unapproved suppression: ${entry.path}: ${entry.directive} (count ${entry.count})`);
      continue;
    }
    if (entry.count > expected.count) {
      errors.push(`Unapproved suppression count ${entry.count}; approved ${expected.count}: ${entry.path}: ${entry.directive}`);
    } else if (entry.count < expected.count) {
      errors.push(`Stale baseline count ${expected.count}; found ${entry.count}: ${entry.path}: ${entry.directive}`);
    }
  }

  for (const [key, entry] of approved) {
    if (!actual.has(key)) {
      errors.push(`Stale baseline: ${entry.path}: ${entry.directive} (approved count ${entry.count})`);
    }
  }

  return errors.sort();
}

async function main() {
  const scriptDir = path.dirname(fileURLToPath(import.meta.url));
  const guiRoot = path.resolve(scriptDir, "..");
  const baselinePath = path.join(scriptDir, "lint-suppression-baseline.json");
  const baseline = JSON.parse(await readFile(baselinePath, "utf8"));
  if (!Array.isArray(baseline)) {
    throw new TypeError("lint-suppression-baseline.json must contain an array");
  }

  const actual = await scanSuppressions(guiRoot);
  const errors = compareSuppressions(actual, baseline);
  if (errors.length > 0) {
    console.error("Lint suppression policy failed:");
    for (const error of errors) console.error(`- ${error}`);
    console.error("Review the exception centrally; do not add or edit inline suppression directives ad hoc.");
    process.exitCode = 1;
    return;
  }
  const total = actual.reduce((sum, entry) => sum + entry.count, 0);
  console.log(`Lint suppression policy passed (${total} approved directive${total === 1 ? "" : "s"}).`);
}

const invokedPath = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : null;
if (invokedPath === import.meta.url) {
  await main();
}
