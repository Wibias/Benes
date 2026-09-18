import assert from "node:assert/strict";
import test from "node:test";
import React, {
  isValidElement,
  type ComponentType,
  type ReactElement,
  type ReactNode,
} from "react";
import { renderToStaticMarkup } from "react-dom/server";
import { createServer, type ViteDevServer } from "vite";
import type { I18nApi, TFn } from "../src/i18n/hooks.ts";

let vite: ViteDevServer;
const t: TFn = (key, vars) => vars ? `${key}:${JSON.stringify(vars)}` : key;
type LoadedComponent = ComponentType<Record<string, unknown>>;

const load = async <T>(path: string) => vite.ssrLoadModule(path) as Promise<T>;
const wrapI18n = async (node: ReactNode) => {
  const { provideI18nApi } = await load<{
    provideI18nApi: (api: I18nApi, children: ReactNode) => ReactNode;
  }>("/src/i18n/hooks.ts");
  return provideI18nApi({ locale: "en", setLocale() {}, t }, node);
};

function childNodes(node: ReactNode): ReactNode[] {
  if (Array.isArray(node)) return node;
  if (!isValidElement<Record<string, unknown>>(node)) return [];
  const children = node.props.children;
  return Array.isArray(children) ? children : [children as ReactNode];
}

function findCustomCapEditor(node: ReactNode): ReactElement<Record<string, unknown>> | null {
  if (!isValidElement<Record<string, unknown>>(node)) return null;
  if (node.props.customCap === "200000" && node.props.busy === true && typeof node.type === "function") {
    return node;
  }
  for (const child of childNodes(node)) {
    const found = findCustomCapEditor(child);
    if (found) return found;
  }
  return null;
}

function findInputByValue(node: ReactNode, value: string): ReactElement<Record<string, unknown>> | null {
  if (!isValidElement<Record<string, unknown>>(node)) return null;
  if (node.type === "input" && node.props.value === value) return node;
  for (const child of childNodes(node)) {
    const found = findInputByValue(child, value);
    if (found) return found;
  }
  return null;
}

function findButtonByText(node: ReactNode, text: string): ReactElement<Record<string, unknown>> | null {
  if (!isValidElement<Record<string, unknown>>(node)) return null;
  if (node.type === "button" && node.props.children === text) return node;
  for (const child of childNodes(node)) {
    const found = findButtonByText(child, text);
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
test.after(async () => { await vite.close(); });

test("page shell preserves cold, failure, and ready surfaces", async () => {
  const { ModelsPageShell } = await load<{ ModelsPageShell: LoadedComponent }>("/src/pages/models-page-shell.tsx");
  const render = async (extra: Record<string, unknown>) => renderToStaticMarkup(await wrapI18n(React.createElement(ModelsPageShell, {
    t,
    appServerState: "fresh",
    codexController: { restarting: false, restart: async () => undefined },
    onRetryCatalog() {},
    ...extra,
  })));
  const cold = await render({ catalogCold: true, catalogColdFailure: null, catalogPanel: React.createElement("div", null, "READY") });
  assert.match(cold, /models\.loading/);
  assert.doesNotMatch(cold, /READY/);
  const failed = await render({ catalogCold: false, catalogColdFailure: "load failed", catalogPanel: React.createElement("div", null, "READY") });
  assert.match(failed, /load failed/);
  assert.match(failed, /common\.retry/);
  assert.doesNotMatch(failed, /READY/);
  const ready = await render({ catalogCold: false, catalogColdFailure: null, catalogPanel: React.createElement("div", null, "READY") });
  assert.match(ready, /READY/);
});

test("catalog panel and provider hint preserve presentation semantics", async () => {
  const { ModelsCatalogPanel } = await load<{ ModelsCatalogPanel: LoadedComponent }>("/src/pages/models-catalog-panel.tsx");
  const panel = renderToStaticMarkup(React.createElement(ModelsCatalogPanel, {
    t,
    showError: false,
    refreshing: true,
    empty: true,
    footer: { providers: 2, models: 3, visible: 1 },
    toolbar: null,
    settings: null,
    collapseControls: null,
    providerList: React.createElement("tr", null, React.createElement("td", { colSpan: 3 }, "PROVIDER")),
    modals: null,
  }));
  assert.match(panel, /aria-busy="true"/);
  assert.equal((panel.match(/<th /g) ?? []).length, 3);
  assert.match(panel, /colspan="3"/i);

  const { EmptyProviderHint } = await load<{ EmptyProviderHint: LoadedComponent }>("/src/pages/models-provider-hints.tsx");
  const live = renderToStaticMarkup(await wrapI18n(React.createElement(EmptyProviderHint, { liveModels: true, onSync() {} })));
  assert.match(live, /prov\.menu\.syncModels/);
  assert.match(live, /models\.openProviderSettings/);
  const staticCatalog = renderToStaticMarkup(await wrapI18n(React.createElement(EmptyProviderHint, { liveModels: false, onSync() {} })));
  assert.doesNotMatch(staticCatalog, /prov\.menu\.syncModels/);
  assert.match(staticCatalog, /models\.openProviderSettings/);
});

test("settings Enter callback and modal guards remain wired", async () => {
  type SettingsProps = {
    t: TFn;
    busy: boolean;
    newModelPolicy: "on" | "off";
    contextCapValue: number;
    showCustom: boolean;
    customCap: string;
    allCapped: boolean;
    onNewModelPolicy: (policy: "on" | "off") => void;
    onSelectCap: (raw: string) => void;
    onCustomCapChange: (value: string) => void;
    onApplyCustomCap: () => void;
    onSetAll: () => void;
  };
  const settings = await load<{
    ModelsCatalogSettings: (props: SettingsProps) => ReactElement;
  }>("/src/pages/models-catalog-settings.tsx");

  let applied = 0;
  const tree = settings.ModelsCatalogSettings({
    t,
    busy: true,
    newModelPolicy: "on",
    contextCapValue: 128000,
    showCustom: true,
    customCap: "200000",
    allCapped: false,
    onNewModelPolicy() {},
    onSelectCap() {},
    onCustomCapChange() {},
    onApplyCustomCap() { applied += 1; },
    onSetAll() {},
  });
  const editor = findCustomCapEditor(tree);
  assert.ok(editor);
  const renderEditor = editor.type as (props: Record<string, unknown>) => ReactNode;
  const customInput = findInputByValue(renderEditor(editor.props), "200000");
  assert.ok(customInput);
  assert.equal(customInput.props.disabled, true);
  const onKeyDown = customInput.props.onKeyDown;
  assert.equal(typeof onKeyDown, "function");
  const handleKeyDown = onKeyDown as (event: { key: string }) => void;
  handleKeyDown({ key: "Escape" });
  assert.equal(applied, 0);
  handleKeyDown({ key: "Enter" });
  assert.equal(applied, 1);

  type ModalProps = {
    open: boolean;
    mode: "add" | "edit";
    provider: string;
    t: TFn;
    error: string;
    saving: boolean;
    modelId: string;
    displayName: string;
    contextWindow: string;
    showCustomCtx: boolean;
    modalities: string[];
    reasoning: boolean;
    reasoningEfforts: string[];
    onClose: () => void;
    onModelIdChange: (value: string) => void;
    onDisplayNameChange: (value: string) => void;
    onContextSelect: (value: string) => void;
    onContextWindowChange: (value: string) => void;
    onToggleModality: (modality: string, checked: boolean) => void;
    onToggleReasoning: (checked: boolean) => void;
    onToggleEffort: (effort: string, checked: boolean) => void;
    onSubmit: () => void;
  };
  const { ModelsCustomModelModal } = await load<{
    ModelsCustomModelModal: (props: ModalProps) => ReactElement;
  }>("/src/pages/models-catalog-modals.tsx");
  const modal = ModelsCustomModelModal({
    open: true,
    mode: "add",
    provider: "openrouter",
    t,
    error: "",
    saving: false,
    modelId: "   ",
    displayName: "",
    contextWindow: "",
    showCustomCtx: false,
    modalities: ["text"],
    reasoning: false,
    reasoningEfforts: [],
    onClose() {},
    onModelIdChange() {},
    onDisplayNameChange() {},
    onContextSelect() {},
    onContextWindowChange() {},
    onToggleModality() {},
    onToggleReasoning() {},
    onToggleEffort() {},
    onSubmit() {},
  });
  const submit = findButtonByText(modal, "models.customAddBtn");
  assert.ok(submit);
  assert.equal(submit.props.disabled, true);
});
