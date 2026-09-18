import { spawnSync } from "node:child_process";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath, pathToFileURL } from "node:url";

const STRUCTURAL_CODES = new Set(["eslint(complexity)", "eslint(max-depth)"]);

function normalizePath(value) {
  return String(value).replace(/\\/g, "/").replace(/^\.\//, "");
}

function normalizeEntry(entry) {
  return {
    path: normalizePath(entry.path),
    code: String(entry.code).trim(),
    message: String(entry.message).trim(),
    count: Number(entry.count),
  };
}

function entryKey(entry) {
  return `${entry.path}\0${entry.code}\0${entry.message}`;
}

export function summarizeStructuralDiagnostics(diagnostics) {
  const counts = new Map();
  for (const diagnostic of diagnostics) {
    if (!STRUCTURAL_CODES.has(diagnostic?.code)) continue;
    const pathValue = normalizePath(diagnostic.filename ?? "");
    const code = String(diagnostic.code).trim();
    const message = String(diagnostic.message ?? "").trim();
    if (!pathValue || !code || !message) continue;
    const key = `${pathValue}\0${code}\0${message}`;
    const previous = counts.get(key);
    counts.set(key, {
      path: pathValue,
      code,
      message,
      count: (previous?.count ?? 0) + 1,
    });
  }
  return [...counts.values()].sort((left, right) => (
    left.path.localeCompare(right.path)
      || left.code.localeCompare(right.code)
      || left.message.localeCompare(right.message)
  ));
}

export function compareStructuralBaseline(actualEntries, approvedEntries) {
  const errors = [];
  const approved = new Map();
  for (const raw of approvedEntries) {
    const entry = normalizeEntry(raw);
    if (!entry.path || !STRUCTURAL_CODES.has(entry.code) || !entry.message || !Number.isInteger(entry.count) || entry.count < 1) {
      errors.push(`Invalid structural baseline entry: ${JSON.stringify(raw)}`);
      continue;
    }
    const key = entryKey(entry);
    if (approved.has(key)) {
      errors.push(`Duplicate structural baseline entry: ${entry.path}: ${entry.code}: ${entry.message}`);
      continue;
    }
    approved.set(key, entry);
  }

  const actual = new Map(actualEntries.map(raw => {
    const entry = normalizeEntry(raw);
    return [entryKey(entry), entry];
  }));

  for (const [key, entry] of actual) {
    const expected = approved.get(key);
    if (!expected) {
      errors.push(`Unapproved structural violation: ${entry.path}: ${entry.code}: ${entry.message} (count ${entry.count})`);
      continue;
    }
    if (entry.count > expected.count) {
      errors.push(`Unapproved structural count ${entry.count}; approved ${expected.count}: ${entry.path}: ${entry.code}: ${entry.message}`);
    } else if (entry.count < expected.count) {
      errors.push(`Stale structural baseline count ${expected.count}; found ${entry.count}: ${entry.path}: ${entry.code}: ${entry.message}`);
    }
  }

  for (const [key, entry] of approved) {
    if (!actual.has(key)) {
      errors.push(`Stale structural baseline: ${entry.path}: ${entry.code}: ${entry.message} (approved count ${entry.count})`);
    }
  }

  return errors.sort();
}

function diagnosticPath(filename, guiRoot) {
  const normalized = normalizePath(filename);
  const normalizedRoot = normalizePath(guiRoot).replace(/\/$/, "");
  if (normalized.toLowerCase().startsWith(`${normalizedRoot.toLowerCase()}/`)) {
    return normalized.slice(normalizedRoot.length + 1);
  }
  return normalized;
}

export function diagnosticsFromOxlintJson(payload, guiRoot) {
  if (!payload || !Array.isArray(payload.diagnostics)) {
    throw new TypeError("Oxlint JSON output must contain a diagnostics array");
  }
  return payload.diagnostics.map(diagnostic => ({
    ...diagnostic,
    filename: diagnosticPath(diagnostic.filename ?? "", guiRoot),
  }));
}

export function runOxlintJson(guiRoot) {
  const oxlintBin = path.join(guiRoot, "node_modules", "oxlint", "bin", "oxlint");
  const result = spawnSync(
    process.execPath,
    [oxlintBin, "--format=json", "-W", "complexity", "-W", "max-depth", "."],
    {
      cwd: guiRoot,
      encoding: "utf8",
      maxBuffer: 32 * 1024 * 1024,
    },
  );
  if (result.error) throw result.error;
  const stdout = String(result.stdout ?? "").trim();
  if (!stdout) {
    throw new Error(`Oxlint JSON scan produced no output (exit ${result.status ?? "unknown"}): ${String(result.stderr ?? "").trim()}`);
  }
  try {
    return diagnosticsFromOxlintJson(JSON.parse(stdout), guiRoot);
  } catch (error) {
    throw new Error(`Failed to parse Oxlint JSON output: ${error instanceof Error ? error.message : String(error)}`);
  }
}

async function readBaseline(scriptDir, filename, code) {
  const raw = JSON.parse(await readFile(path.join(scriptDir, filename), "utf8"));
  if (!Array.isArray(raw)) {
    throw new TypeError(`${filename} must contain an array`);
  }
  return raw.map(entry => ({ ...entry, code }));
}

async function main() {
  const scriptDir = path.dirname(fileURLToPath(import.meta.url));
  const guiRoot = path.resolve(scriptDir, "..");
  const baseline = [
    ...await readBaseline(scriptDir, "complexity-baseline.json", "eslint(complexity)"),
    ...await readBaseline(scriptDir, "max-depth-baseline.json", "eslint(max-depth)"),
  ];

  const diagnostics = runOxlintJson(guiRoot);
  const actual = summarizeStructuralDiagnostics(diagnostics);
  const errors = compareStructuralBaseline(actual, baseline);
  if (errors.length > 0) {
    console.error("Structural lint policy failed:");
    for (const error of errors) console.error(`- ${error}`);
    console.error("Targets are complexity <= 15 (modified) and max-depth <= 4. Refactor new/worsened debt; only shrink the central legacy baselines when debt is removed.");
    process.exitCode = 1;
    return;
  }

  const complexity = actual.filter(entry => entry.code === "eslint(complexity)").reduce((sum, entry) => sum + entry.count, 0);
  const depth = actual.filter(entry => entry.code === "eslint(max-depth)").reduce((sum, entry) => sum + entry.count, 0);
  console.log(`Structural lint policy passed (${complexity} legacy complexity, ${depth} legacy depth; targets 15/4).`);
}

const invokedPath = process.argv[1] ? pathToFileURL(path.resolve(process.argv[1])).href : null;
if (invokedPath === import.meta.url) {
  await main();
}
