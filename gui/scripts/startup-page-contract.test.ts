import assert from "node:assert/strict";
import test from "node:test";
import React, { type ComponentType } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createServer, type ViteDevServer } from "vite";
import type { I18nApi, TFn } from "../src/i18n/hooks.ts";

let vite: ViteDevServer;
const t: TFn = (key, vars) => vars ? `${key}:${JSON.stringify(vars)}` : key;
const API = "http://127.0.0.1:23100";
const CACHE_KEY = `benes.startup.page.v1:${API}`;

const load = async <T>(path: string) => vite.ssrLoadModule(path) as Promise<T>;
const wrapI18n = async (node: React.ReactNode) => {
  const { provideI18nApi } = await load<{
    provideI18nApi: (api: I18nApi, children: React.ReactNode) => React.ReactNode;
  }>("/src/i18n/hooks.ts");
  return provideI18nApi({ locale: "en", setLocale() {}, t }, node);
};

function health(overrides: Record<string, unknown> = {}) {
  return {
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
      installService: "benes service install",
      repairService: "benes service repair",
      installShim: "benes shim install",
      restoreNative: "benes restore",
    },
    ...overrides,
  };
}

/** A browser sessionStorage double; the seed helpers read it lazily at call time. */
function withStorage(seed: Record<string, string>) {
  const store = new Map<string, string>(Object.entries(seed));
  (globalThis as { sessionStorage?: unknown }).sessionStorage = {
    getItem: (key: string) => store.get(key) ?? null,
    setItem: (key: string, value: string) => { store.set(key, value); },
    removeItem: (key: string) => { store.delete(key); },
  };
  return () => { delete (globalThis as { sessionStorage?: unknown }).sessionStorage; };
}

async function renderStartup() {
  const { default: Startup } = await load<{ default: ComponentType<Record<string, unknown>> }>(
    "/src/pages/Startup.tsx",
  );
  return renderToStaticMarkup(
    await wrapI18n(React.createElement(Startup, { apiBase: API })) as React.ReactElement,
  );
}

test.before(async () => {
  vite = await createServer({
    root: process.cwd(),
    appType: "custom",
    logLevel: "silent",
    server: { middlewareMode: true },
  });
});

test.after(async () => {
  await vite.close();
});

test("a cold page paints the Control shell and a loading health surface", async () => {
  const restore = withStorage({});
  try {
    const markup = await renderStartup();
    assert.ok(markup.includes("nav.control"), "Control shell heading missing");
    assert.ok(markup.includes("startup.loading"), "loading surface missing");
    assert.equal(markup.includes("control.protected"), false, "no board row may paint before a read");
  } finally {
    restore();
  }
});

/*
 * The board surfaces (a cached seed, the runtime notice, the stale banner) cannot be
 * rendered here: the board mounts portalled `Select` controls, and this test host has no DOM.
 * Those decisions are pinned executably at the seam the page consumes instead —
 * `startupSurfaceFor` in startup-page-runtime-contract.test.ts.
 */
