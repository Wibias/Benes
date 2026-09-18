import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { describeRefusal } from "../src/pages/integrations/refusal-copy.ts";
import { managementErrorMessage } from "../src/pages/harnesses/claude-settings-error.ts";
import { modelsCatalogFromResponse, readClaudeSettings, saveClaudeSettings } from "../src/pages/harnesses/claude-settings-io.ts";
import {
  AUTO_COMPACT_DEFAULT,
  CATALOG_LOAD_FAILED,
  CLAUDE_SETTINGS_FORBIDDEN_PUT_KEYS,
  acceptSettingsWrite,
  catalogPickerOptions,
  claudeSettingsDirty,
  claudeSettingsPutBody,
  compactInputToOverride,
  decodeClaudeSettings,
  decodeModelsPayload,
  emptyClaudeSettingsDraft,
  familyWriteObject,
  formatCompactTokens,
} from "../src/pages/harnesses/claude-settings-state.ts";
import { harnessHash, harnessShowsSettings, parseHarnessHash } from "../src/pages/harnesses/hash.ts";

const ROOT = path.join(path.dirname(fileURLToPath(import.meta.url)), "..");
const HARNESSES_DIR = path.join(ROOT, "src/pages/harnesses");

const LEGACY_CLAUDE_IMPORTS = [
  "Claude.tsx",
  "ClaudeCode.tsx",
  "ClaudeDesktop.tsx",
  "claude-autoconnect.ts",
  "claude-code-actions.ts",
  "claude-code-decode.ts",
  "claude-code-helper-options.ts",
  "claude-code-sections.tsx",
  "claude-code-settings.tsx",
  "claude-code-sidecar.ts",
  "claude-code-types.ts",
  "claude-code-workspace.tsx",
  "claude-desktop-actions.ts",
  "claude-desktop-board.tsx",
  "claude-desktop-decode.ts",
  "claude-desktop-lane-section.tsx",
  "claude-desktop-lane.ts",
  "claude-desktop-model-card.tsx",
  "claude-desktop-profile.ts",
  "claude-desktop-status-bar.tsx",
  "claude-manual-env.ts",
  "use-claude-code-page.ts",
  "use-claude-desktop-page.ts",
];

function harnessesSources(): string[] {
  return readdirSync(HARNESSES_DIR)
    .filter((name) => /\.(ts|tsx)$/.test(name))
    .map((name) => path.join(HARNESSES_DIR, name));
}

function walkTs(dir: string): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    const full = path.join(dir, name);
    if (statSync(full).isDirectory()) {
      out.push(...walkTs(full));
      continue;
    }
    if (/\.(ts|tsx)$/.test(name)) out.push(full);
  }
  return out;
}

test("current Harnesses sources must not import the inherited Claude workspace", () => {
  const forbidden = LEGACY_CLAUDE_IMPORTS.map((name) => name.replace(/\.[^.]+$/, ""));
  for (const file of harnessesSources()) {
    const source = readFileSync(file, "utf8");
    for (const name of forbidden) {
      assert.equal(
        source.includes(`/${name}`),
        false,
        `${path.basename(file)} imports inherited Claude module ${name}`,
      );
    }
  }
});

test("superseded Claude workspace files are deleted and debug inbound stays independent", () => {
  const pages = path.join(ROOT, "src/pages");
  for (const name of LEGACY_CLAUDE_IMPORTS) {
    assert.equal(existsSync(path.join(pages, name)), false, name);
  }
  assert.equal(existsSync(path.join(ROOT, "src/styles-claudecode-workspace.css")), false);
  const inbound = path.join(pages, "debug-claude-inbound-panel.tsx");
  assert.equal(existsSync(inbound), true);
  const forbidden = LEGACY_CLAUDE_IMPORTS.map((file) => file.replace(/\.[^.]+$/, ""));
  for (const file of walkTs(path.join(ROOT, "src"))) {
    if (file.includes(`${path.sep}i18n${path.sep}`)) continue;
    const source = readFileSync(file, "utf8");
    for (const name of forbidden) {
      const imported = new RegExp(`from\\s+['"][^'"]*/${name.replace(/[.*+?^${}()|[\]\\]/g, "\\$&")}['"]`).test(source);
      assert.equal(imported, false, `${path.relative(ROOT, file)} imports inherited Claude module ${name}`);
    }
  }
});

test("auto-compaction default stays 829800 and displays as 830k", () => {
  const env = readFileSync(path.join(ROOT, "../internal/claude/env.go"), "utf8");
  assert.equal(env.includes("autoCompactDefault = 829_800"), true);
  assert.equal(`${Math.round(829_800 / 1_000)}k`, "830k");
});

test("Harnesses toast refusal copy surfaces the exact Error message", () => {
  const t = (key: string) => key;
  assert.equal(
    describeRefusal(t, new Error('authMode must be "auto", "proxy", or "subscription"'), "harnesses.actionFailed"),
    'authMode must be "auto", "proxy", or "subscription"',
  );
});

test("managementErrorMessage prefers nested error.message", () => {
  assert.equal(
    managementErrorMessage(
      { error: { code: "invalid_body", message: 'authMode must be "auto", "proxy", or "subscription"' } },
      "fallback",
    ),
    'authMode must be "auto", "proxy", or "subscription"',
  );
  assert.equal(
    managementErrorMessage({ error: "legacy string refusal" }, "fallback"),
    "legacy string refusal",
  );
  assert.equal(
    managementErrorMessage({ message: "top-level message" }, "fallback"),
    "top-level message",
  );
  assert.equal(managementErrorMessage({ error: { code: "invalid_body" } }, "fallback"), "fallback");
  assert.equal(managementErrorMessage(null, "fallback"), "fallback");
  assert.equal(managementErrorMessage("plain", "fallback"), "plain");
});

test("current Harness detail keeps Apply/Disable on the overview head", () => {
  const detail = readFileSync(path.join(HARNESSES_DIR, "HarnessDetail.tsx"), "utf8");
  assert.equal(detail.includes("harnesses.disable"), true);
  assert.equal(detail.includes("harnesses.apply"), true);
  assert.equal(detail.includes("harnesses-connect"), true);
});

test("only Claude and Claude Desktop get Overview and Settings tabs", () => {
  assert.equal(harnessShowsSettings("claude"), true);
  assert.equal(harnessShowsSettings("grok"), false);
  assert.equal(harnessShowsSettings("codex"), false);
  assert.equal(harnessShowsSettings("claude-desktop"), true);
  const detail = readFileSync(path.join(HARNESSES_DIR, "HarnessDetail.tsx"), "utf8");
  assert.equal(detail.includes("harnessShowsSettings(harness.id)"), true);
  assert.equal(detail.includes("ClaudeDetailTabs"), true);
  assert.equal(detail.includes("SectionTabs"), false);
  assert.equal(detail.includes("settingsOpen && apiBase"), false);
  assert.equal(detail.includes("ClaudeSettings"), true);
});

test("harness hash keeps overview as the default deep link", () => {
  assert.deepEqual(parseHarnessHash("#harnesses/claude"), { id: "claude", tab: "overview" });
  assert.deepEqual(parseHarnessHash("#harnesses/claude/settings"), { id: "claude", tab: "settings" });
  assert.deepEqual(parseHarnessHash("#harnesses/claude-desktop"), { id: "claude-desktop", tab: "overview" });
  assert.deepEqual(parseHarnessHash("#harnesses/codex"), { id: "codex", tab: "overview" });
  assert.deepEqual(parseHarnessHash("#harnesses/codex/settings"), { id: "codex", tab: "overview" });
  assert.equal(harnessHash("claude", "settings"), "harnesses/claude/settings");
  assert.equal(harnessHash("codex", "settings"), "harnesses/codex");
  const page = readFileSync(
    path.join(path.dirname(fileURLToPath(import.meta.url)), "../src/pages/harnesses/Harnesses.tsx"),
    "utf8",
  );
  assert.equal(page.includes("parseHarnessHash()"), false);
  assert.equal(page.includes("parseHarnessHash(window.location.hash)"), true);
});

test("GET decode keeps only the approved editable contract and ignores retirements", () => {
  const got = decodeClaudeSettings({
    enabled: false,
    authMode: "proxy",
    systemEnv: true,
    fastMode: true,
    autoContext: false,
    injectAgents: false,
    smallFastModel: "gpt-5.4-mini",
    autoCompactWindow: null,
    blockedSkills: ["web-search"],
    aliases: [{ id: "opus" }],
    available: ["should-not-use"],
    webSearchSidecar: { backend: "openai" },
    visionSidecar: { backend: "openai" },
    modelMap: { opus: "from-map", "claude-3": "intercepted", sonnet: "from-map-sonnet" },
    tierModels: { opus: "canonical-opus", haiku: "canonical-haiku" },
    port: 18080,
  });
  assert.equal(got.port, 18080);
  assert.equal(got.draft.authMode, "proxy");
  assert.equal(got.draft.autoContext, false);
  assert.equal(got.draft.injectAgents, false);
  assert.equal(got.draft.smallFastModel, "gpt-5.4-mini");
  assert.equal(got.draft.autoCompactWindow, null);
  assert.equal(got.draft.opus, "canonical-opus");
  assert.equal(got.draft.sonnet, "from-map-sonnet");
  assert.equal(got.draft.haiku, "canonical-haiku");
  assert.equal(got.draft.fable, "");
  assert.equal("enabled" in got.draft, false);
  assert.equal("systemEnv" in got.draft, false);
  assert.equal("available" in got.draft, false);
});

test("auth and autoContext defaults stay clean", () => {
  const got = decodeClaudeSettings({});
  assert.deepEqual(got.draft, emptyClaudeSettingsDraft());
  assert.equal(claudeSettingsDirty(got.draft, emptyClaudeSettingsDraft()), false);
  const auto = decodeClaudeSettings({ authMode: "auto", autoContext: true, injectAgents: true });
  assert.equal(auto.draft.authMode, "auto");
  assert.equal(auto.draft.autoContext, true);
  assert.equal(claudeSettingsDirty(emptyClaudeSettingsDraft(), auto.draft), false);
});

test("null compact window presents the effective default without dirtying", () => {
  const baseline = decodeClaudeSettings({ autoCompactWindow: null }).draft;
  assert.equal(baseline.autoCompactWindow, null);
  assert.equal(formatCompactTokens(AUTO_COMPACT_DEFAULT), "830k");
  assert.equal(compactInputToOverride("829800", null), null);
  assert.equal(compactInputToOverride("830k", null), 830_000);
  assert.equal(claudeSettingsDirty(baseline, { ...baseline, autoCompactWindow: null }), false);
});

test("unrelated save does not materialize 829800 and reset writes null", () => {
  const baseline = decodeClaudeSettings({
    authMode: "auto",
    autoCompactWindow: null,
    smallFastModel: "",
  }).draft;
  const authOnly = { ...baseline, authMode: "proxy" as const };
  const body = claudeSettingsPutBody(baseline, authOnly);
  assert.deepEqual(body, { authMode: "proxy" });
  assert.equal("autoCompactWindow" in body, false);
  const reset = claudeSettingsPutBody(
    { ...baseline, autoCompactWindow: 350_000 },
    { ...baseline, autoCompactWindow: null },
  );
  assert.equal(reset.autoCompactWindow, null);
});

test("explicit compact and helper writes follow the server contract", () => {
  const baseline = emptyClaudeSettingsDraft();
  assert.equal(compactInputToOverride("350000", null), 350_000);
  const compact = claudeSettingsPutBody(baseline, { ...baseline, autoCompactWindow: 350_000 });
  assert.equal(compact.autoCompactWindow, 350_000);
  const helperOn = claudeSettingsPutBody(baseline, { ...baseline, smallFastModel: "gpt-5.4-mini" });
  assert.equal(helperOn.smallFastModel, "gpt-5.4-mini");
  const helperOff = claudeSettingsPutBody(
    { ...baseline, smallFastModel: "gpt-5.4-mini" },
    { ...baseline, smallFastModel: "" },
  );
  assert.equal(helperOff.smallFastModel, "");
});

test("family save uses only Opus Sonnet Haiku Fable on tierModels", () => {
  const baseline = emptyClaudeSettingsDraft();
  const draft = {
    ...baseline,
    opus: "gpt-5.6-sol",
    haiku: "gpt-5.6-mini",
  };
  const body = claudeSettingsPutBody(baseline, draft);
  assert.deepEqual(body.tierModels, { opus: "gpt-5.6-sol", haiku: "gpt-5.6-mini" });
  assert.equal("modelMap" in body, false);
  assert.deepEqual(familyWriteObject(draft), { opus: "gpt-5.6-sol", haiku: "gpt-5.6-mini" });
  const decoded = decodeClaudeSettings({
    modelMap: { opus: "keep", "claude-3-opus-20240229": "intercepted" },
  });
  assert.equal(decoded.draft.opus, "keep");
  assert.equal((decoded.draft as { ["claude-3"]?: string })["claude-3"], undefined);
});

test("save payload never includes retired or connection fields", () => {
  const baseline = emptyClaudeSettingsDraft();
  const body = claudeSettingsPutBody(baseline, {
    ...baseline,
    authMode: "subscription",
    autoContext: false,
    injectAgents: false,
    smallFastModel: "gpt-5.4-mini",
    opus: "gpt-5.6-sol",
    sonnet: "gpt-5.6-sol",
    haiku: "gpt-5.6-mini",
    fable: "gpt-5.6-terra",
    autoCompactWindow: 400_000,
  });
  for (const key of CLAUDE_SETTINGS_FORBIDDEN_PUT_KEYS) {
    assert.equal(key in body, false, key);
  }
  assert.deepEqual(Object.keys(body).toSorted(), [
    "authMode",
    "autoCompactWindow",
    "autoContext",
    "injectAgents",
    "smallFastModel",
    "tierModels",
  ]);
});

test("successful save returns the form to the refetched clean baseline", () => {
  const previous = decodeClaudeSettings({ authMode: "proxy", smallFastModel: "old" }).draft;
  const next = decodeClaudeSettings({ authMode: "subscription", smallFastModel: "" }).draft;
  assert.equal(claudeSettingsDirty(previous, next), true);
  assert.equal(claudeSettingsDirty(next, next), false);
});

test("catalog picker uses namespaced as the stored option value", () => {
  const options = catalogPickerOptions([
    { provider: "openai-apikey", id: "gpt-5.6-sol", namespaced: "openai-apikey/gpt-5.6-sol", disabled: false },
    { provider: "gpt-5.6", id: "gpt-5.6", namespaced: "gpt-5.6", disabled: false },
    { provider: "openai", id: "gpt-5.6", namespaced: "openai/gpt-5.6", disabled: false },
    { provider: "xai", id: "gpt-5.6", namespaced: "xai/gpt-5.6", disabled: false },
    { provider: "combo", id: "skip", namespaced: "combo/skip", disabled: false },
    { provider: "openai-apikey", id: "hidden", namespaced: "openai-apikey/hidden", disabled: true },
    { provider: "legacy", id: "no-ns", disabled: false },
  ], ["kept-extra"]);
  assert.deepEqual(options.map((row) => row.value), [
    "openai-apikey/gpt-5.6-sol",
    "gpt-5.6",
    "openai/gpt-5.6",
    "xai/gpt-5.6",
    "legacy/no-ns",
    "kept-extra",
  ]);
  assert.equal(options.some((row) => row.value === "gpt-5.6/gpt-5.6"), false);
  assert.equal(options.filter((row) => row.id === "gpt-5.6").length, 3);
});

test("models catalog failure does not become an empty picker", () => {
  assert.throws(
    () => modelsCatalogFromResponse(false, { error: { code: "unavailable", message: "models down" } }),
    { message: "models down" },
  );
  assert.throws(
    () => modelsCatalogFromResponse(false, null),
    { message: CATALOG_LOAD_FAILED },
  );
  assert.throws(() => decodeModelsPayload({ error: { code: "invalid_body" } }), { message: CATALOG_LOAD_FAILED });
  assert.throws(() => decodeModelsPayload("nope"), { message: CATALOG_LOAD_FAILED });
  assert.deepEqual(decodeModelsPayload([]), []);
  assert.deepEqual(modelsCatalogFromResponse(true, { models: [{ id: "ok" }] }), [{ id: "ok" }]);
  const settings = readFileSync(path.join(HARNESSES_DIR, "claude-settings.tsx"), "utf8");
  const io = readFileSync(path.join(HARNESSES_DIR, "claude-settings-io.ts"), "utf8");
  assert.equal(settings.includes("modelsRes.ok ? await modelsRes.json()"), false);
  assert.equal(io.includes("modelsCatalogFromResponse"), true);
});

test("stale save completions do not commit after a newer Settings load", () => {
  assert.equal(acceptSettingsWrite(4, 4), true);
  assert.equal(acceptSettingsWrite(5, 4), false);
  assert.equal(acceptSettingsWrite(4, 4, true), false);
  const settings = readFileSync(path.join(HARNESSES_DIR, "claude-settings.tsx"), "utf8");
  const io = readFileSync(path.join(HARNESSES_DIR, "claude-settings-io.ts"), "utf8");
  assert.equal(settings.includes("acceptSettingsWrite"), true);
  assert.equal(io.includes("signal"), true);
  assert.equal(io.includes("saveClaudeSettings"), true);
});

test("compaction input keeps inherited Default and blocks invalid Save", () => {
  const settings = readFileSync(path.join(HARNESSES_DIR, "claude-settings.tsx"), "utf8");
  const css = readFileSync(path.join(ROOT, "src/styles-harnesses.css"), "utf8");
  assert.equal(settings.includes("formatCompactTokens("), false);
  assert.equal(settings.includes("CompactField"), true);
  assert.equal(settings.includes("compactLadderValues"), false);
  assert.equal(settings.includes("harnesses.claude.context.compactDefault"), true);
  assert.equal(settings.includes("harnesses.claude.context.compactReset"), true);
  assert.equal(settings.includes("harnesses.claude.context.compactInvalid"), true);
  assert.equal(settings.includes("navigateHash"), true);
  assert.equal(settings.includes("agents.manage"), true);
  assert.equal(settings.includes("harnesses-claude-save"), true);
  assert.equal(settings.includes("harnesses-claude-row--helper"), true);
  assert.equal(settings.includes("<details open"), false);
  assert.equal(settings.includes("harnesses-claude-manual"), true);
  assert.equal(settings.includes("harnesses-claude-family-grid"), false);
  assert.equal(settings.includes("harnesses-claude-row--family"), true);
  assert.equal(css.includes("minmax(0, 58%) 220px"), true);
  assert.equal(css.includes("justify-self: end"), true);
  assert.equal(css.includes("harnesses-claude-save"), true);
  assert.equal(css.includes("harnesses-claude-row--helper"), true);
  assert.equal(css.includes("harnesses-claude-family-grid"), false);
  assert.equal(css.includes("harnesses-claude-compact-error"), true);
  const baseline = decodeClaudeSettings({ autoCompactWindow: null }).draft;
  assert.equal(compactInputToOverride("829800", null), null);
  assert.equal(compactInputToOverride("99999", null), "invalid");
  assert.equal(claudeSettingsDirty(baseline, { ...baseline, autoCompactWindow: null }), false);
  const reset = claudeSettingsPutBody(
    { ...baseline, autoCompactWindow: 350_000 },
    { ...baseline, autoCompactWindow: null },
  );
  assert.equal(reset.autoCompactWindow, null);
});

test("readClaudeSettings fails closed when the models request or payload is unusable", async () => {
  const orig = globalThis.fetch;
  const json = (status: number, body: unknown) =>
    new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
  try {
    globalThis.fetch = async (input) => {
      const url = String(input);
      if (url.endsWith("/api/claude-code")) return json(200, { authMode: "auto" });
      if (url.endsWith("/api/models")) return json(503, { error: { message: "catalog refused" } });
      throw new Error(url);
    };
    await assert.rejects(() => readClaudeSettings("http://127.0.0.1:23100"), { message: "catalog refused" });

    globalThis.fetch = async (input) => {
      const url = String(input);
      if (url.endsWith("/api/claude-code")) return json(200, { authMode: "auto" });
      if (url.endsWith("/api/models")) return json(200, { error: { code: "invalid_body" } });
      throw new Error(url);
    };
    await assert.rejects(() => readClaudeSettings("http://127.0.0.1:23100"), { message: CATALOG_LOAD_FAILED });
  } finally {
    globalThis.fetch = orig;
  }
});

test("saveClaudeSettings does not complete after abort", async () => {
  const orig = globalThis.fetch;
  const ac = new AbortController();
  ac.abort();
  try {
    globalThis.fetch = async (_input, init) => {
      if (init?.signal?.aborted) {
        throw Object.assign(new Error("The operation was aborted."), { name: "AbortError" });
      }
      return new Response("{}", { status: 200 });
    };
    await assert.rejects(
      () => saveClaudeSettings("http://127.0.0.1:23100", emptyClaudeSettingsDraft(), emptyClaudeSettingsDraft(), ac.signal),
    );
  } finally {
    globalThis.fetch = orig;
  }
});

test("new settings files do not import legacy Claude modules or available catalogues", () => {
  for (const name of ["claude-settings.tsx", "claude-settings-state.ts", "claude-settings-error.ts", "claude-settings-io.ts", "hash.ts"]) {
    const source = readFileSync(path.join(HARNESSES_DIR, name), "utf8");
    assert.equal(source.includes("available"), name === "claude-settings-state.ts");
    if (name === "claude-settings-state.ts") {
      assert.equal(source.includes("payload.available"), false);
      assert.equal(source.includes("\"available\""), true);
    }
    assert.equal(source.includes("claude-manual-env"), false);
    assert.equal(source.includes("ClaudeCode"), false);
    assert.equal(source.includes("use-claude-code-page"), false);
  }
  const settings = readFileSync(path.join(HARNESSES_DIR, "claude-settings.tsx"), "utf8");
  const io = readFileSync(path.join(HARNESSES_DIR, "claude-settings-io.ts"), "utf8");
  assert.equal(io.includes("managementErrorMessage"), true);
  assert.equal(io.includes("/api/models"), true);
  assert.equal(io.includes("/api/claude-code"), true);
  assert.equal(settings.includes("readClaudeSettings"), true);
  assert.equal(settings.includes("saveClaudeSettings"), true);
});
