/**
 * Dashboard producer/consumer contract.
 *
 * Every payload below is the current Go handler's response, captured from a running listener.
 * The tests fail if the Dashboard derives a field the listener does not publish, invents a
 * value the public config withheld, or keeps calling a retired route.
 */
import assert from "node:assert/strict";
import { readdirSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  fetchDashboardModels,
  fetchDashboardOverview,
  fetchDashboardSettings,
  mergeDashboardProviderRows,
  normalizeProjectConfigGroups,
  type DashboardEpochRefs,
} from "../src/pages/dashboard-core-poll.ts";
import { visionModelChangePatch, visionSelectOptions } from "../src/pages/dashboard-model-views.ts";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const API_BASE = "http://127.0.0.1:23100";

function readSrc(...parts: string[]) {
  return readFileSync(path.join(guiRoot, ...parts), "utf8");
}

function readGuiSources(dir = path.join(guiRoot, "src")): string {
  return readdirSync(dir, { withFileTypes: true })
    .map(entry => {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) return readGuiSources(full);
      return /\.tsx?$/.test(entry.name) ? readFileSync(full, "utf8") : "";
    })
    .join("\n");
}

/** The listener's liveness probe: an object with one boolean, and nothing else. */
const HEALTHZ = { ok: true };
/** `GET /api/providers` — identity plus credential presence. */
const PROVIDERS = [{ name: "openai", hasApiKey: false }];
/** `GET /api/config` — the public projection that carries adapter, base URL, and default model. */
const CONFIG = {
  defaultProvider: "openai",
  port: 23100,
  providers: {
    openai: {
      adapter: "openai-responses",
      authMode: "forward",
      baseUrl: "https://chatgpt.com/backend-api/codex",
      hasApiKey: false,
    },
  },
};
/** `GET /api/settings` — no startup-health field exists in this payload. */
const SETTINGS = {
  timeZone: "Local",
  codexAutoStart: false,
  port: 23100,
  hostname: "127.0.0.1",
  streamMode: "auto",
  appOwnedMemoryBudgetMb: 256,
  codexAccountPickerEnabled: false,
  requestPolicy: {},
  codexRuntime: { version: null },
};
/** `GET /api/models` rows, published provider/id/namespaced/disabled plus optional windows. */
const MODELS = [{ provider: "openai", id: "gpt-5.6", namespaced: "openai/gpt-5.6", disabled: false }];
/** `GET /api/diagnostics/project-config` — a coded map plus the flat path list. */
const PROJECT_CONFIG = { warnings: ["config.toml"], grouped: { model_fallback: ["config.toml"] } };
/** `GET /api/sidecar-settings` — `visionModels` is a list of `provider/model` ids. */
const SIDECAR = {
  backends: [],
  webSearch: { backend: null, enabled: true, model: "gpt-5.6-luna", streamRoutedModelOutput: false },
  vision: { backend: null, enabled: true, model: "", reasoning: null, timeoutMs: null, maxDescriptionsPerTurn: null },
  visionModels: ["anthropic/claude-sonnet"],
};

type Route = readonly [match: string, payload: unknown | null];

function idleEpochRefs(): DashboardEpochRefs {
  return {
    settingsRequestEpochRef: { current: 0 },
    settingsMutationEpochRef: { current: 0 },
    settingsMutationInFlightRef: { current: false },
  };
}

async function withFetch(
  routes: readonly Route[],
  run: () => Promise<void>,
): Promise<void> {
  const original = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = String(input instanceof Request ? input.url : input);
    const route = routes.find(([match]) => url.includes(match));
    if (!route) throw new TypeError(`unexpected fetch: ${url}`);
    if (route[1] === null) return new Response("upstream failed", { status: 500 });
    return Response.json(route[1]);
  }) as typeof fetch;
  try {
    await run();
  } finally {
    globalThis.fetch = original;
  }
}

test("the overview poll joins /api/providers identity with /api/config presentation", async () => {
  await withFetch([
    ["/healthz", HEALTHZ],
    ["/api/providers", PROVIDERS],
    ["/api/config", CONFIG],
  ], async () => {
    const poll = await fetchDashboardOverview(API_BASE, new AbortController().signal);
    assert.deepEqual(poll, {
      health: { ok: true },
      providers: [{
        name: "openai",
        hasApiKey: false,
        adapter: "openai-responses",
        baseUrl: "https://chatgpt.com/backend-api/codex",
      }],
      error: false,
    });
  });
});

test("a provider the public config does not describe keeps absent fields absent", () => {
  assert.deepEqual(
    mergeDashboardProviderRows([{ name: "openai", hasApiKey: false }], undefined),
    [{ name: "openai", hasApiKey: false }],
  );
  assert.deepEqual(
    mergeDashboardProviderRows([{ name: "custom", hasApiKey: true }], CONFIG.providers),
    [{ name: "custom", hasApiKey: true }],
  );
  assert.deepEqual(
    mergeDashboardProviderRows(
      [{ name: "openai", hasApiKey: false }],
      { openai: { adapter: "openai-responses", baseUrl: "https://example.test/v1", defaultModel: "gpt-5.6" } },
    ),
    [{
      name: "openai",
      hasApiKey: false,
      adapter: "openai-responses",
      baseUrl: "https://example.test/v1",
      defaultModel: "gpt-5.6",
    }],
  );
});

test("a failed /api/config degrades the row set instead of failing the overview", async () => {
  await withFetch([
    ["/healthz", HEALTHZ],
    ["/api/providers", PROVIDERS],
    ["/api/config", null],
  ], async () => {
    const poll = await fetchDashboardOverview(API_BASE, new AbortController().signal);
    assert.equal(poll.error, false);
    assert.deepEqual(poll.providers, [{ name: "openai", hasApiKey: false }]);
  });
});

test("an unreachable listener is the only overview error", async () => {
  await withFetch([], async () => {
    const poll = await fetchDashboardOverview(API_BASE, new AbortController().signal);
    assert.deepEqual(poll, { health: null, providers: [], error: true });
  });
});

test("the models catalog is read from the published row shape", async () => {
  await withFetch([["/api/models", MODELS]], async () => {
    const models = await fetchDashboardModels(API_BASE, new AbortController().signal);
    assert.deepEqual(models, MODELS);
  });
});

test("the settings poll delivers settings only, never a startup-health seed", async () => {
  await withFetch([["/api/settings", SETTINGS]], async () => {
    const poll = await fetchDashboardSettings(API_BASE, new AbortController().signal, idleEpochRefs());
    assert.deepEqual(Object.keys(poll), ["settings"]);
    assert.equal(poll.settings?.codexAutoStart, false);
    assert.equal(poll.settings?.hostname, "127.0.0.1");
    assert.equal(Object.hasOwn(poll.settings ?? {}, "startupHealth"), false);
  });
});

test("project-config warnings normalize from the coded map and from the flat list", () => {
  assert.deepEqual(normalizeProjectConfigGroups(PROJECT_CONFIG.grouped), [
    { path: "config.toml", issues: ["model_fallback"] },
  ]);
  assert.deepEqual(normalizeProjectConfigGroups(PROJECT_CONFIG.warnings), [
    { path: "config.toml", issues: [] },
  ]);
  assert.deepEqual(normalizeProjectConfigGroups(undefined), []);
  assert.deepEqual(normalizeProjectConfigGroups({ model_fallback: "config.toml" }), []);
});

test("the vision picker reads the server's id list, not option objects", () => {
  assert.deepEqual(visionSelectOptions([], SIDECAR), ["anthropic/claude-sonnet"]);
  const models = [{ provider: "openai", id: "text-only", namespaced: "openai/text-only" }];
  // The stored describer stays selectable, and nothing is invented from the text-only catalog.
  assert.deepEqual(
    visionSelectOptions(models, { ...SIDECAR, vision: { ...SIDECAR.vision, model: "kept" } }),
    ["kept", "anthropic/claude-sonnet"],
  );
});

test("the vision patch only sends fields PUT /api/sidecar-settings acts on", () => {
  const writable = new Set(["model", "backend", "enabled"]);
  for (const patch of [
    visionModelChangePatch("anthropic/claude-sonnet", true),
    visionModelChangePatch("anthropic/claude-sonnet", false),
    visionModelChangePatch("", true),
  ]) {
    for (const key of Object.keys(patch.vision ?? {})) {
      assert.equal(writable.has(key), true, key);
    }
  }
  assert.deepEqual(visionModelChangePatch("anthropic/claude-sonnet", true), {
    vision: { model: "anthropic/claude-sonnet", backend: "vision_describe" },
  });
});

test("the retired self-update routes have no caller and the live badge read stays", () => {
  const sources = readGuiSources();
  for (const retired of ["/api/update/check", "/api/update/run", "/api/update/status"]) {
    assert.equal(sources.includes(retired), false, retired);
  }
  assert.match(readSrc("src", "components", "sidebar-github-row.tsx"), /\/api\/update\/badge/);
  // The orb no longer opens a dialog that cannot run: it opens the manual update path.
  assert.match(readSrc("src", "components", "sidebar-github-row.tsx"), /\/releases/);
});

test("dashboard-shared.ts carries wire vocabulary only", () => {
  const shared = readSrc("src", "pages", "dashboard-shared.ts");
  assert.match(shared, /export interface HealthData \{ ok: boolean \}/);
  for (const retired of [
    "ReleaseFacts",
    "UpdateCheckData",
    "UpdateJob",
    "UpdateJobStatus",
    "Installer",
    "UpdateChannel",
    "owned_by",
    "reasoningEfforts",
    "startupHealth",
    "SyncResult",
    "ProjectCodexConfigWarning",
    "ProjectCodexConfigGroup",
    "VisionModelOption",
    "sidecarSelectOptions",
    "sidecarModelOptions",
    "visionReasoningLadder",
    "visionReasoningPatch",
    "visionTimeoutPatch",
    "visionMaxDescriptionsPatch",
    "VISION_TIMEOUT",
    "EFFORT_CAP_LEVELS",
    "useModalDialog",
  ]) {
    assert.equal(shared.includes(retired), false, retired);
  }
  // `uptime` survives only in the comment that records why nothing reads one.
  assert.equal(/uptime\s*[?:]/.test(shared), false);
});

test("the Overview footer names the running bundle and shows no uptime", () => {
  const board = readSrc("src", "pages", "dashboard-plane-board.tsx");
  assert.match(board, /__APP_VERSION__/);
  assert.equal(board.includes("formatUptime"), false);
  assert.equal(/health\?\.(version|uptime)|health\.uptime/.test(board), false);
  const sections = readSrc("src", "pages", "dashboard-plane-sections.tsx");
  assert.equal(sections.includes("uptimeFooter"), false);
  assert.equal(sections.includes("formatUptime"), false);
  assert.match(sections, /export function PlaneFooter\(\{ version \}: \{ version: string \}\)/);
});
