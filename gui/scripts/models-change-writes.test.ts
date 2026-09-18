import assert from "node:assert/strict";
import test from "node:test";
import React from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createServer, type ViteDevServer } from "vite";
import type { TFn } from "../src/i18n/shared.ts";
import type { ModelDiscoveryState, ProviderModelMap } from "../src/model-visibility.ts";
import type {
  useModelsChangeWrites as UseModelsChangeWrites,
  ModelsCatalogMutationPatch,
} from "../src/pages/use-models-change-writes.ts";

type HookArgs = Parameters<typeof UseModelsChangeWrites>[0];
type HookActions = ReturnType<typeof UseModelsChangeWrites>;

type Deferred<T> = {
  promise: Promise<T>;
  resolve: (value: T) => void;
  reject: (reason?: unknown) => void;
};

let vite: ViteDevServer;
let useModelsChangeWrites: typeof UseModelsChangeWrites;

test.before(async () => {
  vite = await createServer({
    root: process.cwd(),
    appType: "custom",
    logLevel: "silent",
    server: { middlewareMode: true },
  });
  ({ useModelsChangeWrites } = await vite.ssrLoadModule(
    "/src/pages/use-models-change-writes.ts",
  ) as { useModelsChangeWrites: typeof UseModelsChangeWrites });
});

test.after(async () => {
  await vite.close();
});

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function response(status = 200, body?: unknown): Response {
  return new Response(body === undefined ? null : JSON.stringify(body), {
    status,
    headers: body === undefined ? undefined : { "Content-Type": "application/json" },
  });
}

function fakeTimers() {
  const originalSetTimeout = globalThis.setTimeout;
  const originalClearTimeout = globalThis.clearTimeout;
  const callbacks = new Map<number, () => void>();
  let nextId = 1;

  globalThis.setTimeout = ((callback: () => void) => {
    const id = nextId++;
    callbacks.set(id, callback);
    return id;
  }) as unknown as typeof setTimeout;

  globalThis.clearTimeout = ((id: ReturnType<typeof setTimeout>) => {
    callbacks.delete(id as unknown as number);
  }) as typeof clearTimeout;

  return {
    pending: () => callbacks.size,
    flush() {
      const pending = [...callbacks.values()];
      callbacks.clear();
      for (const callback of pending) callback();
    },
    restore() {
      callbacks.clear();
      globalThis.setTimeout = originalSetTimeout;
      globalThis.clearTimeout = originalClearTimeout;
    },
  };
}

function mountHook(overrides: Partial<HookArgs> = {}) {
  const busy: boolean[] = [];
  const disabledStates: Set<string>[] = [];
  const selectedStates: ProviderModelMap[] = [];
  const discoveryStates: ModelDiscoveryState[] = [];
  const capValues: number[] = [];
  const capStates: Record<string, number>[] = [];
  const statuses: string[] = [];
  const oks: boolean[] = [];
  const published: ModelsCatalogMutationPatch[] = [];
  const loadCalls: Array<boolean | undefined> = [];
  let settled = 0;

  const disabledRef = { current: [] as string[] };
  const selectedModelsRef = { current: {} as ProviderModelMap };
  const busyRef = { current: false };
  const discovery: ModelDiscoveryState = { newModelPolicy: "on", providers: {} };
  const t: TFn = key => key;

  const args: HookArgs = {
    apiBase: "http://127.0.0.1:23100",
    t,
    load: async force => {
      loadCalls.push(force);
      return true;
    },
    setBusy: value => busy.push(value),
    busyRef,
    setDisabled: value => disabledStates.push(value),
    setSelectedModels: value => selectedStates.push(value),
    discovery,
    setDiscovery: update => {
      const previous = discoveryStates.at(-1) ?? discovery;
      discoveryStates.push(typeof update === "function" ? update(previous) : update);
    },
    setContextCapValue: value => capValues.push(value),
    setContextCaps: value => capStates.push(value),
    setStatus: value => statuses.push(value),
    setOk: value => oks.push(value),
    disabledRef,
    selectedModelsRef,
    publishCatalogMutation: patch => published.push(patch),
    settleCatalogMutation: () => { settled += 1; },
    ...overrides,
  };

  let actions: HookActions | undefined;
  function Harness() {
    actions = useModelsChangeWrites(args);
    return null;
  }
  renderToStaticMarkup(React.createElement(Harness));
  assert.ok(actions);

  return {
    actions,
    busy,
    busyRef,
    disabledRef,
    disabledStates,
    selectedModelsRef,
    selectedStates,
    discoveryStates,
    capValues,
    capStates,
    statuses,
    oks,
    published,
    loadCalls,
    settled: () => settled,
  };
}

async function withFetch<T>(fetchImpl: typeof fetch, run: () => Promise<T>): Promise<T> {
  const original = globalThis.fetch;
  globalThis.fetch = fetchImpl;
  try {
    return await run();
  } finally {
    globalThis.fetch = original;
  }
}

test("visibility publishes optimistic state before the server write settles", async () => {
  const gate = deferred<Response>();
  await withFetch(async () => gate.promise, async () => {
    const h = mountHook();
    const pending = h.actions.applyVisibility(
      "models",
      "openrouter",
      [{ id: "next" }],
      false,
    );

    assert.deepEqual(h.disabledRef.current, ["openrouter/next"]);
    assert.deepEqual([...h.disabledStates.at(-1)!], ["openrouter/next"]);
    assert.deepEqual(h.published, [{ disabled: ["openrouter/next"], selectedModels: {} }]);

    gate.resolve(response());
    await pending;
    h.actions.cancelPendingSync();
  });
});

test("visibility server writes are serialized", async () => {
  const first = deferred<Response>();
  const calls: string[] = [];
  await withFetch(async input => {
    calls.push(String(input));
    if (calls.length === 1) return first.promise;
    return response();
  }, async () => {
    const h = mountHook();
    const firstWrite = h.actions.applyVisibility("models", "openrouter", [{ id: "a" }], false);
    const secondWrite = h.actions.applyVisibility("models", "openrouter", [{ id: "b" }], false);

    await new Promise<void>(resolve => setImmediate(resolve));
    assert.equal(calls.length, 1);

    first.resolve(response());
    await firstWrite;
    await secondWrite;
    assert.equal(calls.length, 2);
    h.actions.cancelPendingSync();
  });
});

test("failed visibility write force-loads authoritative state and reports failure", async () => {
  const timers = fakeTimers();
  try {
    await withFetch(async () => response(500), async () => {
      const h = mountHook();
      await h.actions.applyVisibility("models", "openrouter", [{ id: "a" }], false);

      assert.equal(h.settled(), 1);
      assert.deepEqual(h.loadCalls, [true]);
      assert.equal(h.oks.at(-1), false);
      assert.equal(h.statuses.at(-1), "models.saveFailed");
      assert.equal(timers.pending(), 0);
    });
  } finally {
    timers.restore();
  }
});

test("successful visibility write schedules one trailing authoritative sync", async () => {
  const timers = fakeTimers();
  try {
    await withFetch(async () => response(), async () => {
      const h = mountHook();
      await h.actions.applyVisibility("models", "openrouter", [{ id: "a" }], false);

      assert.equal(h.oks.at(-1), true);
      assert.equal(h.statuses.at(-1), "models.applied");
      assert.equal(timers.pending(), 1);
      assert.deepEqual(h.loadCalls, []);

      timers.flush();
      await Promise.resolve();
      assert.deepEqual(h.loadCalls, [true]);
    });
  } finally {
    timers.restore();
  }
});

test("cancelPendingSync prevents the scheduled authoritative load", async () => {
  const timers = fakeTimers();
  try {
    await withFetch(async () => response(), async () => {
      const h = mountHook();
      await h.actions.applyVisibility("models", "openrouter", [{ id: "a" }], false);
      assert.equal(timers.pending(), 1);

      h.actions.cancelPendingSync();
      assert.equal(timers.pending(), 0);
      timers.flush();
      await Promise.resolve();
      assert.deepEqual(h.loadCalls, []);
    });
  } finally {
    timers.restore();
  }
});

test("discovery policy stays busy for the request and clears busy in finally", async () => {
  const timers = fakeTimers();
  const gate = deferred<Response>();
  try {
    await withFetch(async () => gate.promise, async () => {
      const h = mountHook();
      const pending = h.actions.applyNewModelPolicy("off");

      assert.deepEqual(h.discoveryStates.at(-1), { newModelPolicy: "off", providers: {} });
      assert.deepEqual(h.published.at(-1), { discovery: { newModelPolicy: "off", providers: {} } });
      assert.equal(h.busy.at(-1), true);
      assert.equal(h.busyRef.current, true);

      gate.resolve(response());
      await pending;

      assert.equal(h.settled(), 1);
      assert.equal(h.busy.at(-1), false);
      assert.equal(h.busyRef.current, false);
      assert.equal(h.oks.at(-1), true);
      assert.equal(h.statuses.at(-1), "models.newPolicyApplied");
      h.actions.cancelPendingSync();
    });
  } finally {
    timers.restore();
  }
});

test("context-cap success publishes returned caps and value", async () => {
  const timers = fakeTimers();
  try {
    await withFetch(async () => response(200, {
      caps: { openrouter: 128000 },
      value: 350000,
    }), async () => {
      const h = mountHook();
      await h.actions.putCap({ provider: "openrouter", value: 128000 });

      assert.deepEqual(h.capStates, [{ openrouter: 128000 }]);
      assert.deepEqual(h.capValues, [350000]);
      assert.deepEqual(h.published.at(-1), {
        contextCaps: { openrouter: 128000 },
        contextCapValue: 350000,
      });
      assert.equal(h.settled(), 1);
      assert.equal(h.oks.at(-1), true);
      assert.equal(h.statuses.at(-1), "models.capApplied");
      h.actions.cancelPendingSync();
    });
  } finally {
    timers.restore();
  }
});

test("context-cap response failure reloads authoritative state with server feedback", async () => {
  await withFetch(async () => response(500, { error: "cap exploded" }), async () => {
    const h = mountHook();
    await h.actions.putCap({ provider: "openrouter", value: 128000 });

    assert.deepEqual(h.loadCalls, [true]);
    assert.equal(h.oks.at(-1), false);
    assert.equal(h.statuses.at(-1), "cap exploded");
    assert.equal(h.busy.at(-1), false);
    assert.equal(h.busyRef.current, false);
  });
});

test("context-cap network failure reports network error without publishing a patch", async () => {
  await withFetch(async () => { throw new Error("offline"); }, async () => {
    const h = mountHook();
    await h.actions.putCap({ provider: "openrouter", value: 128000 });

    assert.deepEqual(h.published, []);
    assert.deepEqual(h.loadCalls, []);
    assert.equal(h.oks.at(-1), false);
    assert.equal(h.statuses.at(-1), "models.networkError");
    assert.equal(h.busy.at(-1), false);
    assert.equal(h.busyRef.current, false);
  });
});
