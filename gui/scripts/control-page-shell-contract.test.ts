import assert from "node:assert/strict";
import test from "node:test";
import React, { isValidElement, type ComponentType, type ReactNode } from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createServer, type ViteDevServer } from "vite";
import type { I18nApi, TFn } from "../src/i18n/hooks.ts";

let vite: ViteDevServer;
const t: TFn = (key, vars) => vars ? `${key}:${JSON.stringify(vars)}` : key;

const load = async <T>(path: string) => vite.ssrLoadModule(path) as Promise<T>;
const wrapI18n = async (node: ReactNode) => {
  const { provideI18nApi } = await load<{
    provideI18nApi: (api: I18nApi, children: ReactNode) => ReactNode;
  }>("/src/i18n/hooks.ts");
  return provideI18nApi({ locale: "en", setLocale() {}, t }, node);
};

const healthPanel = () => React.createElement("span", { "data-testid": "health-panel" }, "panel-body");
const controller = () => ({ restarting: false, restart: () => undefined });

type ShellProps = {
  t: TFn;
  appServerState: "stale" | "fresh" | null;
  codexController: { restarting: boolean; restart: () => void };
  healthPanel: ReactNode;
  healthRefreshing: boolean;
  onRefresh: () => void;
};

async function renderControlShell(extra: Partial<ShellProps> = {}) {
  const { ControlPageShell } = await load<{ ControlPageShell: ComponentType<ShellProps> }>(
    "/src/pages/control-page-shell.tsx",
  );
  return renderToStaticMarkup(await wrapI18n(React.createElement(ControlPageShell, {
    t,
    appServerState: null,
    codexController: controller(),
    healthPanel: healthPanel(),
    healthRefreshing: false,
    onRefresh() {},
    ...extra,
  })) as React.ReactElement);
}

function findElement(
  node: ReactNode,
  predicate: (node: React.ReactElement<Record<string, unknown>>) => boolean,
): React.ReactElement<Record<string, unknown>> | null {
  if (!isValidElement<Record<string, unknown>>(node)) return null;
  if (predicate(node)) return node;
  const children = node.props.children;
  for (const child of Array.isArray(children) ? children : [children as ReactNode]) {
    const found = findElement(child, predicate);
    if (found) return found;
  }
  return null;
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

test("the Control shell labels itself and offers the refresh action", async () => {
  let refreshed = 0;
  const markup = await renderControlShell({ onRefresh() { refreshed += 1; } });
  assert.match(markup, /class="page-head"/);
  assert.ok(markup.includes("nav.control"), "Control heading is missing");
  assert.ok(markup.includes("startup.refresh"), "refresh action is missing");
  assert.equal(refreshed, 0, "render must not fire the refresh action");
});

test("the refresh control is disabled only while a health read is running", async () => {
  const idle = await renderControlShell({ healthRefreshing: false });
  assert.equal(idle.includes("disabled"), false);
  const busy = await renderControlShell({ healthRefreshing: true });
  assert.ok(busy.includes("disabled"), "a refresh in flight must disable the control");
});

test("the health panel renders inside the bounded control panel", async () => {
  const markup = await renderControlShell();
  assert.match(markup, /control-health-panel/);
  assert.match(markup, /data-testid="health-panel"/);
  assert.ok(
    markup.indexOf("control-health-panel") < markup.indexOf("data-testid=\"health-panel\""),
    "panel body must render inside the panel container",
  );
});

test("the panel travels through the error boundary that names the Control page", async () => {
  // The legacy SSR renderer has no error-boundary fallback, so the shell's composition is
  // read off the pure component's own element tree; the render tests above cover the
  // markup it produces.
  const { ControlPageShell } = await load<{
    ControlPageShell: (props: ShellProps) => ReactNode;
  }>("/src/pages/control-page-shell.tsx");
  const tree = ControlPageShell({
    t,
    appServerState: null,
    codexController: controller(),
    healthPanel: healthPanel(),
    healthRefreshing: false,
    onRefresh() {},
  });
  const boundary = findElement(tree, node => typeof node.props.pageName === "string");
  assert.ok(boundary, "no error boundary found");
  assert.equal(boundary.props.pageName, "nav.control");
  assert.equal(boundary.props.title, "errorBoundary.title");
  assert.equal(boundary.props.message, "errorBoundary.message");
  assert.equal(boundary.props.detailsLabel, "errorBoundary.details");
  assert.equal(boundary.props.reloadLabel, "errorBoundary.reload");
  const panel = findElement(tree, node => node.props["data-testid"] === "health-panel");
  assert.ok(panel, "the health panel must sit inside the boundary");
});

test("a stale Codex app-server reading reaches the restart banner", async () => {
  const quiet = await renderControlShell({ appServerState: "fresh" });
  assert.equal(quiet.includes("dash.codexRestart"), false);
  const stale = await renderControlShell({ appServerState: "stale" });
  assert.ok(stale.includes("dash.codexRestart"), "a stale reading must surface the restart action");
});