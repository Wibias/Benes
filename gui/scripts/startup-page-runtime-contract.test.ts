import assert from "node:assert/strict";
import test from "node:test";

import {
  loadStartupSnapshot,
  postInstallAction,
  postTrayAction,
  readStartupPageCache,
  startupBoardToast,
  startupPageCacheKey,
  startupSurfaceFor,
  writeStartupPageCache,
} from "../src/pages/startup-page-runtime.ts";

const HEALTH = {
  status: "protected",
  routingKind: "benes-local",
  routingInjected: true,
  localRoutingDependency: true,
  autostartEnabled: true,
  rebootSafe: true,
  protection: "service",
  serviceInstalled: true,
  serviceViable: true,
  serviceEnabled: true,
  serviceRunning: true,
  serviceStale: false,
  serviceConflict: false,
  serviceSupported: true,
  shimInstalled: false,
  shimHealthy: false,
  shimCoverage: "none",
  platform: "win32",
  recommendedCommand: null,
  diagnosticStale: false,
  commands: {
    installService: "benes service install",
    repairService: "benes service repair",
    installShim: "benes shim install",
    restoreNative: "benes restore",
  },
};

const TRAY = { supported: true, installed: true, running: true, stale: false, summary: "ok" };

function t(key: string, vars?: Record<string, unknown>) {
  return vars ? `${key}:${Object.values(vars).join(",")}` : key;
}

type Route = () => unknown;

function fetchStub(routes: Record<string, Route>, calls: string[] = []): typeof fetch {
  return (async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = String(input);
    calls.push(`${init?.method ?? "GET"} ${url}`);
    const path = Object.keys(routes).find(candidate => url.endsWith(candidate));
    if (!path) return new Response("not found", { status: 404 });
    const value = routes[path]!();
    if (value instanceof Error) throw value;
    if (value instanceof Response) return value;
    return Response.json(value as object);
  }) as typeof fetch;
}

const BASE = "http://127.0.0.1:23100";

test("a Windows snapshot pairs the health row with its tray status", async () => {
  const calls: string[] = [];
  const snapshot = await loadStartupSnapshot({
    apiBase: BASE,
    signal: new AbortController().signal,
    t,
    fetchImpl: fetchStub({
      "/api/startup-health": () => HEALTH,
      "/api/settings": () => ({ codexRuntime: { newerAvailable: { path: "/opt/codex" } } }),
      "/api/windows-tray": () => TRAY,
    }, calls),
  });
  assert.equal(snapshot.health.status, "protected");
  assert.equal(snapshot.health.platform, "win32");
  assert.deepEqual(snapshot.tray, TRAY);
  assert.equal(snapshot.trayError, false);
  assert.equal(snapshot.notice?.warning, "startup.codexRuntime.olderBinary:unknown");
  assert.deepEqual(calls, [
    `GET ${BASE}/api/settings`,
    `GET ${BASE}/api/startup-health`,
    `GET ${BASE}/api/windows-tray`,
  ]);
});

test("a non-Windows host never asks for a tray row", async () => {
  const calls: string[] = [];
  const snapshot = await loadStartupSnapshot({
    apiBase: BASE,
    signal: new AbortController().signal,
    t,
    fetchImpl: fetchStub({
      "/api/startup-health": () => ({ ...HEALTH, platform: "linux" }),
      "/api/settings": () => ({ codexRuntime: null }),
    }, calls),
  });
  assert.equal(snapshot.tray, null);
  assert.equal(snapshot.trayError, false);
  assert.equal(calls.some(call => call.includes("windows-tray")), false);
});

test("a failed tray read keeps the health row and reports the tray failure", async () => {
  const snapshot = await loadStartupSnapshot({
    apiBase: BASE,
    signal: new AbortController().signal,
    t,
    fetchImpl: fetchStub({
      "/api/startup-health": () => HEALTH,
      "/api/settings": () => new Response("nope", { status: 503 }),
      "/api/windows-tray": () => new Response("nope", { status: 500 }),
    }),
  });
  assert.equal(snapshot.health.status, "protected");
  assert.equal(snapshot.tray, null);
  assert.equal(snapshot.trayError, true);
  assert.equal(snapshot.notice, null, "a failed settings read must not fabricate a notice");
});

test("a settings row without codexRuntime clears the notice instead of keeping it", async () => {
  const snapshot = await loadStartupSnapshot({
    apiBase: BASE,
    signal: new AbortController().signal,
    t,
    fetchImpl: fetchStub({
      "/api/startup-health": () => ({ ...HEALTH, platform: "linux" }),
      "/api/settings": () => ({}),
    }),
  });
  assert.deepEqual(snapshot.notice, { warning: null, fix: null });
});

test("an unreadable health row fails with the copy the page renders", async () => {
  const failing = loadStartupSnapshot({
    apiBase: BASE,
    signal: new AbortController().signal,
    t,
    fetchImpl: fetchStub({
      "/api/startup-health": () => new Response("boom", { status: 500 }),
      "/api/settings": () => ({}),
    }),
  });
  await assert.rejects(failing, /fetch failed/);

  const unparseable = loadStartupSnapshot({
    apiBase: BASE,
    signal: new AbortController().signal,
    t,
    fetchImpl: fetchStub({
      "/api/startup-health": () => ({ nope: true }),
      "/api/settings": () => ({}),
    }),
  });
  await assert.rejects(unparseable, /invalid startup health/);
});
test("an aborted health read propagates so the surface can discard its generation", async () => {
  const controller = new AbortController();
  controller.abort();
  const aborted = loadStartupSnapshot({
    apiBase: BASE,
    signal: controller.signal,
    t,
    fetchImpl: (async () => { throw new DOMException("Aborted", "AbortError"); }) as typeof fetch,
  });
  await assert.rejects(aborted, /Aborted/);
});

test("a tray action posts the action and only accepts a well-formed status back", async () => {
  const calls: string[] = [];
  const bodies: unknown[] = [];
  const fetchImpl = (async (input: RequestInfo | URL, init?: RequestInit) => {
    calls.push(`${init?.method ?? "GET"} ${String(input)}`);
    bodies.push(JSON.parse(String(init?.body ?? "null")));
    return Response.json({ status: TRAY });
  }) as typeof fetch;
  assert.deepEqual(await postTrayAction(BASE, "start", fetchImpl), TRAY);
  assert.deepEqual(calls, [`POST ${BASE}/api/windows-tray`]);
  assert.deepEqual(bodies, [{ action: "start" }]);

  await assert.rejects(
    postTrayAction(BASE, "start", (async () => Response.json({ status: { installed: true } })) as typeof fetch),
    /tray/,
  );
  await assert.rejects(
    postTrayAction(BASE, "start", (async () => new Response("no", { status: 500 })) as typeof fetch),
    /tray/,
  );
});

test("an install action posts its action and repair flag and surfaces the server reason", async () => {
  const bodies: unknown[] = [];
  const ok = (async (input: RequestInfo | URL, init?: RequestInit) => {
    bodies.push(JSON.parse(String(init?.body ?? "null")));
    assert.equal(String(input), `${BASE}/api/startup-action`);
    return Response.json({ ok: true });
  }) as typeof fetch;
  await postInstallAction(BASE, "install-service", true, ok);
  await postInstallAction(BASE, "install-shim", false, ok);
  assert.deepEqual(bodies, [
    { action: "install-service", repair: true },
    { action: "install-shim", repair: false },
  ]);

  const refused = (async () => Response.json({ error: "  needs elevation  " }, { status: 500 })) as typeof fetch;
  await assert.rejects(postInstallAction(BASE, "install-shim", false, refused), /needs elevation/);
});

test("the page cache keeps its warning and tray until a newer read replaces them", () => {
  const store = new Map<string, string>();
  (globalThis as { sessionStorage?: unknown }).sessionStorage = {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => { store.set(key, value); },
    removeItem: (key: string) => { store.delete(key); },
  };
  try {
    assert.equal(startupPageCacheKey(BASE), `benes.startup.page.v1:${BASE}`);
    assert.equal(readStartupPageCache(BASE), null);

    writeStartupPageCache(BASE, { data: HEALTH, notice: { warning: "w1", fix: "f1" }, tray: TRAY });
    assert.deepEqual(readStartupPageCache(BASE), { data: HEALTH, warning: "w1", fix: "f1", tray: TRAY });

    // A health-only refresh keeps whatever the previous write learned.
    writeStartupPageCache(BASE, { data: { ...HEALTH, status: "at-risk" } });
    assert.deepEqual(readStartupPageCache(BASE), {
      data: { ...HEALTH, status: "at-risk" },
      warning: "w1",
      fix: "f1",
      tray: TRAY,
    });

    // An explicit null notice is authoritative and clears the cached warning.
    writeStartupPageCache(BASE, { data: HEALTH, notice: { warning: null, fix: null } });
    assert.deepEqual(readStartupPageCache(BASE)?.warning, null);
    assert.deepEqual(readStartupPageCache(BASE)?.tray, TRAY);
  } finally {
    delete (globalThis as { sessionStorage?: unknown }).sessionStorage;
  }
});
test("a successful install wins the toast over a copied command", () => {
  assert.deepEqual(
    startupBoardToast(t, { kind: "success", action: "install-service" }, "benes restore"),
    { tone: "ok", text: "startup.serviceInstalled" },
  );
  assert.deepEqual(
    startupBoardToast(t, { kind: "success", action: "install-shim", repair: true }, null),
    { tone: "ok", text: "startup.shimRepaired" },
  );
});

test("a failed install keeps the listener's reason in the toast", () => {
  assert.deepEqual(
    startupBoardToast(t, { kind: "error", action: "install-shim", detail: "needs elevation" }, null),
    { tone: "err", text: "startup.installFailed needs elevation" },
  );
  assert.deepEqual(
    startupBoardToast(t, { kind: "error", action: "install-shim" }, null),
    { tone: "err", text: "startup.installFailed " },
  );
});

test("a copied command alone still reports, and an idle page shows nothing", () => {
  assert.deepEqual(startupBoardToast(t, null, "benes restore"), { tone: "ok", text: "startup.copied" });
  assert.equal(startupBoardToast(t, null, null), null);
});

test("the surface decision picks the skeleton, the cold failure, then the board", () => {
  const cold = { showSkeleton: true, kind: "loading", refreshing: false, showError: false } as const;
  assert.deepEqual(startupSurfaceFor({ loadState: cold, data: null, failed: false }), { kind: "loading" });

  const failedCold = {
    showSkeleton: false,
    kind: "failed-cold",
    refreshing: false,
    showError: true,
    error: new Error("fetch failed"),
  } as const;
  assert.deepEqual(startupSurfaceFor({ loadState: failedCold, data: null, failed: false }), {
    kind: "cold-failure",
    reason: "fetch failed",
  });

  const loaded = { showSkeleton: false, kind: "loaded", refreshing: false, showError: false } as const;
  const row = {
    status: "protected",
    routingKind: "benes-local",
    routingInjected: true,
    localRoutingDependency: true,
    autostartEnabled: true,
    rebootSafe: true,
    protection: "service",
    serviceInstalled: true,
    serviceViable: true,
    serviceEnabled: true,
    serviceRunning: true,
    serviceStale: false,
    serviceConflict: false,
    serviceSupported: true,
    shimInstalled: false,
    shimHealthy: false,
    shimCoverage: "none",
    platform: "linux",
    recommendedCommand: null,
    diagnosticStale: false,
    commands: {
      installService: "a",
      repairService: "b",
      installShim: "c",
      restoreNative: "d",
    },
  } as const;
  assert.deepEqual(startupSurfaceFor({ loadState: loaded, data: row, failed: false }), {
    kind: "board",
    unresolved: false,
    readFailed: false,
  });
});

test("a failed read keeps the cached board visible and marks it unresolved", () => {
  const loaded = { showSkeleton: false, kind: "loaded", refreshing: false, showError: true } as const;
  const row = {
    status: "protected",
    routingKind: "benes-local",
    platform: "linux",
    diagnosticStale: false,
    commands: { installService: "a", repairService: "b", installShim: "c", restoreNative: "d" },
  } as const;
  assert.deepEqual(startupSurfaceFor({ loadState: loaded, data: row as never, failed: true }), {
    kind: "board",
    unresolved: true,
    readFailed: true,
  });
});

test("a row without its command block is still a loading surface, not a half board", () => {
  const loaded = { showSkeleton: false, kind: "loaded", refreshing: false, showError: false } as const;
  assert.deepEqual(startupSurfaceFor({ loadState: loaded, data: null, failed: false }), { kind: "loading" });
});

test("a cold failure without an Error still yields the page's fallback copy key", () => {
  const failedCold = {
    showSkeleton: false,
    kind: "failed-cold",
    refreshing: false,
    showError: true,
    error: "not-an-error",
  } as const;
  assert.deepEqual(startupSurfaceFor({ loadState: failedCold, data: null, failed: false }), {
    kind: "cold-failure",
    reason: null,
  });
});