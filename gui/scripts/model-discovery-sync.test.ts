import assert from "node:assert/strict";
import test from "node:test";

test("postModelDiscoverySync posts the provider id and treats ok:false as failure", async () => {
  const { postModelDiscoverySync } = await import("../src/model-discovery-sync.ts");
  const calls = [];
  const fetchImpl = async (url, init) => {
    calls.push({ url, init });
    return {
      ok: true,
      json: async () => ({ ok: false, error: "upstream model discovery failed" }),
    };
  };
  const result = await postModelDiscoverySync("http://127.0.0.1:23100", "openai", fetchImpl);
  assert.equal(result.ok, false);
  assert.equal(result.error, "upstream model discovery failed");
  assert.equal(calls[0].url, "http://127.0.0.1:23100/api/model-discovery");
  assert.equal(calls[0].init.method, "POST");
  assert.equal(JSON.parse(calls[0].init.body).provider, "openai");
});

test("postModelDiscoverySync marks static catalogs as not applicable", async () => {
  const { postModelDiscoverySync } = await import("../src/model-discovery-sync.ts");
  const result = await postModelDiscoverySync("http://127.0.0.1:23100", "custom", async () => ({
    ok: true,
    json: async () => ({ ok: false, applicable: false, reason: "static_catalog" }),
  }));
  assert.equal(result.ok, false);
  assert.equal(result.applicable, false);
});

test("same-origin empty apiBase can still sync a running listener", async () => {
  const { canSyncModelDiscovery, postModelDiscoverySync } = await import("../src/model-discovery-sync.ts");
  assert.equal(canSyncModelDiscovery(""), true);
  assert.equal(canSyncModelDiscovery(undefined), false);
  assert.equal(canSyncModelDiscovery("", true), false);
  const calls = [];
  await postModelDiscoverySync("", "openai", async (url, init) => {
    calls.push({ url, init });
    return { ok: true, json: async () => ({ ok: true }) };
  });
  assert.equal(calls[0].url, "/api/model-discovery");
});

test("postModelDiscoverySync keeps live model ids from a successful sync", async () => {
  const { postModelDiscoverySync } = await import("../src/model-discovery-sync.ts");
  const result = await postModelDiscoverySync("http://127.0.0.1:23100", "command-code", async () => ({
    ok: true,
    json: async () => ({
      ok: true,
      provider: "command-code",
      models: ["deepseek/deepseek-v4-pro", "claude-sonnet-5", "xai/grok-4.6"],
    }),
  }));
  assert.equal(result.ok, true);
  assert.deepEqual(result.models, ["deepseek/deepseek-v4-pro", "claude-sonnet-5", "xai/grok-4.6"]);
});
