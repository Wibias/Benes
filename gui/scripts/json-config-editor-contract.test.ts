import assert from "node:assert/strict";
import test from "node:test";

import {
  jsonDraftIsDirty,
  persistConfigDraft,
  saveFailureMessage,
} from "../src/hooks/useJsonConfigEditor.ts";

const t = (key) => key;

/** Run `body` against a stubbed fetch, restoring the real one afterwards. */
async function withFetch(handler, body) {
  const previous = globalThis.fetch;
  const calls = [];
  globalThis.fetch = async (url, init) => {
    calls.push({ url, init });
    return handler(url, init);
  };
  try {
    return await body(calls);
  } finally {
    globalThis.fetch = previous;
  }
}

function jsonResponse(ok, status, payload) {
  return { ok, status, json: async () => payload };
}

test("an open sheet is dirty only while the draft differs from its baseline", () => {
  assert.equal(jsonDraftIsDirty(true, "{}", "{}"), false);
  assert.equal(jsonDraftIsDirty(true, '{"port":1}', "{}"), true);
  assert.equal(jsonDraftIsDirty(false, '{"port":1}', "{}"), false);
  assert.equal(jsonDraftIsDirty(false, "", ""), false);
});

test("a draft that is not JSON never reaches the listener", async () => {
  await withFetch(
    () => { throw new Error("fetch should not run"); },
    async (calls) => {
      assert.deepEqual(await persistConfigDraft("", "{not json"), { outcome: "invalid-json" });
      assert.deepEqual(await persistConfigDraft("", ""), { outcome: "invalid-json" });
      assert.equal(calls.length, 0);
    },
  );
});

test("a JSON draft that is the literal null is still a valid body", async () => {
  await withFetch(
    () => jsonResponse(true, 200, {}),
    async (calls) => {
      assert.deepEqual(await persistConfigDraft("http://x", "null"), { outcome: "saved", config: null });
      assert.equal(calls.length, 1);
      assert.equal(calls[0].init.method, "PUT");
      assert.equal(calls[0].init.body, "null");
    },
  );
});

test("a successful save PUTs the parsed draft and reports the parsed config", async () => {
  await withFetch(
    () => jsonResponse(true, 200, {}),
    async (calls) => {
      const result = await persistConfigDraft("http://x", '{ "port": 23100 }');
      assert.equal(result.outcome, "saved");
      assert.deepEqual(result.config, { port: 23100 });
      assert.equal(calls[0].url, "http://x/api/config");
      assert.equal(calls[0].init.headers["Content-Type"], "application/json");
      assert.deepEqual(JSON.parse(calls[0].init.body), { port: 23100 });
    },
  );
});

test("a refusal carries the listener error when it sent one", async () => {
  await withFetch(
    () => jsonResponse(false, 400, { error: "providers must keep a baseUrl" }),
    async () => {
      assert.deepEqual(await persistConfigDraft("http://x", "{}"), {
        outcome: "refused",
        error: "providers must keep a baseUrl",
      });
    },
  );
});

test("a refusal without a usable error body still reports a refusal", async () => {
  await withFetch(
    () => jsonResponse(false, 500, {}),
    async () => {
      assert.deepEqual(await persistConfigDraft("http://x", "{}"), { outcome: "refused" });
    },
  );
  await withFetch(
    () => ({ ok: false, status: 502, json: async () => { throw new Error("not json"); } }),
    async () => {
      assert.deepEqual(await persistConfigDraft("http://x", "{}"), { outcome: "refused" });
    },
  );
});

test("a failing save shows the listener error, the invalid-json copy, or the generic copy", () => {
  assert.equal(saveFailureMessage({ outcome: "invalid-json" }, t), "prov.invalidJson");
  assert.equal(saveFailureMessage({ outcome: "refused", error: "nope" }, t), "nope");
  assert.equal(saveFailureMessage({ outcome: "refused", error: "" }, t), "prov.saveFailed");
  assert.equal(saveFailureMessage({ outcome: "refused" }, t), "prov.saveFailed");
});

test("a transport failure is left to the caller rather than reported as a refusal", async () => {
  await withFetch(
    () => { throw new Error("network down"); },
    async () => {
      await assert.rejects(() => persistConfigDraft("http://x", "{}"));
    },
  );
});
