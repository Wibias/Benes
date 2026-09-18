/**
 * API Access workspace composition (#287).
 *
 * These are behavioural pins for the contracts the re-authored workspace owns,
 * not shape checks on how a file happens to be written. The pure owners are
 * called directly — the tab registry, the catalogue read and search, the probe
 * request vocabulary, the client-export views, the endpoint board — because
 * that is where the page\u2019s decisions now live.
 *
 * Two ownership assertions are source-level on purpose: a retired helper must be
 * gone, and nothing may still import it.
 */
import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

import type { TFn } from "../src/i18n/shared.ts";
import type { DataSurfaceState } from "../src/data-surface.ts";
import {
  API_TABS,
  DEFAULT_API_TAB,
  apiPanelDomId,
  apiTabDomId,
  apiTabHash,
  nextApiTab,
  readApiTab,
} from "../src/pages/api-tab.ts";
import {
  apiProtocolLabel,
  apiSourceLabel,
  filterCatalogRows,
  readModelsCatalog,
  type ExternalModelRow,
} from "../src/api-access/model-catalog-view.ts";
import {
  modelTestErrorMessage,
  modelTestRequest,
} from "../src/api-access/model-tests.ts";
import { API_SURFACES, DEFAULT_ENDPOINTS } from "../src/api-access/endpoints.ts";
import { AUTH_MATRIX_COLUMNS } from "../src/api-access/auth-matrix.ts";
import { decodeKeysPayload, readKeysBoardSeed } from "../src/pages/api-keys-decode.ts";
import {
  clientConfigDetailView,
  clientConfigRowView,
  clientConfigStatus,
} from "../src/components/apikeys-workspace/client-config-resource.ts";
import type { ClientConfigEnvelope } from "../src/api-access/export-clients.ts";
import { installGuiDom } from "./gui-dom-harness.ts";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repoRoot = path.resolve(guiRoot, "..");

/** Obviously synthetic. Never a real key, never a key-shaped local value. */
const SYNTHETIC_PREFIX = "benes_data_0a1b2c\u2026";

/** Echoes the key, so an assertion names the copy the surface asked for. */
const t = ((key: string, vars?: Record<string, unknown>) =>
  vars ? key + ":" + JSON.stringify(vars) : key) as unknown as TFn;

/** A settled resource state, with only the fields a read actually varies. */
function surfaceState<T>(overrides: Partial<DataSurfaceState<T>>): DataSurfaceState<T> {
  return {
    kind: "cold",
    data: undefined,
    error: undefined,
    showSkeleton: true,
    refreshing: false,
    showError: false,
    ...overrides,
  };
}

function keyBoard() {
  return {
    keys: [{
      id: "key-1",
      name: "laptop",
      prefix: SYNTHETIC_PREFIX,
      createdAt: "2026-08-22T10:00:00Z",
      usage: { kind: "attributed", requests7d: 4, totalRequests: 9, lastUsedAt: null },
    }],
    endpoints: DEFAULT_ENDPOINTS,
    claudeCodeEnabled: true,
    authMatrix: [{ endpoint: "/v1/responses", bearer: "accepted", dedicated: "accepted", xApiKey: "accepted" }],
  };
}

function envelope(overrides: Partial<ClientConfigEnvelope> = {}): ClientConfigEnvelope {
  return {
    client: "kimi",
    filename: "kimi-config.toml",
    destination: "~/.kimi-code/config.toml",
    apiKeyEnv: "KIMI_API_KEY",
    exportHint: "Kimi Code reads credentials from its config file.",
    modelCount: 3,
    modelsWithoutLimits: 1,
    format: "toml",
    mediaType: "application/toml",
    text: "[providers.benes]\napi = 127.0.0.1:23100/v1",
    config: { providers: {} },
    ...overrides,
  };
}

test("the API workspace addresses its panels from one registry", () => {
  assert.deepEqual([...API_TABS], ["keys", "clients", "endpoints", "models", "examples"]);
  assert.equal(DEFAULT_API_TAB, "keys");
  assert.equal(apiTabHash("keys"), "api");
  assert.equal(apiTabHash("models"), "api/models");
  // The panel ids are the strip's own contract, so they are derived, not typed.
  assert.equal(apiTabDomId("clients"), "api-tab-clients");
  assert.equal(apiPanelDomId("clients"), "api-panel-clients");
  // A retired bookmark or another workspace's hash lands on the default panel.
  assert.equal(readApiTab(""), "keys");
  assert.equal(readApiTab("#api/examples"), "examples");
  assert.equal(readApiTab("#storage/trash"), "keys");
});

test("arrow keys wrap at both ends and unknown keys move nothing", () => {
  assert.equal(nextApiTab("keys", "ArrowLeft"), "examples");
  assert.equal(nextApiTab("examples", "ArrowRight"), "keys");
  assert.equal(nextApiTab("models", "ArrowRight"), "examples");
  assert.equal(nextApiTab("models", "Home"), "keys");
  assert.equal(nextApiTab("models", "End"), "examples");
  assert.equal(nextApiTab("models", "Enter"), null);
  assert.equal(nextApiTab("models", "Tab"), null);
});

test("the catalogue read takes either payload shape and rejects the rest", () => {
  const rows = readModelsCatalog({
    data: [{ id: "gpt-5.4", owned_by: "openai" }, { id: "combo/fast", owned_by: "combo" }],
  });
  assert.deepEqual(rows?.map(row => row.id), ["combo/fast", "gpt-5.4"]);
  assert.deepEqual(readModelsCatalog([{ id: "solo" }])?.map(row => row.id), ["solo"]);
  // A payload that is neither shape, or holds an unreadable row, is not a catalogue.
  assert.equal(readModelsCatalog(null), null);
  assert.equal(readModelsCatalog("{}"), null);
  assert.equal(readModelsCatalog({ data: "nope" }), null);
  assert.equal(readModelsCatalog({ data: {} }), null);
});

test("the catalogue search matches the three published fields", () => {
  const rows: ExternalModelRow[] = [
    { id: "openai/gpt-5.4", displayName: "openai/gpt-5.4", provider: "openai" },
    { id: "claude-sonnet-4-6", displayName: "claude-sonnet-4-6", provider: "anthropic" },
  ];
  assert.deepEqual(filterCatalogRows(rows, "").length, 2);
  assert.deepEqual(filterCatalogRows(rows, "  ").length, 2);
  assert.deepEqual(filterCatalogRows(rows, "GPT-5.4").map(row => row.id), ["openai/gpt-5.4"]);
  assert.deepEqual(filterCatalogRows(rows, "anthropic").map(row => row.id), ["claude-sonnet-4-6"]);
  assert.deepEqual(filterCatalogRows(rows, "sonnet").map(row => row.id), ["claude-sonnet-4-6"]);
  assert.deepEqual(filterCatalogRows(rows, "kimi"), []);
  assert.deepEqual(filterCatalogRows(rows, "nothing").length, 0);
});

test("a catalogue row is labelled by the source that owns it", () => {
  const provider = (_provider: string, _t: TFn) => "Anthropic";
  assert.equal(apiSourceLabel({ id: "gpt-5.4", displayName: "gpt-5.4", provider: "openai", native: true }, t, provider), "api.sourceNative");
  assert.equal(apiSourceLabel({ id: "combo/fast", displayName: "combo/fast", provider: "combo" }, t, provider), "api.sourceCombo");
  assert.equal(apiSourceLabel({ id: "kimi/k2", displayName: "kimi/k2", provider: "kimi", custom: true }, t, provider), "api.sourceCustom");
  assert.equal(apiSourceLabel({ id: "claude-sonnet-4-6", displayName: "claude-sonnet-4-6", provider: "anthropic" }, t, provider), "Anthropic");
  assert.equal(apiProtocolLabel("chat", t), "api.protocolChatCompletions");
});

test("a probe asks the wire the reader chose for one token", () => {
  const chat = modelTestRequest("chat", "gpt-5.4", DEFAULT_ENDPOINTS);
  assert.equal(chat.url, DEFAULT_ENDPOINTS.chatCompletions);
  assert.deepEqual(chat.body, {
    model: "gpt-5.4",
    messages: [{ role: "user", content: "ping" }],
    max_tokens: 1,
    stream: false,
  });
  const responses = modelTestRequest("responses", "gpt-5.4", DEFAULT_ENDPOINTS);
  assert.equal(responses.url, DEFAULT_ENDPOINTS.responses);
  assert.deepEqual(responses.body, { model: "gpt-5.4", input: "ping", stream: false });
  const messages = modelTestRequest("messages", "claude-sonnet-4-6", DEFAULT_ENDPOINTS);
  assert.equal(messages.url, DEFAULT_ENDPOINTS.messages);
  assert.deepEqual(messages.body, {
    model: "claude-sonnet-4-6",
    max_tokens: 1,
    messages: [{ role: "user", content: "ping" }],
  });
});

test("a refused probe reports the upstream's sentence, never its body", () => {
  assert.equal(modelTestErrorMessage("provider said no", 502), "provider said no");
  assert.equal(
    modelTestErrorMessage('{"error":{"message":"Unknown parameter: max_output_tokens"}}', 400),
    "Unknown parameter: max_output_tokens",
  );
  assert.equal(modelTestErrorMessage('{"message":"quota exhausted"}', 429), "quota exhausted");
  assert.equal(modelTestErrorMessage('{"detail":"no such model"}', 404), "no such model");
  // Markup, truncated JSON, and an empty body are the status code, not the body.
  assert.equal(modelTestErrorMessage("<html>502</html>", 502), "HTTP 502");
  assert.equal(modelTestErrorMessage("{", 502), "HTTP 502");
  assert.equal(modelTestErrorMessage("   ", 502), "HTTP 502");
  assert.equal(modelTestErrorMessage("", 500), "HTTP 500");
  // A chatty upstream is bounded so one error cannot dominate the surface.
  assert.equal(modelTestErrorMessage("x".repeat(400), 500).length, 240);
});

test("a cached key board is reused only when the accepted parser still reads it", () => {
  const dom = installGuiDom();
  const cacheKey = "benes.apikeys.list.test";
  try {
    assert.equal(readKeysBoardSeed(cacheKey), null);
    sessionStorage.setItem(cacheKey, JSON.stringify({ __benesCachedAt: 1000, data: keyBoard() }));
    const seed = readKeysBoardSeed(cacheKey);
    assert.equal(seed?.cachedAt, 1000);
    assert.deepEqual(seed?.board.keys.map(key => key.id), ["key-1"]);
    // A board whose rows fail the accepted key parse is discarded, not painted.
    const broken = keyBoard();
    broken.keys[0]!.usage = { requests7d: "4" } as never;
    sessionStorage.setItem(cacheKey, JSON.stringify({ __benesCachedAt: 1000, data: broken }));
    assert.equal(readKeysBoardSeed(cacheKey), null);
    sessionStorage.setItem(cacheKey, JSON.stringify({ __benesCachedAt: 1000, data: { keys: [] } }));
    assert.equal(readKeysBoardSeed(cacheKey), null);
  } finally {
    dom.restore();
  }
});

test("the endpoint and header tables are the accepted owners' own", () => {
  // The board prints these four routes in this order, and each is a key of the
  // endpoint record the accepted owner derives — so a route cannot be listed
  // without a URL, or typed without being listed.
  assert.deepEqual([...API_SURFACES], ["responses", "chatCompletions", "messages", "models"]);
  for (const surface of API_SURFACES) assert.equal(typeof DEFAULT_ENDPOINTS[surface], "string");
  // Each column binds a header to the verdict field the owner validates, which
  // keeps a column from being drawn against a verdict nobody sent.
  assert.deepEqual(AUTH_MATRIX_COLUMNS.map(column => column.header), [
    "Authorization: Bearer",
    "x-benes-api-key",
    "x-api-key",
  ]);
  assert.deepEqual(AUTH_MATRIX_COLUMNS.map(column => column.field), ["bearer", "dedicated", "xApiKey"]);
  const row = { endpoint: "/v1/responses", bearer: "accepted", dedicated: "required", xApiKey: "rejected" } as const;
  assert.deepEqual(AUTH_MATRIX_COLUMNS.map(column => row[column.field]), [
    "accepted",
    "required",
    "rejected",
  ]);
});

test("the clients panel reads the selected client from the same keyed envelope", () => {
  const ready = clientConfigStatus(surfaceState<ClientConfigEnvelope | null>({ kind: "ready-populated", data: envelope() }));
  assert.deepEqual(clientConfigRowView(ready, "Kimi", t), { kind: "ready", models: "3" });
  const failed = clientConfigStatus(surfaceState<ClientConfigEnvelope | null>({ kind: "failed-cold" }));
  assert.deepEqual(clientConfigRowView(failed, "Kimi", t), {
    kind: "failed",
    message: 'api.clientConfig.rowError:{"client":"Kimi"}',
    retryLabel: "common.retry",
  });
  const loading = clientConfigStatus(surfaceState<ClientConfigEnvelope | null>({ kind: "cold" }));
  assert.deepEqual(clientConfigRowView(loading, "Kimi", t), {
    kind: "loading",
    label: "api.clientConfig.loading",
  });
});

test("the generated file is previewed and moved byte for byte", () => {
  const ready = clientConfigStatus(surfaceState<ClientConfigEnvelope | null>({ kind: "ready-populated", data: envelope() }));
  assert.equal(ready.kind, "ready");
  const view = clientConfigDetailView(envelope(), true, t);
  assert.equal(view.preview.text, envelope().text);
  assert.deepEqual(view.overview.map(row => row.label), [
    "api.clientConfig.destinationShort",
    "api.clientConfig.format",
    "api.clientConfig.modelCountLabel",
    "api.clientConfig.modelsWithoutLimitsLabel",
    "api.clientConfig.apiKeyEnv",
    "api.clientConfig.exportHintLabel",
  ]);
  assert.equal(view.overview[2]?.value, "3");
  assert.equal(view.overview[4]?.code, true);
  const withoutKeys = clientConfigDetailView(envelope(), false, t);
  assert.equal(view.notes.length, 2);
  assert.equal(withoutKeys.notes.length, 3);
  assert.match(withoutKeys.notes[2] ?? "", /KIMI_API_KEY/);
});

test("a client that failed cold cannot read as an empty export", () => {
  assert.deepEqual(clientConfigStatus(surfaceState<ClientConfigEnvelope | null>({ kind: "failed-cold" })), { kind: "failed" });
  assert.deepEqual(clientConfigStatus(surfaceState<ClientConfigEnvelope | null>({ kind: "failed-with-stale", data: null })), { kind: "failed" });
  assert.deepEqual(clientConfigStatus(surfaceState<ClientConfigEnvelope | null>({ kind: "cold" })), { kind: "loading" });
  assert.deepEqual(clientConfigStatus(surfaceState<ClientConfigEnvelope | null>({ kind: "ready-populated", data: null })), { kind: "failed" });
});

test("the retired endpoint seeding helper is gone and nothing imports it", () => {
  const decode = readFileSync(path.join(guiRoot, "src", "pages", "api-keys-decode.ts"), "utf8");
  assert.equal(decode.includes("seedEndpointsFromApiBase"), false);
  const hits = spawnSync("git", ["grep", "-n", "seedEndpointsFromApiBase"], {
    cwd: repoRoot,
    encoding: "utf8",
  });
  // git grep exits 1 with no output when nothing matches, which is the point.
  assert.equal(hits.stdout.trim(), "");
});
