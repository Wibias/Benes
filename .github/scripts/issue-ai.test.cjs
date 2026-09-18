"use strict";

const { describe, it, afterEach } = require("node:test");
const assert = require("node:assert/strict");
const { completeJson } = require("./issue-ai.cjs");

const restoreFetch = globalThis.fetch;

afterEach(() => {
  globalThis.fetch = restoreFetch;
});

describe("requestJsonCompletion leakage and transport", () => {
  it("does not call fetch when the token is absent", async () => {
    let hits = 0;
    globalThis.fetch = async () => {
      hits += 1;
      throw new Error("network must stay idle without a token");
    };
    const result = await completeJson({ token: "", system: "sys", user: "usr" });
    assert.equal(result.ok, false);
    assert.equal(result.reason, "missing_token");
    assert.equal(hits, 0);
    assert.equal(Object.hasOwn(result, "text"), false);
  });

  it("maps HTTP failures to a status reason and never echoes the secret", async () => {
    globalThis.fetch = async (_url, init) => {
      assert.equal(init.headers.authorization, "Bearer secret-token-value");
      return { ok: false, status: 401, json: async () => ({ error: "nope" }) };
    };
    const result = await completeJson({
      token: "secret-token-value",
      system: "sys",
      user: "usr",
    });
    assert.deepEqual(result, { ok: false, reason: "http_401" });
    assert.equal(JSON.stringify(result).includes("secret-token-value"), false);
  });

  it("reports empty_completion when the model content is missing", async () => {
    globalThis.fetch = async () => ({
      ok: true,
      json: async () => ({ choices: [{ message: { content: "" } }] }),
    });
    const result = await completeJson({
      token: "secret-token-value",
      system: "sys",
      user: "usr",
    });
    assert.deepEqual(result, { ok: false, reason: "empty_completion" });
  });

  it("returns only the completion text on success and posts to chat/completions", async () => {
    globalThis.fetch = async (url, init) => {
      assert.equal(String(url), "https://api.openai.com/v1/chat/completions");
      const payload = JSON.parse(init.body);
      assert.equal(payload.response_format.type, "json_object");
      assert.equal(JSON.stringify(payload).includes("secret-token-value"), false);
      return {
        ok: true,
        json: async () => ({
          choices: [{ message: { content: '{"requires_translation":false}' } }],
        }),
      };
    };
    const result = await completeJson({
      token: "secret-token-value",
      system: "sys",
      user: "usr",
    });
    assert.deepEqual(result, { ok: true, text: '{"requires_translation":false}' });
    assert.equal(JSON.stringify(result).includes("secret-token-value"), false);
  });

  it("maps abort to timeout", async () => {
    globalThis.fetch = async (_url, init) => {
      const error = new Error("aborted");
      error.name = "AbortError";
      if (init.signal?.aborted) throw error;
      return new Promise((_, reject) => {
        init.signal.addEventListener("abort", () => reject(error));
      });
    };
    const result = await completeJson({
      token: "secret-token-value",
      system: "sys",
      user: "usr",
      timeoutMs: 1,
    });
    assert.deepEqual(result, { ok: false, reason: "timeout" });
  });

  it("maps other fetch throws to network without leaking the token", async () => {
    globalThis.fetch = async () => {
      throw new TypeError("fetch failed");
    };
    const result = await completeJson({
      token: "secret-token-value",
      system: "sys",
      user: "usr",
    });
    assert.deepEqual(result, { ok: false, reason: "network" });
    assert.equal(JSON.stringify(result).includes("secret-token-value"), false);
  });
});
