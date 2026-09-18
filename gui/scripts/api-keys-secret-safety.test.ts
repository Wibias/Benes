import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import { deriveApiEndpoints } from "../src/api-access/endpoints.ts";
import { redactApiKeyPrefix } from "../src/api-access/key-display.ts";
import { consumedOneTimeSecret, oneTimeSecretForDialog } from "../src/api-access/one-time-secret.ts";
import { decodeKeysPayload } from "../src/pages/api-keys-decode.ts";
import { buildRequestExamples } from "../src/components/apikeys-workspace/request-examples.ts";
import { copyTextToClipboard } from "../src/copy-feedback.ts";
import { installGuiDom } from "./gui-dom-harness.ts";

/** Obviously synthetic. Never a real key, never a key-shaped local value. */
const SYNTHETIC_SECRET = "benes_FAKE_TEST_KEY_DO_NOT_USE";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const affordanceSource = readFileSync(
  path.join(guiRoot, "src", "components", "apikeys-workspace", "copy-affordance.tsx"),
  "utf8",
);

test("the create dialog reveals a one-time secret exactly until it is dismissed", () => {
  assert.equal(oneTimeSecretForDialog(SYNTHETIC_SECRET, true, false), SYNTHETIC_SECRET);
  // Dismissing consumes the value it showed.
  const consumed = consumedOneTimeSecret(SYNTHETIC_SECRET, null);
  assert.equal(consumed, SYNTHETIC_SECRET);
  assert.equal(oneTimeSecretForDialog(SYNTHETIC_SECRET, true, true), null);
  // A later create is a different secret, so it reveals again.
  assert.equal(oneTimeSecretForDialog("benes_FAKE_TEST_KEY_TWO", true, false), "benes_FAKE_TEST_KEY_TWO");
  assert.equal(oneTimeSecretForDialog(null, true, false), null);
  assert.equal(oneTimeSecretForDialog(SYNTHETIC_SECRET, false, false), null);
  // Dismissing with nothing revealed keeps the earlier consumed value.
  assert.equal(consumedOneTimeSecret(null, consumed), consumed);
});

test("copy affordances report success only from an honest copied outcome", () => {
  // The shared clipboard owner already reports `unavailable` for a rejected or
  // absent write; the affordance must read success from that outcome alone, so
  // a failed write can never render the copied label.
  assert.match(affordanceSource, /const copied = feedback\.outcomeFor\(text\) === "copied"/);
  assert.match(affordanceSource, /t\(copied \? COPY_COPY\[subject\]\.copied : COPY_COPY\[subject\]\.hint\)/);
  assert.equal(affordanceSource.includes('"unavailable"'), false);
});

test("a refused clipboard write reports failure", async () => {
  const dom = installGuiDom();
  const originalNavigator = Object.getOwnPropertyDescriptor(globalThis, "navigator");
  Object.defineProperty(globalThis, "navigator", {
    configurable: true,
    value: { clipboard: { writeText: async () => { throw new Error("denied"); } } },
  });
  try {
    assert.equal(await copyTextToClipboard(SYNTHETIC_SECRET), false);
  } finally {
    if (originalNavigator) Object.defineProperty(globalThis, "navigator", originalNavigator);
    else delete (globalThis as Record<string, unknown>).navigator;
    dom.restore();
  }
});

test("clipboard writes report failure when no clipboard path exists at all", async () => {
  const dom = installGuiDom();
  const originalNavigator = Object.getOwnPropertyDescriptor(globalThis, "navigator");
  Object.defineProperty(globalThis, "navigator", { configurable: true, value: {} });
  try {
    assert.equal(await copyTextToClipboard(SYNTHETIC_SECRET), false);
  } finally {
const DEFAULT_ENDPOINTS_EXPECTED = deriveApiEndpoints("");

function expectedChatSample(): string {
  return [
    "curl http://127.0.0.1:23100/v1/chat/completions \\",
    '  -H "x-benes-api-key: benes_YOUR_KEY_HERE" \\',
    '  -H "Content-Type: application/json" \\',
    "  -d '{",
    '    "model": "gpt-5.4",',
    '    "messages": [{"role": "user", "content": "hi"}]',
    "  }'",
  ].join("\n");
}

test("request examples authenticate with a placeholder, never a held key", () => {
  const examples = buildRequestExamples({
    endpoints: DEFAULT_ENDPOINTS_EXPECTED,
    sampleInput: '"hi"',
    includeMessages: true,
  });
  assert.deepEqual(examples.map(example => example.id), ["chat", "responses", "messages"]);
  assert.equal(examples[0].text, expectedChatSample());
  for (const example of examples) {
    assert.ok(example.text.includes("benes_YOUR_KEY_HERE"), `${example.id} lost the placeholder`);
    assert.equal(example.text.includes(SYNTHETIC_SECRET), false, `${example.id} interpolated a live key`);
    assert.equal(example.text.includes("benes_FAKE"), false);
  }
  assert.ok(examples[1].text.includes('"input": "hi"'));
  assert.ok(examples[2].text.includes('"max_tokens": 64'));
  assert.ok(examples[2].text.includes("claude-sonnet-4-6"));
});

test("the Messages example only appears when that surface exists", () => {
  const without = buildRequestExamples({
    endpoints: DEFAULT_ENDPOINTS_EXPECTED,
    sampleInput: '"hi"',
    includeMessages: false,
  });
  assert.deepEqual(without.map(example => example.id), ["chat", "responses"]);
  for (const example of without) assert.equal(example.text.includes("/v1/messages"), false);
});

test("the decoded key list carries prefixes only, never plaintext", () => {
  const decoded = decodeKeysPayload({
    keys: [{
      id: "key-1",
      name: "laptop",
      prefix: "benes_data_0a1b2c...",
      createdAt: "2026-08-22T10:00:00Z",
      usage: { requests7d: 0, totalRequests: 0 },
    }],
    authMatrix: [{ endpoint: "/v1/responses", bearer: "accepted", dedicated: "accepted", xApiKey: "rejected" }],
  });
  assert.ok(decoded);
  assert.deepEqual(Object.keys(decoded.keys[0]).sort(), ["createdAt", "id", "name", "prefix", "usage"]);
  const serialized = JSON.stringify(decoded);
  assert.ok(serialized.includes("benes_data_0a1b2c..."));
  assert.equal(serialized.includes(SYNTHETIC_SECRET), false);
  // The display form drops the data-plane marker and adds nothing back.
  assert.equal(redactApiKeyPrefix(decoded.keys[0].prefix), "benes_0a1b2c...");
});

test("a malformed key row fails the whole payload rather than being zeroed", () => {
  const authMatrix = [{ endpoint: "/v1/responses", bearer: "accepted", dedicated: "accepted", xApiKey: "rejected" }];
  assert.equal(decodeKeysPayload({
    keys: [{ id: "key-1", name: "laptop", prefix: "benes_0a1b2c...", createdAt: "", usage: { requests7d: "0" } }],
    authMatrix,
  }), null);
  assert.equal(decodeKeysPayload({ keys: [], authMatrix: [] }), null);
  assert.equal(decodeKeysPayload({ keys: [] }), null);
});
    if (originalNavigator) Object.defineProperty(globalThis, "navigator", originalNavigator);
    else delete (globalThis as Record<string, unknown>).navigator;
    dom.restore();
  }
});
