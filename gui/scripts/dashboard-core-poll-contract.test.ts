import assert from "node:assert/strict";
import test from "node:test";
import { createServer, type ViteDevServer } from "vite";

const API = "http://127.0.0.1:23100";

type Poll = {
  fetchStartupHealth: (apiBase: string, signal: AbortSignal) => Promise<{ status: string; stale: boolean }>;
  fetchDashboardModels: (apiBase: string, signal: AbortSignal) => Promise<Array<{ namespaced: string }>>;
  fetchDashboardUsage: (apiBase: string, signal: AbortSignal) => Promise<{ summary: { requests: number } }>;
  fetchDashboardOverview: (apiBase: string, signal: AbortSignal) => Promise<{
    health: { ok: boolean } | null;
    providers: Array<{ name: string; adapter?: string; baseUrl?: string }>;
    error: boolean;
  }>;
  fetchProjectConfigDiagnostics: (
    apiBase: string,
    signal: AbortSignal,
  ) => Promise<Array<{ path: string; issues: string[] }>>;
  fetchDashboardSettings: (
    apiBase: string,
    signal: AbortSignal,
    epochs: Epochs,
  ) => Promise<{ settings: { port: number } | undefined }>;
};

type Epochs = {
  settingsRequestEpochRef: { current: number };
  settingsMutationEpochRef: { current: number };
  settingsMutationInFlightRef: { current: boolean };
};

let vite: ViteDevServer;
let poll: Poll;

function idleEpochs(): Epochs {
  return {
    settingsRequestEpochRef: { current: 0 },
    settingsMutationEpochRef: { current: 0 },
    settingsMutationInFlightRef: { current: false },
  };
}

type Route = (url: string) => Response | Promise<Response>;

function stubFetch(route: Route) {
  const original = globalThis.fetch;
  const seen: string[] = [];
  globalThis.fetch = (async (input: RequestInfo | URL) => {
    const url = String(input);
    seen.push(url);
    return await route(url);
  }) as typeof fetch;
  return {
    seen,
    restore() { globalThis.fetch = original; },
  };
}

function json(body: unknown, status = 200) {
  return Response.json(body, { status });
}

test.before(async () => {
  vite = await createServer({
    root: process.cwd(),
    appType: "custom",
    logLevel: "silent",
    server: { middlewareMode: true },
  });
  poll = await vite.ssrLoadModule("/src/pages/dashboard-core-poll.ts") as Poll;
});

test.after(async () => {
  await vite.close();
});

test("a stale-while-revalidate probe keeps its status and its staleness", async () => {
  const stub = stubFetch(() => json({ status: "at-risk", diagnosticStale: true }));
  try {
    assert.deepEqual(await poll.fetchStartupHealth(API, new AbortController().signal), {
      status: "at-risk",
      stale: true,
    });
    assert.deepEqual(stub.seen, [`${API}/api/startup-health`]);
  } finally {
    stub.restore();
  }
});

test("a probe that cannot be read reports an error without throwing", async () => {
  const failed = stubFetch(() => new Response("nope", { status: 500 }));
  try {
    assert.deepEqual(await poll.fetchStartupHealth(API, new AbortController().signal), {
      status: "error",
      stale: false,
    });
  } finally {
    failed.restore();
  }

  const malformed = stubFetch(() => json({ status: "whatever" }));
  try {
    assert.deepEqual(await poll.fetchStartupHealth(API, new AbortController().signal), {
      status: "error",
      stale: false,
    });
  } finally {
    malformed.restore();
  }
});

test("an aborted probe propagates so the caller can discard its generation", async () => {
  const aborted = new AbortController();
  aborted.abort();
  const aborting = stubFetch(() => {
    throw new DOMException("Aborted", "AbortError");
  });
  try {
    await assert.rejects(
      () => poll.fetchStartupHealth(API, aborted.signal),
      (error: unknown) => error instanceof Error && error.name === "AbortError",
    );
  } finally {
    aborting.restore();
  }

  // Any failure that lands after the caller aborted belongs to that abandoned generation,
  // so it must not be reported as a readable health answer.
  const network = stubFetch(() => {
    throw new TypeError("network");
  });
  try {
    await assert.rejects(() => poll.fetchStartupHealth(API, aborted.signal));
  } finally {
    network.restore();
  }
  const live = stubFetch(() => {
    throw new TypeError("network");
  });
  try {
    assert.deepEqual(await poll.fetchStartupHealth(API, new AbortController().signal), {
      status: "error",
      stale: false,
    });
  } finally {
    live.restore();
  }
});

test("catalog and usage reads fail loudly so the last good snapshot survives", async () => {
  const broken = stubFetch(() => new Response("boom", { status: 502 }));
  try {
    await assert.rejects(() => poll.fetchDashboardModels(API, new AbortController().signal));
    await assert.rejects(() => poll.fetchDashboardUsage(API, new AbortController().signal));
  } finally {
    broken.restore();
  }

  const good = stubFetch(url =>
    url.includes("/api/usage")
      ? json({ summary: { requests: 3, totalTokens: 30, coverageRatio: 1 } })
      : json([{ id: "gpt", provider: "openai", namespaced: "openai/gpt" }]));
  try {
    const models = await poll.fetchDashboardModels(API, new AbortController().signal);
    assert.equal(models[0]?.namespaced, "openai/gpt");
    const usage = await poll.fetchDashboardUsage(API, new AbortController().signal);
    assert.equal(usage.summary.requests, 3);
    assert.deepEqual(good.seen, [`${API}/api/models`, `${API}/api/usage?range=30d`]);
  } finally {
    good.restore();
  }
});

test("the overview pairs the liveness probe with the provider rows the config presents", async () => {
  const good = stubFetch(url => {
    if (url.endsWith("/healthz")) return json({ ok: true });
    if (url.endsWith("/api/providers")) return json([{ name: "openai", hasApiKey: false }]);
    return json({ providers: { openai: { adapter: "openai-responses", baseUrl: "https://example.invalid" } } });
  });
  try {
    const overview = await poll.fetchDashboardOverview(API, new AbortController().signal);
    assert.equal(overview.error, false);
    // The probe publishes liveness only; the row's presentation comes from /api/config.
    assert.deepEqual(overview.health, { ok: true });
    assert.equal(overview.providers[0]?.name, "openai");
    assert.equal(overview.providers[0]?.adapter, "openai-responses");
    assert.equal(overview.providers[0]?.baseUrl, "https://example.invalid");
    assert.deepEqual(good.seen, [`${API}/healthz`, `${API}/api/providers`, `${API}/api/config`]);
  } finally {
    good.restore();
  }
});

test("an overview that cannot read a row clears instead of half-filling", async () => {
  const bad = stubFetch(url =>
    url.endsWith("/api/providers") ? new Response("", { status: 503 }) : json({ ok: true }));
  try {
    const overview = await poll.fetchDashboardOverview(API, new AbortController().signal);
    assert.deepEqual(overview, { health: null, providers: [], error: true });
  } finally {
    bad.restore();
  }

  // A public config that fails is a degraded row set, not a failed overview: the providers
  // still exist, and the presentation fields stay absent rather than invented.
  const degraded = stubFetch(url => {
    if (url.endsWith("/api/config")) return new Response("", { status: 500 });
    if (url.endsWith("/api/providers")) return json([{ name: "openai", hasApiKey: true }]);
    return json({ ok: true });
  });
  try {
    const overview = await poll.fetchDashboardOverview(API, new AbortController().signal);
    assert.deepEqual(overview.providers, [{ name: "openai", hasApiKey: true }]);
    assert.equal(overview.error, false);
  } finally {
    degraded.restore();
  }
});

test("project-config diagnostics prefer the grouped payload and fall back to warnings", async () => {
  const grouped = stubFetch(() => json({ grouped: { steep: ["/a/config.toml", "/b/config.toml"] } }));
  try {
    assert.deepEqual(await poll.fetchProjectConfigDiagnostics(API, new AbortController().signal), [
      { path: "/a/config.toml", issues: ["steep"] },
      { path: "/b/config.toml", issues: ["steep"] },
    ]);
  } finally {
    grouped.restore();
  }

  const warnings = stubFetch(() => json({ warnings: ["/c/config.toml"] }));
  try {
    assert.deepEqual(await poll.fetchProjectConfigDiagnostics(API, new AbortController().signal), [
      { path: "/c/config.toml", issues: [] },
    ]);
  } finally {
    warnings.restore();
  }
});

test("project-config diagnostics drop unusable rows instead of inventing paths", async () => {
  const stub = stubFetch(() => json({
    grouped: { steep: ["  ", 7, { path: "/not-a-published-shape" }, "/d/config.toml"] },
  }));
  try {
    assert.deepEqual(await poll.fetchProjectConfigDiagnostics(API, new AbortController().signal), [
      { path: "/d/config.toml", issues: ["steep"] },
    ]);
  } finally {
    stub.restore();
  }

  const broken = stubFetch(() => new Response("", { status: 500 }));
  try {
    assert.deepEqual(await poll.fetchProjectConfigDiagnostics(API, new AbortController().signal), []);
  } finally {
    broken.restore();
  }
});

test("settings commit only while nothing moved and no mutation is running", async () => {
  // A rogue startupHealth key travels inside the payload but is never surfaced as a seed:
  // /api/startup-health is the only owner of that state.
  const payload = {
    codexAutoStart: false,
    port: 23100,
    hostname: "127.0.0.1",
    startupHealth: { status: "protected" },
  };
  const epochs = idleEpochs();
  const stub = stubFetch(() => json(payload));
  try {
    const first = await poll.fetchDashboardSettings(API, new AbortController().signal, epochs);
    assert.equal(first.settings?.port, 23100);
    assert.deepEqual(Object.keys(first), ["settings"]);
    assert.equal(epochs.settingsRequestEpochRef.current, 1);
  } finally {
    stub.restore();
  }

  const moved = idleEpochs();
  moved.settingsMutationEpochRef.current = 4;
  const racing = stubFetch(() => {
    moved.settingsMutationEpochRef.current = 5;
    return json(payload);
  });
  try {
    const skipped = await poll.fetchDashboardSettings(API, new AbortController().signal, moved);
    assert.equal(skipped.settings, undefined);
  } finally {
    racing.restore();
  }
});
