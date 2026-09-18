import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { reachableRuntimeModules, viteRuntimeEntryPoints } from "./i18n-module-graph.ts";

function graph(files: Record<string, string>, entryPoints: string[]) {
  return reachableRuntimeModules(
    Object.entries(files).map(([path, source]) => ({ path, source })),
    entryPoints,
  );
}

test("runtime entry reaches an imported page", () => {
  const result = graph(
    {
      "src/main.tsx": `import App from "./App";\n`,
      "src/App.tsx": `import Live from "./pages/Live";\nexport default function App() { return <Live />; }\n`,
      "src/pages/Live.tsx": `export default function Live() { return null; }\n`,
      "src/pages/Orphan.tsx": `export default function Orphan() { return null; }\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.reachable.has("src/main.tsx"), true);
  assert.equal(result.reachable.has("src/App.tsx"), true);
  assert.equal(result.reachable.has("src/pages/Live.tsx"), true);
  assert.equal(result.reachable.has("src/pages/Orphan.tsx"), false);
});

test("type-only import does not establish runtime reachability", () => {
  const result = graph(
    {
      "src/main.tsx": `import type { Flag } from "./flags";\nimport { live } from "./live";\n`,
      "src/live.ts": `export const live = 1;\n`,
      "src/flags.ts": `export type Flag = "on";\nexport const unused = 2;\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.reachable.has("src/live.ts"), true);
  assert.equal(result.reachable.has("src/flags.ts"), false);
});

test("default import with type-only named bindings stays a runtime dependency", () => {
  const result = graph(
    {
      "src/main.tsx": `import Live, { type Flag } from "./live";\nexport const n = Live;\n`,
      "src/live.ts": `export default 1;\nexport type Flag = "on";\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.reachable.has("src/live.ts"), true);
});

test("named type-only bindings do not keep the module reachable", () => {
  const result = graph(
    {
      "src/main.tsx": `import { type Flag } from "./flags";\nexport const n = 1;\n`,
      "src/flags.ts": `export type Flag = "on";\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.reachable.has("src/flags.ts"), false);
});

test("static re-export keeps the runtime dependency reachable", () => {
  const result = graph(
    {
      "src/main.tsx": `import { live } from "./barrel";\n`,
      "src/barrel.ts": `export { live } from "./live";\n`,
      "src/live.ts": `export const live = 1;\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.reachable.has("src/barrel.ts"), true);
  assert.equal(result.reachable.has("src/live.ts"), true);
});

test("type-only re-export does not keep the source reachable", () => {
  const result = graph(
    {
      "src/main.tsx": `export type { Flag } from "./flags";\n`,
      "src/flags.ts": `export type Flag = "on";\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.reachable.has("src/flags.ts"), false);
});

test("named type-only re-export does not keep the source reachable", () => {
  const result = graph(
    {
      "src/main.tsx": `export { type Flag } from "./flags";\n`,
      "src/flags.ts": `export type Flag = "on";\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.reachable.has("src/flags.ts"), false);
});

test("static dynamic import keeps a module reachable", () => {
  const result = graph(
    {
      "src/main.tsx": `export function load() { return import("./lazy"); }\n`,
      "src/lazy.ts": `export const n = 1;\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.reachable.has("src/lazy.ts"), true);
  assert.equal(result.unsupportedLoads.length, 0);
});

test("finite conditional dynamic import follows both modules", () => {
  const result = graph(
    {
      "src/main.tsx": `export function load(ok: boolean) { return import(ok ? "./a" : "./b"); }\n`,
      "src/a.ts": `export const a = 1;\n`,
      "src/b.ts": `export const b = 2;\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.reachable.has("src/a.ts"), true);
  assert.equal(result.reachable.has("src/b.ts"), true);
  assert.equal(result.unsupportedLoads.length, 0);
});

test("non-literal dynamic import fails closed", () => {
  const result = graph(
    {
      "src/main.tsx": `export function load(getPath: () => string) { const path = getPath(); return import(path); }\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.unsupportedLoads.length, 1);
  assert.equal(result.unsupportedLoads[0]?.from, "src/main.tsx");
  assert.equal(result.unsupportedLoads[0]?.reason, "unsupported-dynamic-import");
  assert.equal(result.unsupportedLoads[0]?.detail, "path");
  assert.equal(result.unsupportedLoads[0]?.line, 1);
});

test("template dynamic import fails closed", () => {
  const result = graph(
    {
      "src/main.tsx": `export function load(name: string) { return import(\`./pages/\${name}.tsx\`); }\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.unsupportedLoads.length, 1);
  assert.equal(result.unsupportedLoads[0]?.from, "src/main.tsx");
  assert.equal(result.unsupportedLoads[0]?.reason, "unsupported-dynamic-import");
  assert.match(result.unsupportedLoads[0]?.detail ?? "", /pages/);
});

test("non-literal dynamic import in an orphan module does not invalidate the graph", () => {
  const result = graph(
    {
      "src/main.tsx": `export const n = 1;\n`,
      "src/orphan.ts": `export function load(getPath: () => string) { return import(getPath()); }\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.reachable.has("src/main.tsx"), true);
  assert.equal(result.reachable.has("src/orphan.ts"), false);
  assert.equal(result.unsupportedLoads.length, 0);
});

test("import.meta.glob in reachable source fails closed", () => {
  const result = graph(
    {
      "src/main.tsx": `export const pages = import.meta.glob("./pages/*.tsx");\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.unsupportedLoads.length, 1);
  assert.equal(result.unsupportedLoads[0]?.reason, "unsupported-runtime-loader");
  assert.match(result.unsupportedLoads[0]?.detail ?? "", /glob/);
});

test("extensionless and tsx index resolution work", () => {
  const result = graph(
    {
      "src/main.tsx": `import { n } from "./mod";\n`,
      "src/mod/index.ts": `export const n = 1;\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.reachable.has("src/mod/index.ts"), true);
});

test("TypeScript compiles src while translation liveness starts at the Vite runtime entry", () => {
  const configPath = path.join(path.dirname(fileURLToPath(import.meta.url)), "..", "tsconfig.app.json");
  const source = readFileSync(configPath, "utf8");
  assert.equal(/"include"\s*:\s*\[\s*"src"\s*\]/.test(source), true);
  assert.deepEqual(viteRuntimeEntryPoints(), ["src/main.tsx"]);
});

test("mixed type and value named imports keep the runtime module reachable", () => {
  const result = graph(
    {
      "src/main.tsx": `import { type Flag, live } from "./flags";\nexport const n = live;\n`,
      "src/flags.ts": `export type Flag = "on";\nexport const live = 1;\n`,
    },
    ["src/main.tsx"],
  );
  assert.equal(result.reachable.has("src/flags.ts"), true);
});
