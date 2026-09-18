import assert from "node:assert/strict";
import test from "node:test";

import { deriveModelCatalogView, type ModelCatalogState } from "../src/api-access/model-catalog-view.ts";
import { buildRequestExamples, deriveExamplesPanel } from "../src/components/apikeys-workspace/request-examples.ts";
import { deriveApiEndpoints } from "../src/api-access/endpoints.ts";
import type { ExternalModelRow } from "../src/api-access/model-catalog-view.ts";

/** Obviously synthetic. Never a real key, never a key-shaped local value. */
const SYNTHETIC_SECRET = "benes_FAKE_TEST_KEY_DO_NOT_USE";

const TRANSLATIONS: Record<string, string> = {
  "api.modelsTitle": "Models",
  "api.modelsSubtitle": "Catalog of {count} models",
  "api.modelsSearch": "Search models",
  "api.models.testNeedsKey": "Probing needs a fresh key",
  "api.modelsLoadFailed": "Could not load models",
  "api.modelsLoading": "Loading models",
  "api.modelsEmpty": "No models are available",
  "api.modelsNoMatch": "No models match {query}",
  "api.models.testDisabledNote": "Probing stays disabled",
  "api.colModel": "Model",
  "api.colSource": "Source",
  "api.colProtocols": "Protocols",
  "api.auth.testNeedsFreshKey": "Needs a fresh key",
  "api.auth.testProtocol": "Test {protocol}",
  "api.modelCopied": "Copied",
  "api.copyModelId": "Copy ID",
  "api.testingModel": "Testing",
  "api.testSucceeded": "OK",
  "api.testFailed": "Failed",
  "common.retry": "Retry",
  "api.section.examples": "Examples",
  "api.examplesIntro": "These examples authenticate with x-benes-api-key.",
  "api.usageSampleInput": "Hello",
  "api.usageChatTitle": "Chat Completions",
  "api.usageResponsesTitle": "Responses",
  "api.usageMessagesTitle": "Messages",
};

function translate(key: string, vars?: Record<string, unknown>): string {
  let out = TRANSLATIONS[key] ?? key;
  for (const [name, value] of Object.entries(vars ?? {})) out = out.split(`{${name}}`).join(String(value));
  return out;
}

const t = translate as never;

function model(overrides: Partial<ExternalModelRow> = {}): ExternalModelRow {
  return {
    id: "openai/gpt-5.4",
    displayName: "openai/gpt-5.4",
    provider: "openai",
    native: true,
    ...overrides,
  };
}

function state(overrides: Partial<ModelCatalogState> = {}): ModelCatalogState {
  return {
    models: [model()],
    totalCount: 1,
    query: "",
    loading: false,
    refreshing: false,
    loadFailed: false,
    hasData: true,
    copiedModelId: null,
    probes: {},
    probesAvailable: false,
    sourceLabel: () => "Native",
    protocolLabel: protocol => protocol,
    ...overrides,
  };
}

function rowsOf(view: ReturnType<typeof deriveModelCatalogView>) {
  return view.surface.kind === "rows" ? view.surface.rows : [];
}

test("a cold load is a loading surface, not an empty or failed one", () => {
  const view = deriveModelCatalogView(state({ loading: true, hasData: false }), t);
  assert.equal(view.surface.kind, "loading");
  assert.equal(view.surface.kind === "loading" ? view.surface.label : "", "Loading models");
  assert.equal(view.failure, null);
  assert.equal(view.refreshing, null);
});

test("a cold failure is unavailable rather than an empty catalog", () => {
  const view = deriveModelCatalogView(
    state({ loading: false, hasData: false, loadFailed: true, models: [], totalCount: 0 }),
    t,
  );
  assert.equal(view.surface.kind, "unavailable");
  assert.equal(view.failure?.message, "Could not load models");
  assert.equal(view.failure?.retryLabel, "Retry");
});

test("a catalog that really has no models is a genuine empty state", () => {
  const view = deriveModelCatalogView(state({ models: [], totalCount: 0 }), t);
  assert.equal(view.surface.kind, "empty");
  assert.equal(view.surface.kind === "empty" ? view.surface.message : "", "No models are available");
  assert.equal(view.failure, null);
});

test("a search that matched nothing names the trimmed query", () => {
  const view = deriveModelCatalogView(state({ models: [], totalCount: 3, query: "  zz  " }), t);
  assert.equal(view.surface.kind, "no-match");
  assert.equal(view.surface.kind === "no-match" ? view.surface.message : "", "No models match zz");
});

test("a refresh keeps last-good rows visible and reports itself alongside", () => {
  const view = deriveModelCatalogView(state({ refreshing: true }), t);
  assert.equal(view.surface.kind, "rows");
  assert.equal(rowsOf(view).length, 1);
  assert.equal(view.refreshing, "Loading models");
});

test("a failed refresh keeps last-good rows visible and still offers the retry", () => {
  const view = deriveModelCatalogView(state({ refreshing: true, loadFailed: true }), t);
  assert.equal(view.surface.kind, "rows");
  assert.equal(rowsOf(view).length, 1);
  assert.equal(view.failure?.retryLabel, "Retry");
});

test("a hidden refresh does not double-report while the first load runs", () => {
  const view = deriveModelCatalogView(state({ loading: true, refreshing: true, hasData: false }), t);
  assert.equal(view.surface.kind, "loading");
  assert.equal(view.refreshing, null);
});

test("probe chips project every model × protocol state", () => {
  const anthropic = model({ id: "anthropic/claude", provider: "anthropic", native: false });
  const view = deriveModelCatalogView(
    state({
      models: [model(), anthropic],
      totalCount: 2,
      probesAvailable: true,
      probes: {
        "openai/gpt-5.4": {
          responses: { status: "ok" },
          chat: { status: "error", detail: "denied for [key redacted]" },
        },
      },
    }),
    t,
  );
  const [openaiRow, anthropicRow] = rowsOf(view);
  assert.deepEqual(openaiRow?.chips.map(chip => chip.protocol), ["responses", "chat"]);
  assert.deepEqual(anthropicRow?.chips.map(chip => chip.protocol), ["responses", "chat", "messages"]);

  const ok = openaiRow?.chips[0];
  assert.equal(ok?.label, "Test responses");
  assert.equal(ok?.disabled, false);
  assert.equal(ok?.hint, undefined);
  assert.equal(ok?.status?.text, "OK");
  assert.equal(ok?.status?.className, "api-test-note api-test-note--ok");
  assert.equal(ok?.status?.detail, undefined);

  const failed = openaiRow?.chips[1];
  assert.equal(failed?.status?.text, "Failed");
  assert.equal(failed?.status?.className, "api-test-note api-test-note--error");
  assert.equal(failed?.status?.detail, "denied for [key redacted]");

  // An idle protocol is still offered, with nothing to announce.
  assert.equal(anthropicRow?.chips[2]?.status, undefined);
  assert.equal(anthropicRow?.chips[2]?.disabled, false);
});

test("a running probe is disabled while it runs", () => {
  const view = deriveModelCatalogView(
    state({ probesAvailable: true, probes: { "openai/gpt-5.4": { chat: { status: "testing" } } } }),
    t,
  );
  const chip = rowsOf(view)[0]?.chips[1];
  assert.equal(chip?.disabled, true);
  assert.equal(chip?.status?.text, "Testing");
  assert.equal(chip?.status?.className, "api-test-note api-test-note--testing");
  assert.equal(chip?.status?.detail, undefined);
});

test("an unavailable probe is disabled, says why, and is noted once", () => {
  const view = deriveModelCatalogView(state({ probesAvailable: false }), t);
  const chip = rowsOf(view)[0]?.chips[0];
  assert.equal(chip?.disabled, true);
  assert.equal(chip?.hint, "Needs a fresh key");
  assert.equal(view.probeNote, "Probing needs a fresh key");
  assert.equal(view.disabledNote, "Probing stays disabled");
});

test("the disabled note is withheld when there is nothing to probe", () => {
  const view = deriveModelCatalogView(state({ probesAvailable: false, models: [], totalCount: 0 }), t);
  assert.equal(view.disabledNote, null);
  assert.equal(view.probeNote, "Probing needs a fresh key");
});

test("copied feedback is per model and falls back to the idle label", () => {
  const plain = deriveModelCatalogView(state(), t);
  assert.equal(rowsOf(plain)[0]?.copyLabel, "Copy ID");
  const copied = deriveModelCatalogView(state({ copiedModelId: "openai/gpt-5.4" }), t);
  assert.equal(rowsOf(copied)[0]?.copyLabel, "Copied");
});

test("the catalog header, search, and columns follow the listener copy", () => {
  const view = deriveModelCatalogView(state({ totalCount: 7 }), t);
  assert.equal(view.title, "Models");
  assert.equal(view.subtitle, "Catalog of 7 models");
  assert.equal(view.searchPlaceholder, "Search models");
  assert.deepEqual(view.columns.map(column => column.header), ["Model", "Source", "Protocols"]);
  assert.deepEqual(view.columns.map(column => column.id), ["model", "source", "protocols"]);
});

test("a repeat display name is left off the identity cell", () => {
  const plain = deriveModelCatalogView(state(), t);
  assert.equal(rowsOf(plain)[0]?.displayName, undefined);
  const named = deriveModelCatalogView(state({ models: [model({ displayName: "GPT-5.4" })] }), t);
  assert.equal(rowsOf(named)[0]?.displayName, "GPT-5.4");
  assert.equal(rowsOf(named)[0]?.id, "openai/gpt-5.4");
  assert.equal(rowsOf(named)[0]?.source, "Native");
});

const ENDPOINTS = deriveApiEndpoints("http://127.0.0.1:23199/v1/responses");

test("the examples panel offers two samples without the Claude surface", () => {
  const panel = deriveExamplesPanel(ENDPOINTS, false, t);
  assert.equal(panel.title, "Examples");
  assert.equal(panel.intro, "These examples authenticate with x-benes-api-key.");
  assert.deepEqual(panel.examples.map(example => example.id), ["chat", "responses"]);
  assert.deepEqual(panel.examples.map(example => example.title), ["Chat Completions", "Responses"]);
  for (const example of panel.examples) {
    assert.equal(example.text.includes("/v1/messages"), false);
  }
});

test("the Claude surface appends the Messages sample last", () => {
  const panel = deriveExamplesPanel(ENDPOINTS, true, t);
  assert.deepEqual(panel.examples.map(example => example.id), ["chat", "responses", "messages"]);
  assert.deepEqual(
    panel.examples.map(example => example.title),
    ["Chat Completions", "Responses", "Messages"],
  );
  assert.ok(panel.examples[2]?.text.includes("/v1/messages"));
});

test("panel sample text is the native builder's output, byte for byte", () => {
  for (const includeMessages of [false, true]) {
    const panel = deriveExamplesPanel(ENDPOINTS, includeMessages, t);
    const built = buildRequestExamples({
      endpoints: ENDPOINTS,
      sampleInput: JSON.stringify(translate("api.usageSampleInput")),
      includeMessages,
    });
    assert.deepEqual(panel.examples.map(example => example.text), built.map(example => example.text));
  }
});

test("no sample carries a held key and all use the literal placeholder", () => {
  const panel = deriveExamplesPanel(ENDPOINTS, true, t);
  for (const example of panel.examples) {
    assert.ok(example.text.includes("benes_YOUR_KEY_HERE"), `${example.id} lost the placeholder`);
    assert.equal(example.text.includes(SYNTHETIC_SECRET), false, `${example.id} used a held key`);
    assert.equal(example.text.includes("benes_FAKE"), false);
  }
});

test("the localized sample input survives into every request body", () => {
  const panel = deriveExamplesPanel(ENDPOINTS, true, t);
  for (const example of panel.examples) {
    assert.ok(example.text.includes('"Hello"'), `${example.id} lost the localized input`);
  }
  const localized = deriveExamplesPanel(ENDPOINTS, false, ((key: string) =>
    key === "api.usageSampleInput" ? "Hallo, Welt!" : translate(key)) as never);
  assert.ok(localized.examples[0]?.text.includes('"Hallo, Welt!"'));
});
