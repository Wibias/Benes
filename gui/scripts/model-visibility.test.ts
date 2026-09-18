import assert from "node:assert/strict";
import test from "node:test";

function recordFetch() {
  const calls = [];
  const fetchImpl = async (url, init) => {
    calls.push({ url, init });
    return { ok: true, json: async () => ({ ok: true }) };
  };
  return { calls, fetchImpl };
}

test("putModelVisibility writes the merged disabled list to /api/disabled-models", async () => {
  const { putModelVisibility } = await import("../src/model-visibility.ts");
  const { calls, fetchImpl } = recordFetch();
  const response = await putModelVisibility("http://127.0.0.1:23100", {
    scope: "provider",
    provider: "openai",
    targets: [
      { id: "gpt-5.4", native: true },
      { id: "gpt-5.5", native: true },
    ],
    enabled: false,
    disabled: ["openrouter/other"],
  }, fetchImpl);
  assert.equal(response.ok, true);
  assert.equal(calls.length, 1);
  assert.equal(calls[0].url, "http://127.0.0.1:23100/api/disabled-models");
  assert.equal(calls[0].init.method, "PUT");
  assert.deepEqual(JSON.parse(calls[0].init.body).models, [
    "openai/gpt-5.4",
    "openai/gpt-5.5",
    "openrouter/other",
  ]);
});

test("putModelVisibility enable-all removes the provider's ids and leaves other disabled models", async () => {
  const { putModelVisibility } = await import("../src/model-visibility.ts");
  const { calls, fetchImpl } = recordFetch();
  await putModelVisibility("http://127.0.0.1:23100", {
    scope: "provider",
    provider: "openai",
    targets: [
      { id: "gpt-5.4", native: true },
      { id: "gpt-5.5", native: true },
    ],
    enabled: true,
    disabled: ["openai/gpt-5.4", "gpt-5.5", "openrouter/other"],
  }, fetchImpl);
  assert.equal(calls.length, 1);
  assert.deepEqual(JSON.parse(calls[0].init.body).models, ["openrouter/other"]);
});

test("putModelVisibility enable-all on a custom allowlist also clears selected-models", async () => {
  const { putModelVisibility } = await import("../src/model-visibility.ts");
  const { calls, fetchImpl } = recordFetch();
  await putModelVisibility("http://127.0.0.1:23100", {
    scope: "provider",
    provider: "openrouter",
    targets: [
      { id: "claude-sonnet-4" },
      { id: "gpt-5" },
    ],
    enabled: true,
    disabled: ["openrouter/claude-sonnet-4"],
    selected: { openrouter: ["claude-sonnet-4"] },
  }, fetchImpl);
  assert.equal(calls.length, 2);
  assert.equal(calls[0].url, "http://127.0.0.1:23100/api/disabled-models");
  assert.deepEqual(JSON.parse(calls[0].init.body).models, []);
  assert.equal(calls[1].url, "http://127.0.0.1:23100/api/selected-models");
  assert.deepEqual(JSON.parse(calls[1].init.body), { provider: "openrouter", models: [] });
});

test("selected-model parsing is fail-closed and de-duplicates ids", async () => {
  const { parseSelectedModels } = await import("../src/model-visibility.ts");
  assert.deepEqual(parseSelectedModels({ selected: { openrouter: ["a", "a", "b"] } }), {
    openrouter: ["a", "b"],
  });
  assert.throws(() => parseSelectedModels(null), /invalid selected models response/);
  assert.throws(() => parseSelectedModels({ selected: [] }), /invalid selected models response/);
  assert.throws(
    () => parseSelectedModels({ selected: { openrouter: ["a", 2] } }),
    /invalid model list/,
  );
});

test("native inclusion overrides allowlists while blocked still wins visibility", async () => {
  const { modelIncluded, modelVisible } = await import("../src/model-visibility.ts");
  const selected = { openrouter: ["allowed"] };
  assert.equal(modelIncluded(selected, "openrouter", "missing", true), true);
  assert.equal(modelIncluded(selected, "openrouter", "missing", false), false);
  assert.equal(modelVisible(selected, "openrouter", "missing", true, false), true);
  assert.equal(modelVisible(selected, "openrouter", "missing", true, true), false);
});

test("enabling one custom model extends an existing selected-model allowlist", async () => {
  const { nextSelectedAllowlist } = await import("../src/model-visibility.ts");
  assert.deepEqual(
    nextSelectedAllowlist(
      { openrouter: ["existing"] },
      "models",
      "openrouter",
      [{ id: "next" }],
      true,
    ),
    ["existing", "next"],
  );
  assert.equal(
    nextSelectedAllowlist(
      { openrouter: ["existing"] },
      "models",
      "openrouter",
      [{ id: "native", native: true }],
      true,
    ),
    undefined,
  );
});

test("failed disabled-model write prevents selected-model follow-up", async () => {
  const { putModelVisibility } = await import("../src/model-visibility.ts");
  const calls = [];
  const fetchImpl = async (url, init) => {
    calls.push({ url, init });
    return { ok: false, status: 500 };
  };
  const response = await putModelVisibility("http://127.0.0.1:23100", {
    scope: "provider",
    provider: "openrouter",
    targets: [{ id: "a" }],
    enabled: true,
    disabled: [],
    selected: { openrouter: ["a"] },
  }, fetchImpl);
  assert.equal(response.ok, false);
  assert.deepEqual(calls.map(call => call.url), [
    "http://127.0.0.1:23100/api/disabled-models",
  ]);
});

test("selected-model fetch rejects non-OK responses and forwards its abort signal", async () => {
  const { fetchSelectedModels } = await import("../src/model-visibility.ts");
  const controller = new AbortController();
  const calls = [];
  const fetchImpl = async (url, init) => {
    calls.push({ url, init });
    return { ok: false, status: 503 };
  };
  await assert.rejects(
    () => fetchSelectedModels(
      "http://127.0.0.1:23100",
      fetchImpl,
      controller.signal,
    ),
    /selected models HTTP 503/,
  );
  assert.equal(calls.length, 1);
  assert.equal(calls[0].init.signal, controller.signal);
});

test("preset parser preserves supported fields and defaults unknown mode to custom", async () => {
  const { parseModelPresets } = await import("../src/model-visibility.ts");
  assert.deepEqual(parseModelPresets({ providers: {
    openrouter: {
      mode: "future",
      available: true,
      version: 2,
      matched: 3,
      catalog: 4,
      selected: ["a"],
      warning: "warn",
    },
  } }), {
    openrouter: {
      mode: "custom",
      available: true,
      version: 2,
      matched: 3,
      catalog: 4,
      selected: ["a"],
      warning: "warn",
    },
  });
  assert.throws(
    () => parseModelPresets({ providers: { bad: { matched: "3", catalog: 4 } } }),
    /invalid model presets response/,
  );
});

test("discovery parser normalizes policy/state and rejects malformed arrivals", async () => {
  const { parseModelDiscovery, arrivalOffCount } = await import("../src/model-visibility.ts");
  const parsed = parseModelDiscovery({
    newModelPolicy: "future",
    providers: {
      openrouter: {
        policy: "off",
        skip: true,
        arrivals: [{ id: "a", state: "enabled" }, { id: "b", state: "future" }],
      },
    },
  });
  assert.equal(parsed.newModelPolicy, "on");
  assert.equal(parsed.providers.openrouter.policy, "off");
  assert.equal(parsed.providers.openrouter.arrivals[1].state, "auto-disabled");
  assert.equal(arrivalOffCount(parsed.providers.openrouter), 1);
  assert.throws(
    () => parseModelDiscovery({ providers: { openrouter: { arrivals: [null] } } }),
    /invalid model discovery response/,
  );
});

test("preset and discovery writes preserve endpoint and request shape", async () => {
  const { putModelPreset, putModelDiscoveryPolicy } = await import("../src/model-visibility.ts");
  const { calls, fetchImpl } = recordFetch();
  await putModelPreset("http://127.0.0.1:23100", "openrouter", "all", fetchImpl);
  await putModelDiscoveryPolicy("http://127.0.0.1:23100", "off", fetchImpl);
  assert.equal(calls[0].url, "http://127.0.0.1:23100/api/model-presets");
  assert.deepEqual(JSON.parse(calls[0].init.body), { provider: "openrouter", mode: "all" });
  assert.equal(calls[1].url, "http://127.0.0.1:23100/api/model-discovery");
  assert.deepEqual(JSON.parse(calls[1].init.body), { newModelPolicy: "off" });
});

test("load generation accepts only the current request generation", async () => {
  const { shouldApplyLoadGeneration } = await import("../src/model-visibility.ts");
  assert.equal(shouldApplyLoadGeneration(4, 4), true);
  assert.equal(shouldApplyLoadGeneration(3, 4), false);
  assert.equal(shouldApplyLoadGeneration(5, 4), false);
});
