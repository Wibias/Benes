import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import test from "node:test";

import { en } from "../src/i18n/en.ts";
import {
  MEMORY_OBSERVABILITY_KEYS,
  TRANSLATED_LOCALES,
  analyzeGuiCatalog,
  analyzeKeyLiveness,
  extractCatalogKeys,
  extractTopLevelExportKeys,
  guiRootFromScript,
  livenessGateFailures,
  localeParity,
  removeCatalogKeysFromSource,
  sourceOnlyKeys,
  statusOf,
  unusedKeys,
} from "./i18n-key-liveness.ts";

function analyze(keys: string[], files: Array<{ path: string; source: string }>, catalogPaths?: string[]) {
  return analyzeKeyLiveness({ canonicalKeys: keys, files, catalogPaths });
}

test("literal t(\"key\") is live", () => {
  const report = analyze(["live.key", "dead.key"], [
    { path: "src/pages/example.tsx", source: `export function Title({ t }: { t: (k: string) => string }) { return t("live.key"); }\n` },
  ]);
  assert.equal(statusOf(report, "live.key"), "LIVE_STATIC");
  assert.equal(report.keys["live.key"]?.evidence[0]?.kind, "t-literal");
});

test("typed TKey lookup-map entry is live", () => {
  const report = analyze(["theme.light", "dead.key"], [
    {
      path: "src/app-sidebar.tsx",
      source: `type TKey = "theme.light" | "dead.key";
const THEME_TKEY: Record<string, TKey> = { light: "theme.light" };
`,
    },
  ]);
  assert.equal(statusOf(report, "theme.light"), "LIVE_TYPED_REFERENCE");
});

test("static metadata nameKey/labelKey is live", () => {
  const report = analyze(["integrations.tab.claude", "dead.key"], [
    {
      path: "src/pages/harnesses/catalog.ts",
      source: `export const claude = { nameKey: "integrations.tab.claude" as const, labelKey: "integrations.tab.claude" };\n`,
    },
  ]);
  assert.equal(statusOf(report, "integrations.tab.claude"), "LIVE_TYPED_REFERENCE");
});

test("catalogue self-reference does not mark a key live", () => {
  const report = analyze(["catalog.only", "also.dead"], [
    {
      path: "src/i18n/en.ts",
      source: `export const en = { "catalog.only": "Catalog", "also.dead": "Dead" };\n`,
    },
    {
      path: "src/i18n/de.ts",
      source: `export const de = { "catalog.only": "Katalog", "also.dead": "Tot" };\n`,
    },
  ]);
  assert.equal(statusOf(report, "catalog.only"), "UNUSED_PROVEN");
  assert.equal(statusOf(report, "also.dead"), "UNUSED_PROVEN");
});

test("a genuinely unreferenced canonical key becomes UNUSED_PROVEN", () => {
  const report = analyze(["used.key", "unused.key"], [
    { path: "src/ui.tsx", source: `t("used.key");\n` },
  ]);
  assert.equal(statusOf(report, "unused.key"), "UNUSED_PROVEN");
  assert.equal(statusOf(report, "used.key"), "LIVE_STATIC");
});

test("ambiguous dynamic translation access does not become safely deletable", () => {
  const report = analyze(["dyn.alpha", "dyn.beta", "other.dead"], [
    {
      path: "src/pages/models.tsx",
      source: `export function label(t: (k: string) => string, item: string) { return t(\`dyn.\${item}\` as TKey); }\n`,
    },
  ]);
  assert.equal(statusOf(report, "dyn.alpha"), "UNRESOLVED_DYNAMIC");
  assert.equal(statusOf(report, "dyn.beta"), "UNRESOLVED_DYNAMIC");
  assert.equal(statusOf(report, "other.dead"), "UNUSED_PROVEN");
});

test("interpolation values in t() vars do not classify keys", () => {
  const report = analyze(["quota.resetsInCompact", "dash.mem.observed"], [
    {
      path: "src/provider-workspace/quota-presentation.ts",
      source: `t("quota.resetsInCompact", { wait: \`\${days}d\` });\n`,
    },
  ]);
  assert.equal(statusOf(report, "quota.resetsInCompact"), "LIVE_STATIC");
  assert.equal(statusOf(report, "dash.mem.observed"), "UNUSED_PROVEN");
});

test("historical alias-route title keys remain live where routing still exposes the alias", () => {
  const report = analyze(["nav.integrations", "nav.harnesses"], [
    {
      path: "src/app-shell.ts",
      source: `type TKey = "nav.integrations" | "nav.harnesses";
export const PAGE_TKEY: Record<string, TKey> = { integrations: "nav.integrations", harnesses: "nav.harnesses" };
`,
    },
  ]);
  assert.equal(statusOf(report, "nav.integrations"), "LIVE_TYPED_REFERENCE");
});

test("Trans k= and catalogValue literals are live", () => {
  const report = analyze(["dash.runStart", "uptime.day"], [
    { path: "src/pages/Dashboard.tsx", source: `export const n = <Trans k="dash.runStart" cmd="benes start" />;\n` },
    { path: "src/formatUptime.ts", source: `catalogValue(locale, "uptime.day");\n` },
  ]);
  assert.equal(statusOf(report, "dash.runStart"), "LIVE_STATIC");
  assert.equal(statusOf(report, "uptime.day"), "LIVE_STATIC");
});

test("locale parity rejects missing entries", () => {
  const findings = localeParity(new Set(["a", "b"]), {
    de: new Set(["a"]),
  });
  assert.deepEqual(findings, [{ locale: "de", missing: ["b"], extra: [] }]);
});

test("locale parity rejects extra/stale entries", () => {
  const findings = localeParity(new Set(["a"]), {
    de: new Set(["a", "stale.key"]),
  });
  assert.deepEqual(findings, [{ locale: "de", missing: [], extra: ["stale.key"] }]);
});

test("extractCatalogKeys reads string-literal object keys", () => {
  const keys = extractCatalogKeys(`export const de = { "nav.dashboard": "Übersicht", 'theme.light': "Hell" };`);
  assert.deepEqual([...keys].sort(), ["nav.dashboard", "theme.light"]);
});

test("extractTopLevelExportKeys ignores nested locale codes", () => {
  const keys = extractTopLevelExportKeys(
    `export const localeOverrides = { "nav.dashboard": { de: "Übersicht", fr: "Tableau de bord" } };`,
    "localeOverrides",
  );
  assert.deepEqual([...keys], ["nav.dashboard"]);
});

test("removeCatalogKeysFromSource deletes only named entries and keeps survivors byte-for-byte", () => {
  const source = [
    "export const en = {",
    '  "keep.one": "Keep",',
    '  "drop.me": "Gone",',
    '  "keep.two": "Also",',
    "};",
    "",
  ].join("\n");
  const next = removeCatalogKeysFromSource(source, new Set(["drop.me"]));
  assert.equal(
    next,
    [
      "export const en = {",
      '  "keep.one": "Keep",',
      '  "keep.two": "Also",',
      "};",
      "",
    ].join("\n"),
  );
});

test("finite conditional assigned to t(key) retains both keys", () => {
  const report = analyze(["a.ok", "a.fail", "a.dead"], [
    {
      path: "src/page.ts",
      source: `export function label(t: (k: string) => string, result: { ok: boolean }) {
  const key = result.ok ? "a.ok" : "a.fail";
  return t(key);
}
`,
    },
  ]);
  assert.equal(statusOf(report, "a.ok"), "LIVE_STATIC");
  assert.equal(statusOf(report, "a.fail"), "LIVE_STATIC");
  assert.equal(statusOf(report, "a.dead"), "UNUSED_PROVEN");
});

test("finite helper result passed to t(key) retains the union", () => {
  const report = analyze(["a.ok", "a.fail", "a.dead"], [
    {
      path: "src/page.ts",
      source: `function statusKey(ok: boolean) {
  return ok ? "a.ok" : "a.fail";
}
export function label(t: (k: string) => string, ok: boolean) {
  return t(statusKey(ok));
}
`,
    },
  ]);
  assert.equal(statusOf(report, "a.ok"), "LIVE_STATIC");
  assert.equal(statusOf(report, "a.fail"), "LIVE_STATIC");
  assert.equal(statusOf(report, "a.dead"), "UNUSED_PROVEN");
});

test("typed map lookup passed to t() retains its values", () => {
  const report = analyze(["state.on", "state.off", "state.dead"], [
    {
      path: "src/page.ts",
      source: `type TKey = "state.on" | "state.off" | "state.dead";
const LABELS: Record<"on" | "off", TKey> = { on: "state.on", off: "state.off" };
export function label(t: (k: string) => string, state: "on" | "off") {
  return t(LABELS[state]);
}
`,
    },
  ]);
  assert.equal(statusOf(report, "state.on"), "LIVE_STATIC");
  assert.equal(statusOf(report, "state.off"), "LIVE_STATIC");
  assert.equal(statusOf(report, "state.dead"), "UNUSED_PROVEN");
});

test("unbounded dynamic key produces explicit unresolved evidence", () => {
  const report = analyze(["keep.me", "other.dead"], [
    {
      path: "src/page.ts",
      source: `export function label(t: (k: string) => string, key: string) {
  return t(key);
}
`,
    },
  ]);
  assert.ok(report.unboundedCalls.length > 0);
  assert.equal(statusOf(report, "other.dead"), "UNUSED_PROVEN");
});

test("unrelated exact canonical literal is conservative, not typed", () => {
  const report = analyze(["nav.dashboard", "dead.key"], [
    {
      path: "src/page.ts",
      source: `export const note = "nav.dashboard";\n`,
    },
  ]);
  assert.equal(statusOf(report, "nav.dashboard"), "LIVE_CONSERVATIVE_LITERAL");
  assert.equal(report.keys["nav.dashboard"]?.evidence[0]?.kind, "string-literal");
});

test("runtime entry imported page translation key is live", () => {
  const report = analyzeKeyLiveness({
    canonicalKeys: ["page.title", "dead.key"],
    entryPoints: ["src/main.tsx"],
    files: [
      { path: "src/main.tsx", source: `import { Page } from "./pages/Live";\nexport const n = Page;\n` },
      { path: "src/pages/Live.ts", source: `export function Page(t: (k: string) => string) { return t("page.title"); }\n` },
    ],
  });
  assert.equal(statusOf(report, "page.title"), "LIVE_STATIC");
  assert.equal(statusOf(report, "dead.key"), "UNUSED_PROVEN");
});

test("type-only import of a translation module does not keep its keys live", () => {
  const report = analyzeKeyLiveness({
    canonicalKeys: ["typed.only"],
    entryPoints: ["src/main.tsx"],
    files: [
      { path: "src/main.tsx", source: `import type { Label } from "./labels";\nexport const n = 1;\n` },
      { path: "src/labels.ts", source: `export type Label = string;\nexport function label(t: (k: string) => string) { return t("typed.only"); }\n` },
    ],
  });
  assert.equal(statusOf(report, "typed.only"), "SOURCE_ONLY_REFERENCE");
});

test("livenessGateFailures fails closed on unused, unbounded, and analysis errors", () => {
  const unused = analyze(["dead.key"], [{ path: "src/page.ts", source: `export const n = 1;\n` }]);
  assert.ok(livenessGateFailures(unused).some((line) => line.includes("unused canonical keys")));
  const unbounded = analyze(["keep.me"], [{
    path: "src/page.ts",
    source: `export function label(t: (k: string) => string, key: string) { return t(key); }\n`,
  }]);
  assert.ok(livenessGateFailures(unbounded).some((line) => line.includes("unbounded translation calls")));
  const broken = analyzeKeyLiveness({
    canonicalKeys: ["a.key"],
    entryPoints: ["src/missing.tsx"],
    files: [{ path: "src/page.ts", source: `export const n = 1;\n` }],
  });
  assert.ok(livenessGateFailures(broken).some((line) => line.includes("runtime entry points resolved to no modules")));
  const dynamic = analyzeKeyLiveness({
    canonicalKeys: ["live.key"],
    entryPoints: ["src/main.tsx"],
    files: [
      { path: "src/main.tsx", source: `export function load(path: string) { return import(path); }\nexport function live(t: (k: string) => string) { return t("live.key"); }\n` },
    ],
  });
  assert.ok(livenessGateFailures(dynamic).some((line) => line.includes("unsupported runtime module load")));
  assert.equal(statusOf(dynamic, "live.key"), "LIVE_STATIC");
});

test("orphan source containing t() is source-only, not runtime-live", () => {
  const report = analyzeKeyLiveness({
    canonicalKeys: ["live.key", "orphan.key"],
    entryPoints: ["src/main.tsx"],
    files: [
      { path: "src/main.tsx", source: `import { Live } from "./live";\nexport const n = Live;\n` },
      { path: "src/live.ts", source: `export function Live(t: (k: string) => string) { return t("live.key"); }\n` },
      { path: "src/orphan.ts", source: `export function Orphan(t: (k: string) => string) { return t("orphan.key"); }\n` },
    ],
  });
  assert.equal(statusOf(report, "live.key"), "LIVE_STATIC");
  assert.equal(statusOf(report, "orphan.key"), "SOURCE_ONLY_REFERENCE");
  assert.deepEqual(sourceOnlyKeys(report), ["orphan.key"]);
  assert.equal(unusedKeys(report).includes("orphan.key"), false);
});

test("untyped literal in orphan source does not retain the key", () => {
  const report = analyzeKeyLiveness({
    canonicalKeys: ["orphan.key"],
    entryPoints: ["src/main.tsx"],
    files: [
      { path: "src/main.tsx", source: `export const n = 1;\n` },
      { path: "src/orphan.ts", source: `export const note = "orphan.key";\n` },
    ],
  });
  assert.equal(statusOf(report, "orphan.key"), "UNUSED_PROVEN");
});

test("legacy integrations alias stays live while an unmounted Integrations page is source-only", () => {
  const report = analyzeKeyLiveness({
    canonicalKeys: ["nav.integrations", "integrations.dead"],
    entryPoints: ["src/main.tsx"],
    files: [
      {
        path: "src/main.tsx",
        source: `import { AppPage } from "./app-page";\nexport const n = AppPage;\n`,
      },
      {
        path: "src/app-page.tsx",
        source: `type TKey = "nav.integrations";
export const PAGE_TKEY: Record<string, TKey> = { integrations: "nav.integrations" };
export function AppPage() { return null; }
`,
      },
      {
        path: "src/pages/Integrations.tsx",
        source: `export default function Integrations(t: (k: string) => string) { return t("integrations.dead"); }\n`,
      },
    ],
  });
  assert.notEqual(statusOf(report, "nav.integrations"), "UNUSED_PROVEN");
  assert.notEqual(statusOf(report, "nav.integrations"), "SOURCE_ONLY_REFERENCE");
  assert.equal(statusOf(report, "integrations.dead"), "SOURCE_ONLY_REFERENCE");
});

test("broad TKey parameter without finite evidence is unbounded", () => {
  const report = analyze(["a.one", "a.two"], [
    {
      path: "src/page.ts",
      source: `type TKey = "a.one" | "a.two";
function label(t: (key: TKey) => string, key: TKey) {
  return t(key);
}
`,
    },
  ]);
  assert.ok(report.unboundedCalls.length > 0);
  assert.ok(livenessGateFailures(report).some((line) => line.includes("unbounded translation calls")));
  assert.equal(report.unboundedCalls[0]?.kind, "unbounded-call");
});

test("mixed finite and dynamic helper return is unbounded", () => {
  const report = analyze(["a.ok", "a.dead"], [
    {
      path: "src/page.ts",
      source: `function choose(ok: boolean, dynamic: string) {
  if (ok) return "a.ok";
  return dynamic;
}
export function label(t: (k: string) => string, ok: boolean, dynamic: string) {
  return t(choose(ok, dynamic));
}
`,
    },
  ]);
  assert.ok(report.unboundedCalls.length > 0);
  assert.ok(livenessGateFailures(report).some((line) => line.includes("unbounded translation calls")));
});

test("known plus unknown conditional branch is unbounded", () => {
  const report = analyze(["a.ok", "a.dead"], [
    {
      path: "src/page.ts",
      source: `export function label(t: (k: string) => string, ok: boolean, dynamic: string) {
  return t(ok ? "a.ok" : dynamic);
}
`,
    },
  ]);
  assert.ok(report.unboundedCalls.length > 0);
});

test("broad TKey variable with finite initializer retains the union", () => {
  const report = analyze(["a.ok", "a.fail", "a.dead"], [
    {
      path: "src/page.ts",
      source: `type TKey = "a.ok" | "a.fail" | "a.dead";
export function label(t: (k: TKey) => string, ok: boolean) {
  const key: TKey = ok ? "a.ok" : "a.fail";
  return t(key);
}
`,
    },
  ]);
  assert.equal(statusOf(report, "a.ok"), "LIVE_STATIC");
  assert.equal(statusOf(report, "a.fail"), "LIVE_STATIC");
  assert.equal(statusOf(report, "a.dead"), "UNUSED_PROVEN");
  assert.equal(report.unboundedCalls.length, 0);
});

test("broad TKey parameter without call-site proof is unbounded", () => {
  const report = analyze(["a.ok", "a.dead"], [
    {
      path: "src/page.ts",
      source: `type TKey = "a.ok" | "a.dead";
function render(t: (k: TKey) => string, key: TKey) {
  return t(key);
}
`,
    },
  ]);
  assert.ok(report.unboundedCalls.length > 0);
});

test("nested function returns do not count as outer-function returns", () => {
  const report = analyze(["a.inner", "a.dead"], [
    {
      path: "src/page.ts",
      source: `function outer(dynamic: string) {
  const inner = () => "a.inner";
  return dynamic;
}
export function label(t: (k: string) => string, dynamic: string) {
  return t(outer(dynamic));
}
`,
    },
  ]);
  assert.ok(report.unboundedCalls.length > 0);
  assert.notEqual(statusOf(report, "a.inner"), "LIVE_STATIC");
});

test("unreachable catalogue-like source cannot prove liveness", () => {
  const report = analyzeKeyLiveness({
    canonicalKeys: ["catalog.only"],
    entryPoints: ["src/main.tsx"],
    files: [
      { path: "src/main.tsx", source: `export const n = 1;\n` },
      { path: "src/i18n/en.ts", source: `export const en = { "catalog.only": "Catalog" };\n` },
    ],
  });
  assert.equal(statusOf(report, "catalog.only"), "UNUSED_PROVEN");
});

test("Memory Observability keys are unused unless a live product consumer exists", () => {
  const report = analyze([...MEMORY_OBSERVABILITY_KEYS, "nav.dashboard"], [
    { path: "src/i18n/en-base.mts", source: `export const en = { "dash.mem.title": "Memory observability", "nav.dashboard": "Dashboard" };\n` },
    { path: "src/app-shell.ts", source: `export const PAGE_TKEY = { dashboard: "nav.dashboard" };\n` },
  ]);
  for (const key of MEMORY_OBSERVABILITY_KEYS) {
    assert.equal(statusOf(report, key), "UNUSED_PROVEN", key);
  }
  assert.equal(statusOf(report, "nav.dashboard"), "LIVE_CONSERVATIVE_LITERAL");
});

test("product catalogue is live, parity-clean, and does not revive unused Memory Observability keys", async () => {
  const guiRoot = guiRootFromScript();
  const canonicalKeys = Object.keys(en);
  const report = await analyzeGuiCatalog(guiRoot, canonicalKeys);
  assert.deepEqual(livenessGateFailures(report), []);
  assert.deepEqual(report.analysisErrors, []);
  assert.deepEqual(report.unboundedCalls, []);
  assert.deepEqual(unusedKeys(report), []);
  assert.notEqual(statusOf(report, "nav.integrations"), "UNUSED_PROVEN");
  assert.equal(statusOf(report, "models.v2Help"), "LIVE_STATIC");
  const present = new Set(canonicalKeys);
  for (const key of MEMORY_OBSERVABILITY_KEYS) {
    if (present.has(key)) {
      assert.notEqual(statusOf(report, key), "UNUSED_PROVEN", key);
    }
  }
  const overrideSource = await readFile(path.join(guiRoot, "src", "i18n", "locale-overrides.ts"), "utf8");
  const overrideKeys = extractTopLevelExportKeys(overrideSource, "localeOverrides");
  const extraOverrides = [...overrideKeys].filter((key) => !canonicalKeys.includes(key)).sort();
  assert.deepEqual(extraOverrides, []);
});
