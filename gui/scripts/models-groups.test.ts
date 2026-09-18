import assert from "node:assert/strict";
import test from "node:test";

test("forward providers still get a Models-tab group when the catalog is empty", async () => {
  const { buildProviderModelGroups } = await import("../src/models-groups.ts");
  const groups = buildProviderModelGroups([], [
    { name: "openai", authMode: "forward" },
    { name: "openrouter", authMode: "key" },
  ]);
  assert.deepEqual(groups.map(group => group.provider), ["openai", "openrouter"]);
  assert.equal(groups[0].rows.length, 0);
  assert.equal(groups[0].catalogProbe, false);
  assert.equal(groups[1].catalogProbe, true);
});

test("native ChatGPT rows hide catalog Probe even without authMode on GET /api/providers", async () => {
  const { buildProviderModelGroups } = await import("../src/models-groups.ts");
  const groups = buildProviderModelGroups(
    [{ provider: "openai", native: true }],
    [{ name: "openai" }],
  );
  assert.equal(groups[0].nativeProviderGroup, true);
  assert.equal(groups[0].catalogProbe, false);
});

test("catalog Probe is hidden for ChatGPT login and static catalogs", async () => {
  const { catalogProbeSupported, mergeProviderCatalogMeta, buildProviderModelGroups } = await import("../src/models-groups.ts");
  assert.equal(catalogProbeSupported("forward", true), false);
  assert.equal(catalogProbeSupported("key", true), true);
  assert.equal(catalogProbeSupported("oauth", true), true);
  assert.equal(catalogProbeSupported("key", false), false);

  const merged = mergeProviderCatalogMeta(
    [{ name: "openai" }],
    { openai: { authMode: "forward" }, openrouter: { authMode: "key" } },
  );
  assert.equal(merged[0].authMode, "forward");
  assert.equal(buildProviderModelGroups([], merged)[0].catalogProbe, false);
});

test("provider config metadata keeps only valid context-window entries", async () => {
  const { mergeProviderCatalogMeta } = await import("../src/models-groups.ts");
  const [row] = mergeProviderCatalogMeta(
    [{ name: "openrouter", modelContextWindows: { old: 1 } }],
    {
      openrouter: {
        authMode: "key",
        liveModels: false,
        modelContextWindows: { " a ": 128000, zero: 0, bad: "x", "": 64000 },
      },
    },
  );
  assert.equal(row.authMode, "key");
  assert.equal(row.liveModels, false);
  assert.deepEqual(row.modelContextWindows, { a: 128000 });
});

test("invalid config metadata does not overwrite valid provider-list metadata", async () => {
  const { mergeProviderCatalogMeta } = await import("../src/models-groups.ts");
  const [row] = mergeProviderCatalogMeta(
    [{
      name: "openrouter",
      authMode: "oauth",
      liveModels: true,
      modelContextWindows: { current: 64000 },
    }],
    {
      openrouter: {
        authMode: 42,
        liveModels: "no",
        modelContextWindows: { invalid: -1 },
      },
    },
  );
  assert.equal(row.authMode, "oauth");
  assert.equal(row.liveModels, true);
  assert.deepEqual(row.modelContextWindows, { current: 64000 });
});

test("pending context patches overlay in order and retire once their effects reach server state", async () => {
  const {
    overlayPendingModelContextWindows,
    remainingModelContextWindowPatches,
  } = await import("../src/models-groups.ts");
  const base = [{ name: "openrouter", modelContextWindows: { a: 64000 } }];
  const pending = [
    { provider: "openrouter", windows: { a: 128000, b: 32000 } },
    { provider: "openrouter", windows: { a: null } },
  ];

  assert.deepEqual(
    overlayPendingModelContextWindows(base, pending)[0].modelContextWindows,
    { b: 32000 },
  );
  assert.equal(remainingModelContextWindowPatches(base, pending).length, 2);

  const settled = [{ name: "openrouter", modelContextWindows: { b: 32000 } }];
  const remaining = remainingModelContextWindowPatches(settled, pending);
  assert.deepEqual(remaining, [pending[0]]);
});

test("context-window patches ignore blank ids and non-positive values", async () => {
  const { applyProviderModelContextWindows } = await import("../src/models-groups.ts");
  const [row] = applyProviderModelContextWindows(
    [{ name: "openrouter", modelContextWindows: { keep: 64000, clear: 32000 } }],
    "openrouter",
    { " ": 128000, keep: 0, clear: null, add: 256000 },
  );
  assert.deepEqual(row.modelContextWindows, { keep: 64000, add: 256000 });
});

test("native provider identity survives mixed rows and sorts before custom providers", async () => {
  const { buildProviderModelGroups } = await import("../src/models-groups.ts");
  const groups = buildProviderModelGroups(
    [
      { provider: "openai", id: "native", native: true },
      { provider: "openai", id: "custom", native: false },
      { provider: "anthropic", id: "claude", native: false },
    ],
    [{ name: "openai" }, { name: "anthropic" }],
  );
  assert.deepEqual(groups.map(group => group.provider), ["openai", "anthropic"]);
  assert.equal(groups[0].native, false);
  assert.equal(groups[0].nativeProviderGroup, true);
  assert.equal(groups[0].catalogProbe, false);
});

test("disabled configured providers are absent even when catalog rows exist", async () => {
  const { buildProviderModelGroups } = await import("../src/models-groups.ts");
  const groups = buildProviderModelGroups(
    [{ provider: "disabled", id: "x" }],
    [{ name: "disabled", disabled: true }],
  );
  assert.deepEqual(groups, []);
});

test("group projection preserves configured models, discovery, and context metadata", async () => {
  const { buildProviderModelGroups } = await import("../src/models-groups.ts");
  const discovery = { status: "failed", reason: "network" } as const;
  const [group] = buildProviderModelGroups(
    [{ provider: "openrouter", id: "a", native: false }],
    [{
      name: "openrouter",
      models: ["a", "b"],
      contextWindow: 128000,
      modelContextWindows: { a: 64000 },
      discovery,
    }],
  );
  assert.deepEqual(group.configuredModels, ["a", "b"]);
  assert.equal(group.contextWindow, 128000);
  assert.deepEqual(group.modelContextWindows, { a: 64000 });
  assert.equal(group.discovery, discovery);
});
