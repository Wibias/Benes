import assert from "node:assert/strict";
import test from "node:test";
import { createServer, type ViteDevServer } from "vite";

let vite: ViteDevServer;
let orchestration: {
  planCatalogApplication: (input: {
    models: Array<{
      provider: string;
      id: string;
      namespaced: string;
      disabled: boolean;
      native?: boolean;
      contextWindow?: number;
    }>;
    providers: Array<{
      name: string;
      modelContextWindows?: Record<string, number>;
    }>;
    selectedProvider: string | null;
    pendingContextWrites: Array<{
      provider: string;
      windows: Record<string, number | null>;
    }>;
  }) => {
    selectedProvider: string | null;
    providers: Array<{
      name: string;
      modelContextWindows?: Record<string, number>;
    }>;
    pendingContextWrites: Array<{
      provider: string;
      windows: Record<string, number | null>;
    }>;
    staleContextWrites: Array<{
      provider: string;
      windows: Record<string, number | null>;
    }>;
  };
  fetchModelsCatalogSnapshot: (
    apiBase: string,
    signal: AbortSignal,
    fetchImpl?: typeof fetch,
  ) => Promise<{
    models: unknown[];
    providers: unknown[];
    selectedModels: Record<string, string[]>;
    modelPresets: Record<string, unknown>;
    discovery: { newModelPolicy: "on" | "off"; providers: Record<string, unknown> };
    disabled: string[];
    contextCaps: Record<string, number>;
    contextCapValue: number;
  }>;
};

test.before(async () => {
  vite = await createServer({
    root: process.cwd(),
    appType: "custom",
    logLevel: "silent",
    server: { middlewareMode: true },
  });
  orchestration = await vite.ssrLoadModule(
    "/src/pages/models-page-orchestration.ts",
  ) as typeof orchestration;
});

test.after(async () => {
  await vite.close();
});

function row(id: string, contextWindow: number) {
  return {
    provider: "openrouter",
    id,
    namespaced: `openrouter/${id}`,
    disabled: false,
    contextWindow,
  };
}

test("catalog application drops vanished provider selection and overlays unresolved context writes", () => {
  const pending = [{ provider: "openrouter", windows: { a: 100_000 } }];
  const result = orchestration.planCatalogApplication({
    models: [row("a", 128_000)],
    providers: [{ name: "openrouter", modelContextWindows: { a: 64_000 } }],
    selectedProvider: "missing-provider",
    pendingContextWrites: pending,
  });

  assert.equal(result.selectedProvider, null);
  assert.deepEqual(result.providers[0]?.modelContextWindows, { a: 100_000 });
  assert.deepEqual(result.pendingContextWrites, pending);
  assert.deepEqual(result.staleContextWrites, []);
});

test("catalog application retires context writes already reflected by the server", () => {
  const result = orchestration.planCatalogApplication({
    models: [row("a", 128_000)],
    providers: [{ name: "openrouter", modelContextWindows: { a: 100_000 } }],
    selectedProvider: "openrouter",
    pendingContextWrites: [{ provider: "openrouter", windows: { a: 100_000 } }],
  });

  assert.equal(result.selectedProvider, "openrouter");
  assert.deepEqual(result.pendingContextWrites, []);
  assert.deepEqual(result.providers[0]?.modelContextWindows, { a: 100_000 });
});

test("catalog application clears stale uniform preset overrides and queues their persistence", () => {
  const result = orchestration.planCatalogApplication({
    models: [row("a", 128_000), row("b", 256_000)],
    providers: [{
      name: "openrouter",
      modelContextWindows: { a: 64_000, b: 64_000 },
    }],
    selectedProvider: "openrouter",
    pendingContextWrites: [],
  });

  assert.equal(result.providers[0]?.modelContextWindows, undefined);
  assert.deepEqual(result.staleContextWrites, [{
    provider: "openrouter",
    windows: { a: null, b: null },
  }]);
  assert.deepEqual(result.pendingContextWrites, result.staleContextWrites);
});

test("catalog fetch shares one AbortSignal across every request", async () => {
  const controller = new AbortController();
  const seenSignals: AbortSignal[] = [];
  const seenUrls: string[] = [];

  const fetchImpl: typeof fetch = async (input, init) => {
    const url = String(input);
    seenUrls.push(url);
    if (init?.signal) seenSignals.push(init.signal);

    const body = url.endsWith("/api/models")
      ? [row("a", 128_000)]
      : url.endsWith("/api/provider-context-caps")
        ? { caps: { openrouter: 128_000 }, value: 350_000 }
        : url.endsWith("/api/providers")
          ? [{ name: "openrouter" }]
          : url.endsWith("/api/config")
            ? { providers: {} }
            : url.endsWith("/api/selected-models")
              ? { selected: { openrouter: ["a"] } }
              : url.endsWith("/api/model-presets")
                ? { providers: {} }
                : url.endsWith("/api/model-discovery")
                  ? { newModelPolicy: "on", providers: {} }
                  : {};

    return new Response(JSON.stringify(body), {
      status: 200,
      headers: { "Content-Type": "application/json" },
    });
  };

  const snapshot = await orchestration.fetchModelsCatalogSnapshot(
    "http://127.0.0.1:23100",
    controller.signal,
    fetchImpl,
  );

  assert.equal(seenUrls.length, 7);
  assert.equal(seenSignals.length, 7);
  assert.ok(seenSignals.every(signal => signal === controller.signal));
  assert.deepEqual(snapshot.disabled, []);
  assert.deepEqual(snapshot.contextCaps, { openrouter: 128_000 });
  assert.equal(snapshot.contextCapValue, 350_000);
});
