import assert from "node:assert/strict";
import { existsSync, readdirSync, readFileSync, statSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  decodeGrokRegistration,
  loadGrokFenceStatus,
} from "../src/pages/integrations/integration-api.ts";
import { harnessHash, harnessShowsSettings, parseHarnessHash } from "../src/pages/harnesses/hash.ts";
import { overlayHarnessesAt } from "../src/pages/harnesses/live-overlay.ts";

const ROOT = path.join(path.dirname(fileURLToPath(import.meta.url)), "..");
const PAGES = path.join(ROOT, "src", "pages");
const HARNESSES = path.join(PAGES, "harnesses");
const HARNESS_DETAIL = path.join(HARNESSES, "HarnessDetail.tsx");

/** The unmounted Integrations-era Grok tree #252 deleted, and the rejected selection layer. */
const RETIRED_GROK_SOURCES = [
  "Grok.tsx",
  "grok-page-body.tsx",
  "use-grok-page.ts",
  "grok-actions.ts",
  "grok-decode.ts",
  "grok-groups.ts",
];
const REJECTED_SELECTION_SOURCES = [
  "grok-selection.ts",
  "grok-selection-io.ts",
  "grok-settings.tsx",
];

function read(file: string): string {
  return readFileSync(file, "utf8");
}

function walkSources(dir: string): string[] {
  const out: string[] = [];
  for (const name of readdirSync(dir)) {
    const full = path.join(dir, name);
    if (statSync(full).isDirectory()) {
      out.push(...walkSources(full));
      continue;
    }
    if (/\.(ts|tsx)$/.test(name)) out.push(full);
  }
  return out;
}

function importsModule(source: string, module: string): boolean {
  return [
    "\"./" + module + "\"",
    "\"./" + module + ".ts\"",
    "\"../pages/" + module + "\"",
    "\"../pages/" + module + ".ts\"",
    "\"../harnesses/" + module + "\"",
    "\"../harnesses/" + module + ".ts\"",
  ].some((needle) => source.includes(needle));
}

function liveBundle(registration: Record<string, unknown> | null) {
  return {
    files: [],
    natives: [{ clientId: "grok", state: "absent", installed: false, configPath: "", desiredEnabled: false, disableBlocked: null }],
    journal: [],
    claude: null,
    desktop: null,
    registrations: registration ? { grok: registration } : {},
    codex: null,
    settings: {},
    probes: [],
  };
}

test("the Grok surface is a normal Harness detail, not a second Models page", () => {
  // No Grok Settings tab: there is no Grok-specific setting left to put in one.
  assert.equal(harnessShowsSettings("claude"), true);
  assert.equal(harnessShowsSettings("grok"), false);
  assert.equal(harnessShowsSettings("codex"), false);
  assert.equal(harnessShowsSettings(null), false);
  assert.deepEqual(parseHarnessHash("#harnesses/grok"), { id: "grok", tab: "overview" });
  assert.deepEqual(parseHarnessHash("#harnesses/grok/settings"), { id: "grok", tab: "overview" });
  assert.equal(harnessHash("grok", "settings"), "harnesses/grok");
  assert.equal(harnessHash("grok"), "harnesses/grok");

  const detail = read(HARNESS_DETAIL);
  assert.equal(detail.includes("GrokSettings"), false);
  assert.equal(detail.includes("ClaudeDetailTabs"), true);
  assert.equal(detail.includes("harnessShowsSettings(harness.id)"), true);
  assert.equal(detail.includes("HarnessRegistration"), true);
});

test("no Grok-specific model-selection surface or authority remains", () => {
  for (const name of [...RETIRED_GROK_SOURCES, ...REJECTED_SELECTION_SOURCES]) {
    assert.equal(existsSync(path.join(PAGES, name)), false, name);
    assert.equal(existsSync(path.join(HARNESSES, name)), false, name);
  }
  const retiredModules = [...RETIRED_GROK_SOURCES, ...REJECTED_SELECTION_SOURCES]
    .map((name) => name.replace(/\.[^.]+$/, ""));
  for (const file of walkSources(path.join(ROOT, "src"))) {
    if (file.includes(path.sep + "i18n" + path.sep)) continue;
    const source = read(file);
    for (const module of retiredModules) {
      assert.equal(importsModule(source, module), false, path.basename(file) + " imports retired " + module);
    }
  }
  // Nothing in the dashboard may address the retired selection route or persist a Grok list.
  for (const file of walkSources(path.join(ROOT, "src"))) {
    const source = read(file);
    assert.equal(source.includes("/api/grok/selection"), false, path.basename(file));
    assert.equal(source.includes("grokExcludedModels"), false, path.basename(file));
  }
  const css = read(path.join(ROOT, "src", "styles-harnesses.css"));
  assert.equal(css.includes("harnesses-grok"), false);
  assert.equal(css.includes(".harnesses-registration"), true);
});

test("Harnesses carries no per-Harness model policy for Grok", () => {
  // The rejected surface lived in the Harnesses tree: no selection control may remain there.
  for (const file of walkSources(HARNESSES)) {
    const source = read(file);
    for (const forbidden of ["Enable all", "Disable all", "Save selection", "grok-selection"]) {
      assert.equal(source.includes(forbidden), false, path.basename(file) + " keeps " + forbidden);
    }
  }
  const en = read(path.join(ROOT, "src", "i18n", "en.ts"));
  for (const key of [
    "harnesses.grok.shown",
    "harnesses.grok.groupEnableAll",
    "harnesses.grok.groupDisableAll",
    "harnesses.grok.save",
    "harnesses.grok.toggle",
  ]) {
    assert.equal(en.includes("\"" + key + "\""), false, key);
  }
});

test("the registration summary decodes only the shape the listener publishes", () => {
  const payload = {
    configPath: "/tmp/benes-fixture/.grok/config.toml",
    present: true,
    baseUrl: "http://127.0.0.1:23100/v1",
    catalogue: 19,
    registered: 19,
    current: true,
  };
  assert.deepEqual(decodeGrokRegistration(payload), {
    configPath: "/tmp/benes-fixture/.grok/config.toml",
    present: true,
    catalogue: 19,
    registered: 19,
    current: true,
    lastAutomaticError: null,
  });
  // The listener's reason for a failed automatic write travels with the summary when present.
  assert.equal(
    decodeGrokRegistration({ ...payload, current: false, lastAutomaticError: "orphaned benes marker" })?.lastAutomaticError,
    "orphaned benes marker",
  );
  for (const bad of [
    null,
    {},
    { ...payload, catalogue: "19" },
    { ...payload, registered: -1 },
    { ...payload, present: "yes" },
    { ...payload, current: 1 },
    { ...payload, configPath: 4 },
  ]) {
    assert.equal(decodeGrokRegistration(bad), null);
  }
});

test("a published registration is the Harness presence truth and stays read-only", () => {
  const published = { present: true, catalogue: 19, registered: 19, current: true, configPath: "/tmp/g" };

  // The native row stays authoritative for presence whenever the listener publishes one...
  const withNative = overlayHarnessesAt(liveBundle(published), "now").find((row) => row.id === "grok");
  assert.equal(withNative?.installed, false);
  assert.deepEqual(withNative?.registration, published);

  // ...and the registration alone carries presence when that list is unavailable.
  const fallback = liveBundle(published);
  fallback.natives = [];
  assert.equal(overlayHarnessesAt(fallback, "now").find((row) => row.id === "grok")?.installed, true);

  // A listener that publishes nothing leaves every Harness without a registration block.
  for (const row of overlayHarnessesAt(liveBundle(null), "now")) {
    assert.equal(row.registration ?? null, null, row.id);
  }
});

test("the board reads the projection summary from the listener", async () => {
  const originalFetch = globalThis.fetch;
  const json = (body: unknown) =>
    new Response(JSON.stringify(body), { status: 200, headers: { "Content-Type": "application/json" } });
  try {
    globalThis.fetch = (async () => json({
      configPath: "/tmp/benes-fixture/.grok/config.toml",
      present: true,
      baseUrl: "http://127.0.0.1:23100/v1",
      catalogue: 19,
      registered: 19,
      current: true,
    })) as typeof fetch;
    const loaded = await loadGrokFenceStatus("http://127.0.0.1:23100");
    assert.equal(loaded?.present, true);
    assert.equal(loaded?.registration?.registered, 19);

    // A truncated summary and a refused read are both "nothing published", never a claim.
    globalThis.fetch = (async () => json({ present: true })) as typeof fetch;
    assert.equal(await loadGrokFenceStatus("http://127.0.0.1:23100"), null);
    globalThis.fetch = (async () => new Response("nope", { status: 503 })) as typeof fetch;
    assert.equal(await loadGrokFenceStatus("http://127.0.0.1:23100"), null);
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("the registered-model copy is derived from the catalogue, never from a stored list", () => {
  const detail = read(HARNESS_DETAIL);
  assert.equal(detail.includes("harnesses.registration.count"), true);
  assert.equal(detail.includes("harnesses.registration.current"), true);
  assert.equal(detail.includes("harnesses.registration.stale"), true);
  assert.equal(detail.includes("harnesses.registration.absent"), true);
  // The block reports state; it holds no control that could change it.
  const block = detail.slice(detail.indexOf("function HarnessRegistration"), detail.indexOf("function HarnessCaps"));
  for (const control of ["<Switch", "<Select", "<input", "<button", "onClick"]) {
    assert.equal(block.includes(control), false, "registration block keeps " + control);
  }
});

test("a failed automatic write is reported, and never faked as current", () => {
  const detail = read(HARNESS_DETAIL);
  const block = detail.slice(detail.indexOf("function HarnessRegistration"), detail.indexOf("function HarnessCaps"));
  // The listener's own reason wins over the generic copy, and the current state is only ever the
  // one the listener reported.
  assert.equal(block.includes("lastAutomaticError"), true);
  assert.equal(block.includes("harnesses.registration.current"), true);
  assert.equal(block.includes("harnesses.registration.stale"), true);
  assert.equal(block.includes("harnesses.registration.absent"), true);
  assert.equal(block.includes("registration.current"), true);
  // Every state the block can show comes from the listener's summary; the dashboard never
  // re-derives "current" for itself.
  assert.equal(block.includes("registration.catalogue"), true);
  assert.equal(block.includes("registration.registered"), true);
});

test("the retired Grok copy left the catalogue and the registration copy replaced it", () => {
  const en = read(path.join(ROOT, "src", "i18n", "en.ts"));
  const overrides = read(path.join(ROOT, "src", "i18n", "locale-overrides.ts"));
  for (const source of [en, overrides]) {
    assert.equal(/^\s*"harnesses\.grok\./m.test(source), false);
    assert.equal(/^\s*"grok\./m.test(source), false);
  }
  for (const key of [
    "harnesses.registration.title",
    "harnesses.registration.count",
    "harnesses.registration.current",
    "harnesses.registration.stale",
    "harnesses.registration.absent",
  ]) {
    assert.equal(en.includes("\"" + key + "\""), true, key);
  }
});

test("the Models board owns its own collapse preference", () => {
  // #252 left the shared collapse helper in place because this board was then its last
  // consumer. #233 has since consolidated that persistence into the Models owner, so the
  // key belongs to the board that folds the groups and the helper is gone.
  const shared = read(path.join(PAGES, "models-shared.ts"));
  assert.equal(existsSync(path.join(PAGES, "collapse-store.ts")), false);
  assert.equal(shared.includes("benes-models-collapsed:v2"), true);
  assert.equal(shared.includes("makeCollapseStore"), false);
});
