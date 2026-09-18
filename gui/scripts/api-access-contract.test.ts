import assert from "node:assert/strict";
import test from "node:test";

import { parseApiAuthMatrix } from "../src/api-access/auth-matrix.ts";
import { DEFAULT_ENDPOINTS, deriveApiEndpoints } from "../src/api-access/endpoints.ts";
import {
  API_KEY_NAME_MAX_LENGTH,
  UNKNOWN_DATE_LABEL,
  formatKeyTimestamp,
  redactApiKeyPrefix,
} from "../src/api-access/key-display.ts";
import { isApiKeyUsage, parseApiKeyEntry, parseApiKeyUsage } from "../src/api-access/keys.ts";
import { modelProbe, probeFailure, redactProbeDetail } from "../src/api-access/model-tests.ts";
import {
  classifyExternalModel,
  externalModelId,
  type ExternalModelRow,
} from "../src/api-access/model-catalog-view.ts";
import { modelTestProtocols } from "../src/api-access/model-tests.ts";

test("usage keeps ambiguity apart from a real zero", () => {
  assert.deepEqual(parseApiKeyUsage({ requests7d: 0, totalRequests: 0 }), {
    kind: "attributed",
    requests7d: 0,
    totalRequests: 0,
    lastUsedAt: null,
  });
  assert.deepEqual(parseApiKeyUsage({ ambiguous: true }), { kind: "ambiguous" });
  assert.deepEqual(
    parseApiKeyUsage({ ambiguous: false, requests7d: 7, totalRequests: 9, lastUsedAt: "2026-08-22T10:00:00Z" }),
    { kind: "attributed", requests7d: 7, totalRequests: 9, lastUsedAt: "2026-08-22T10:00:00Z" },
  );
});

test("malformed usage fails closed instead of reading as zero", () => {
  const malformed: unknown[] = [
    undefined,
    null,
    "0",
    0,
    {},
    { requests7d: 3 },
    { totalRequests: 3 },
    { requests7d: "3", totalRequests: 3 },
    { requests7d: Number.NaN, totalRequests: 3 },
    { requests7d: Number.POSITIVE_INFINITY, totalRequests: 3 },
    { requests7d: 1, totalRequests: 2, lastUsedAt: "not-a-date" },
    { requests7d: 1, totalRequests: 2, lastUsedAt: 17 },
    { ambiguous: "yes", requests7d: 1, totalRequests: 2 },
  ];
  for (const value of malformed) {
    assert.equal(parseApiKeyUsage(value), null, `expected null for ${JSON.stringify(value)}`);
  }
});

test("cache revalidation accepts only the parsed view model", () => {
  assert.equal(isApiKeyUsage({ kind: "ambiguous" }), true);
  assert.equal(isApiKeyUsage({ kind: "attributed", requests7d: 0, totalRequests: 0, lastUsedAt: null }), true);
  assert.equal(
    isApiKeyUsage({ kind: "attributed", requests7d: 0, totalRequests: 0, lastUsedAt: "2026-08-22T10:00:00Z" }),
    true,
  );
  // A cache written by an older build carries the wire shape, not this one.
  assert.equal(isApiKeyUsage({ requests7d: 0, totalRequests: 0 }), false);
  assert.equal(isApiKeyUsage({ kind: "attributed", requests7d: "0", totalRequests: 0, lastUsedAt: null }), false);
  assert.equal(isApiKeyUsage(null), false);
});

test("auth matrix accepts only the three listener dispositions", () => {
  const row = { endpoint: "/v1/responses", bearer: "accepted", dedicated: "accepted", xApiKey: "rejected" };
  assert.deepEqual(parseApiAuthMatrix([row]), [row]);
  assert.equal(parseApiAuthMatrix([row, { ...row, endpoint: "/v1/models" }])?.length, 2);
});

test("auth matrix rejects an empty or unusable table rather than guessing", () => {
  const row = { endpoint: "/v1/responses", bearer: "accepted", dedicated: "accepted", xApiKey: "rejected" };
  assert.equal(parseApiAuthMatrix([]), null);
  assert.equal(parseApiAuthMatrix(undefined), null);
  assert.equal(parseApiAuthMatrix({}), null);
  assert.equal(parseApiAuthMatrix([{ ...row, bearer: "sometimes" }]), null);
  assert.equal(parseApiAuthMatrix([{ ...row, dedicated: true }]), null);
  assert.equal(parseApiAuthMatrix([{ ...row, xApiKey: undefined }]), null);
test("endpoint derivation follows the reported responses route", () => {
  assert.deepEqual(deriveApiEndpoints(""), DEFAULT_ENDPOINTS);
  assert.deepEqual(deriveApiEndpoints("http://127.0.0.1:23100/v1/responses"), DEFAULT_ENDPOINTS);

  const trailing = deriveApiEndpoints("http://127.0.0.1:23100/v1/responses/");
  assert.equal(trailing.baseUrl, "http://127.0.0.1:23100/v1");
  assert.equal(trailing.responses, "http://127.0.0.1:23100/v1/responses/");

  const custom = deriveApiEndpoints("http://192.168.1.10:24000/v1/responses");
  assert.equal(custom.baseUrl, "http://192.168.1.10:24000/v1");
  assert.equal(custom.chatCompletions, "http://192.168.1.10:24000/v1/chat/completions");
  assert.equal(custom.messages, "http://192.168.1.10:24000/v1/messages");
  assert.equal(custom.models, "http://192.168.1.10:24000/v1/models");

  // A configured route that is not `/v1/responses` keeps its own base.
  const noncanonical = deriveApiEndpoints("http://gateway.test/proxy/responses");
  assert.equal(noncanonical.baseUrl, "http://gateway.test/proxy");
  assert.equal(noncanonical.chatCompletions, "http://gateway.test/proxy/chat/completions");
  assert.equal(noncanonical.responses, "http://gateway.test/proxy/responses");

  const bare = deriveApiEndpoints("http://gateway.test");
  assert.equal(bare.baseUrl, "http://gateway.test");
  assert.equal(bare.models, "http://gateway.test/models");
});

test("key prefix redaction only removes the data-plane segment", () => {
  assert.equal(redactApiKeyPrefix("benes_data_0a1b2c..."), "benes_0a1b2c...");
  assert.equal(redactApiKeyPrefix("benes_0a1b2c..."), "benes_0a1b2c...");
  assert.equal(redactApiKeyPrefix(""), "");
  assert.equal(redactApiKeyPrefix("benes_data_"), "benes_");
  // Only a leading marker is display furniture; anything else is left alone.
  assert.equal(redactApiKeyPrefix("xbenes_data_abc"), "xbenes_data_abc");
  assert.equal(redactApiKeyPrefix("benes_data_abcbenes_data_def"), "benes_abcbenes_data_def");
});

test("key timestamps never fabricate a date", () => {
  assert.equal(formatKeyTimestamp("", "en-US"), UNKNOWN_DATE_LABEL);
  assert.equal(formatKeyTimestamp("not a date", "en-US"), UNKNOWN_DATE_LABEL);
  assert.equal(
    formatKeyTimestamp("2026-08-22T10:00:00Z", "en-US"),
    new Date("2026-08-22T10:00:00Z").toLocaleDateString("en-US"),
  );
  assert.equal(API_KEY_NAME_MAX_LENGTH, 64);
});

test("model probe state is per model and per protocol, idle by default", () => {
  const probes = { "openai/gpt-5.4": { responses: { status: "ok" as const } } };
  assert.equal(modelProbe(probes, "openai/gpt-5.4", "responses").status, "ok");
  assert.equal(modelProbe(probes, "openai/gpt-5.4", "chat").status, "idle");
  assert.equal(modelProbe(probes, "missing/model", "chat").status, "idle");
  assert.equal(modelProbe({}, "missing/model", "messages").status, "idle");
});

test("a failed probe always carries a redacted reason", () => {
  const failure = probeFailure("denied for benes_ABCDEF123456");
  assert.equal(failure.status, "error");
  assert.equal(failure.status === "error" ? failure.detail : "", "denied for [key redacted]");
  assert.equal(probeFailure("x".repeat(400)).status === "error", true);
});

test("a key row is parsed whole or rejected whole", () => {
  const row = {
    id: "key-1",
    name: "laptop",
    prefix: "benes_data_0a1b2c...",
    createdAt: "2026-08-22T10:00:00Z",
    usage: { requests7d: 0, totalRequests: 0 },
  };
  assert.deepEqual(parseApiKeyEntry(row), {
    id: "key-1",
    name: "laptop",
    prefix: "benes_data_0a1b2c...",
    createdAt: "2026-08-22T10:00:00Z",
    usage: { kind: "attributed", requests7d: 0, totalRequests: 0, lastUsedAt: null },
  });
  for (const broken of [
    { ...row, id: undefined },
    { ...row, prefix: undefined },
    { ...row, createdAt: 0 },
    { ...row, usage: undefined },
    { ...row, usage: { requests7d: 1 } },
  ]) {
    assert.equal(parseApiKeyEntry(broken), null, `expected null for ${JSON.stringify(broken)}`);
  }
  assert.equal(parseApiKeyEntry(null), null);
});

test("probe details are stripped of credential-shaped text and bounded", () => {
  assert.equal(
    redactProbeDetail('upstream said: {"error":{"message":"invalid key benes_ABCdef123456 rejected"}}'),
    'upstream said: {"error":{"message":"invalid key [key redacted] rejected"}}',
  );
  assert.equal(redactProbeDetail("sent x-benes-api-key: benes_ZYXwvu987654"), "sent x-benes-api-key: [key redacted]");
  assert.equal(redactProbeDetail("Authorization: Bearer sk-live-abcdef123456"), "Authorization: Bearer [key redacted]");
  assert.equal(redactProbeDetail("x".repeat(400)).length, 240);
});

test("a catalogue row keeps its callable id and names the provider that owns it", () => {
  // [id, provider, native, custom] exactly as the API tab draws them.
  const rows = [
    classifyExternalModel({ id: "gpt-5" }),
    classifyExternalModel({ id: "gpt-5", owned_by: "openai" }),
    classifyExternalModel({ id: "combo-id", owned_by: "combo" }),
    classifyExternalModel({ id: "anthropic/claude-sonnet", owned_by: "something-else" }),
    classifyExternalModel({ id: "custom/model" }),
    classifyExternalModel({ id: "bare-id", owned_by: "custom-provider" }),
    classifyExternalModel({ id: "blank-owner", owned_by: "   " }),
    classifyExternalModel({ id: "/leading", owned_by: "openai" }),
    classifyExternalModel({ id: "weird-owner", owned_by: 7 as never }),
  ];
  assert.deepEqual(
    rows.map(row => [row.id, row.provider, row.native, row.custom]),
    [
      ["gpt-5", "openai", true, false],
      ["gpt-5", "openai", true, false],
      ["combo-id", "combo", false, false],
      ["anthropic/claude-sonnet", "anthropic", false, true],
      ["custom/model", "custom", false, true],
      ["bare-id", "custom-provider", false, true],
      ["blank-owner", "openai", true, false],
      // A leading slash is not a provider prefix, and a slash at all cancels native.
      ["/leading", "openai", false, false],
      ["weird-owner", "openai", true, false],
    ],
  );
  // The published display name and the callable id are the announced id, never a rebuild.
  assert.deepEqual(rows.map(row => row.displayName), rows.map(row => row.id));
  assert.equal(externalModelId(rows[3]!), "anthropic/claude-sonnet");
  // The callable id is never rebuilt from the provider prefix.
  assert.equal(externalModelId(rows[5]!), "bare-id");
  // The classifier hands the catalogue its own row domain entity.
  const shaped: ExternalModelRow = rows[0]!;
  assert.deepEqual(Object.keys(shaped), ["id", "displayName", "provider", "native", "custom"]);
  assert.equal(shaped.disabled, undefined);
});

test("only the Anthropic provider family offers Messages", () => {
  const protocols = (provider: string) => modelTestProtocols({ provider });
  for (const anthropic of ["anthropic", "ANTHROPIC", "  anthropic  ", "claude", "Claude", "anthropic-bedrock"]) {
    assert.deepEqual(protocols(anthropic), ["responses", "chat", "messages"], anthropic);
  }
  // The model name never implies Messages; only the provider identity does.
  for (const other of ["openai", "combo", "other", "", "claude-sonnet", "anthropiclike"]) {
    assert.deepEqual(protocols(other), ["responses", "chat"], other);
  }
});

  assert.equal(parseApiAuthMatrix([{ ...row, endpoint: "" }]), null);
  assert.equal(parseApiAuthMatrix([{ ...row, endpoint: 1 }]), null);
  assert.equal(parseApiAuthMatrix([row, null]), null);
});
