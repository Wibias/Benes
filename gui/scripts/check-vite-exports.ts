/** Benes dashboard: assert live Vite transforms still export importer names. */

/**
 * tsc only sees disk. After a truncated write, Vite can serve an empty module
 * while tsc is green — the browser then throws
 * "The requested module ... doesn't provide an export named: '...'".
 *
 * Requires benes dev (default http://127.0.0.1:23200).
 * Override with BENES_DEV_ORIGIN.
 */
import { existsSync, readFileSync, statSync } from "node:fs";
import { dirname, join, relative, resolve } from "node:path";
import { fileURLToPath } from "node:url";

const root = join(dirname(fileURLToPath(import.meta.url)), "..");
const srcRoot = join(root, "src");
const origin = (process.env.BENES_DEV_ORIGIN ?? "http://127.0.0.1:23200").replace(/\/$/, "");

const SEED_FILES = [
  "src/pages/Dashboard.tsx",
  "src/pages/dashboard-plane-board.tsx",
  "src/pages/dashboard-core-poll.ts",
  "src/pages/use-dashboard-data.ts",
  "src/App.tsx",
  "src/formatUptime.ts",
  "src/format-tokens.ts",
  "src/startup-health-ui.ts",
];

const IMPORTERS = ["src/pages/Dashboard.tsx", "src/App.tsx"];

function namedFromClause(clause) {
  const inner = clause.replace(/^\{/, "").replace(/\}$/, "");
  const names = [];
  for (const part of inner.split(",")) {
    const bit = part.trim();
    if (!bit || bit.startsWith("type ")) continue;
    names.push(bit.split(/\s+as\s+/)[0].trim());
  }
  return names.filter(Boolean);
}

function parseRelativeImports(src) {
  const results = [];
  const re = /(?:^|[;\n])\s*import\s+([^;]+?)\s+from\s+["'](\.[^"']+)["']/g;
  let match;
  while ((match = re.exec(src))) {
    const clause = match[1].trim();
    const spec = match[2];
    if (clause.startsWith("type ")) continue;
    const names = [];
    if (clause.startsWith("{")) {
      names.push(...namedFromClause(clause));
    } else if (clause.includes("{")) {
      const def = clause.slice(0, clause.indexOf("{")).replace(/,$/, "").trim();
      if (def && def !== "*") names.push("default");
      names.push(...namedFromClause(clause.slice(clause.indexOf("{"))));
    } else if (!clause.startsWith("*")) {
      names.push("default");
    }
    if (names.length) results.push({ spec, names });
  }
  return results;
}

function resolveImport(fromAbs, spec) {
  const base = resolve(dirname(fromAbs), spec);
  const candidates = [
    base,
    base + ".tsx",
    base + ".ts",
    base + ".jsx",
    base + ".js",
    join(base, "index.tsx"),
    join(base, "index.ts"),
  ];
  return candidates.find((path) => existsSync(path) && statSync(path).isFile()) ?? null;
}

function toVitePath(abs) {
  return "/src/" + relative(srcRoot, abs).replaceAll("\\", "/");
}

function transformLooksEmpty(body) {
  const stripped = body.replace(/\/\/# sourceMappingURL=.*$/s, "").trim();
  if (stripped.length < 80) return true;
  const mapped = body.match(/sourceMappingURL=data:application\/json;base64,([A-Za-z0-9+/=]+)/);
  if (!mapped) return false;
  try {
    const map = JSON.parse(Buffer.from(mapped[1], "base64").toString("utf8"));
    const contents = map.sourcesContent;
    return Array.isArray(contents) && contents.length > 0
      && contents.every((chunk) => !String(chunk ?? "").trim());
  } catch {
    return false;
  }
}

function hasNamedExport(body, name) {
  if (name === "default") return /\bexport\s+default\b/.test(body);
  const escaped = name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
  return new RegExp(
    "export\\s+(?:async\\s+)?(?:function|const|class|let|var)\\s+" + escaped + "\\b"
      + "|export\\s*\\{[^}]*\\b" + escaped + "\\b",
  ).test(body);
}

const expected = new Map();

function addExpected(abs, names) {
  const key = toVitePath(abs);
  const set = expected.get(key) ?? new Set();
  for (const name of names) set.add(name);
  expected.set(key, set);
}

for (const rel of SEED_FILES) {
  const abs = join(root, rel);
  if (existsSync(abs)) addExpected(abs, []);
}

for (const rel of IMPORTERS) {
  const abs = join(root, rel);
  if (!existsSync(abs)) continue;
  for (const { spec, names } of parseRelativeImports(readFileSync(abs, "utf8"))) {
    const resolved = resolveImport(abs, spec);
    if (resolved) addExpected(resolved, names);
  }
}

addExpected(join(root, "src/pages/dashboard-plane-board.tsx"), ["DashboardPlaneBoard"]);

let probe;
try {
  probe = await fetch(origin + "/", { signal: AbortSignal.timeout(4000) });
} catch (err) {
  const detail = err.cause && err.cause.code ? err.cause.code : err.message;
  console.error("check:vite: Vite not reachable at " + origin + " (" + detail + ").");
  console.error("Start it with: benes dev --open");
  process.exit(2);
}
if (!probe.ok) {
  console.error("check:vite: GET " + origin + "/ -> " + probe.status);
  process.exit(2);
}

const failures = [];
console.log("check:vite " + origin);

for (const [vitePath, names] of [...expected.entries()].sort((a, b) => a[0].localeCompare(b[0]))) {
  const abs = join(srcRoot, vitePath.replace(/^\/src\//, ""));
  const diskBytes = existsSync(abs) ? statSync(abs).size : 0;
  if (diskBytes < 40) {
    failures.push(vitePath + ": disk file empty (" + diskBytes + " bytes)");
    console.log("  FAIL " + vitePath + "  disk " + diskBytes + "b");
    continue;
  }
  let res;
  try {
    res = await fetch(origin + vitePath, { signal: AbortSignal.timeout(8000) });
  } catch (err) {
    failures.push(vitePath + ": fetch failed (" + err.message + ")");
    console.log("  FAIL " + vitePath + "  fetch " + err.message);
    continue;
  }
  const body = await res.text();
  if (!res.ok) {
    failures.push(vitePath + ": HTTP " + res.status);
    console.log("  FAIL " + vitePath + "  HTTP " + res.status);
    continue;
  }
  if (transformLooksEmpty(body)) {
    failures.push(vitePath + ": empty Vite transform (disk " + diskBytes + "b -> vite " + body.length + "b)");
    console.log("  FAIL " + vitePath + "  empty transform  disk " + diskBytes + "b -> vite " + body.length + "b");
    continue;
  }
  const missing = [...names].filter((name) => !hasNamedExport(body, name));
  if (missing.length) {
    failures.push(vitePath + ": missing export " + missing.join(", ") + " (vite " + body.length + "b)");
    console.log("  FAIL " + vitePath + "  missing " + missing.join(", "));
    continue;
  }
  const label = names.size ? [...names].join(", ") : "non-empty";
  console.log("  ok   " + vitePath + "  disk " + diskBytes + "b -> vite " + body.length + "b  " + label);
}

if (failures.length) {
  console.error("\ncheck:vite failed (" + failures.length + "):");
  for (const line of failures) console.error("  - " + line);
  process.exit(1);
}

console.log("check:vite passed");
