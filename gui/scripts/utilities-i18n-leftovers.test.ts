import assert from "node:assert/strict";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

import {
  diagnosticsFromOxlintJson,
  summarizeStructuralDiagnostics,
} from "./check-structural-baseline.ts";
import { isTechnicalLiteral } from "../.eslint/i18n-allowlist.ts";
import {
  isCodeSampleTag,
  isInsideNonUiContext,
  isNonUiCalleeIdentifier,
  isNonUiCssPropertyName,
  isNonUiJsxAttributeName,
} from "../.eslint/i18n-non-ui-context.ts";
import {
  commitResourceFetch,
  resourceInFlightSnapshot,
  resourceNormalizeLoadError,
  resourceReplaceInflight,
  resourceTimeoutError,
  shouldSkipQuietResourceFetch,
} from "../src/client-resource-policy.ts";
import {
  accessFromTier,
  CATALOG_ACCESS_FILTER_KEYS,
  CATALOG_CONNECTION_FILTER_KEYS,
  catalogAccessLabel,
  catalogAccountStatusText,
  catalogConnectionIsSelfHosted,
  catalogConnectionLabel,
  catalogEmptyKind,
  filterAccountRows,
  isCatalogAccessFilter,
  isCatalogConnectionFilter,
  railSubtitle,
} from "../src/components/provider-catalog/catalog-policy.ts";
import {
  applySelectTriggerKey,
  selectActiveIndex,
  selectActiveOptionId,
  selectChevronTransform,
  selectDropdownClassName,
  selectDropdownStyle,
  selectListboxControls,
  selectOptionClassName,
  selectSelectedIndex,
  selectShouldRenderMenu,
  selectTriggerKeyCommand,
} from "../src/select-policy.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");
const oxlintBin = path.join(guiRoot, "node_modules", "oxlint", "bin", "oxlint");
const configPath = path.join(guiRoot, ".oxlintrc.json");
const involvedFiles = [
  path.join(guiRoot, ".eslint", "i18n-allowlist.ts"),
  path.join(guiRoot, ".eslint", "i18n-non-ui-context.ts"),
  path.join(guiRoot, ".eslint", "local-i18n-plugin.ts"),
  path.join(guiRoot, "src", "client-resource.ts"),
  path.join(guiRoot, "src", "client-resource-policy.ts"),
  path.join(guiRoot, "src", "select-policy.ts"),
  path.join(guiRoot, "src", "ui.tsx"),
  path.join(guiRoot, "src", "components", "provider-catalog", "ProviderCatalog.tsx"),
  path.join(guiRoot, "src", "components", "provider-catalog", "catalog-policy.ts"),
];

function structuralDiagnostics() {
  const result = spawnSync(
    process.execPath,
    [oxlintBin, "-c", configPath, "--format=json", ...involvedFiles],
    { cwd: guiRoot, encoding: "utf8" },
  );
  if (result.error) throw result.error;
  const stdout = String(result.stdout ?? "").trim();
  assert.ok(stdout, `Oxlint leftovers scan produced no output:\n${String(result.stderr ?? "")}`);
  return summarizeStructuralDiagnostics(diagnosticsFromOxlintJson(JSON.parse(stdout), guiRoot));
}

function jsxElement(tag) {
  return {
    type: "JSXElement",
    openingElement: { name: { type: "JSXIdentifier", name: tag } },
  };
}

function jsxAttr(name) {
  return {
    type: "JSXAttribute",
    name: { type: "JSXIdentifier", name },
  };
}

function withParent(node, parent) {
  node.parent = parent;
  return node;
}

function snapshot(overrides = {}) {
  return {
    data: undefined,
    error: undefined,
    loading: false,
    refreshing: false,
    hasSucceeded: false,
    lastAttemptOk: false,
    ...overrides,
  };
}

function identity(overrides = {}) {
  return {
    attemptGeneration: 1,
    storeGeneration: 1,
    aborted: false,
    timedOut: false,
    ...overrides,
  };
}

const t = (key) => key;

const TECHNICAL_LITERALS = [
  "",
  "   ",
  "ms",
  "k",
  "1M",
  "c",
  "w",
  "v",
  "Mo",
  "Mi",
  "Fr",
  "HTTP",
  "GiB",
  "https://api.openai.com/v1",
  "http://127.0.0.1",
  "http://127.0.0.1:23100",
  "/api/providers",
  "/healthz?refresh=1",
  "?refresh=",
  "&model=",
  "var(--surface)",
  "calc(100% - 8px)",
  "repeat(2, minmax(0, 1fr))",
  "hsl(0 0% 100%)",
  "url(#icon)",
  "linear-gradient(90deg, black, white)",
  "package.paths",
  "x-benes-api-key.header",
  "export BENES_API_AUTH_TOKEN=",
  "EXPORT FOO",
  "BENES_API_AUTH_TOKEN=secret",
  "BENES_API_AUTH_TOKEN",
  "# journaled Codex config",
  "curl -s http://127.0.0.1:23100/healthz",
  "-H 'Authorization: Bearer'",
  "-d '{\"model\":\"gpt-4o\"}'",
  "\\ -H Authorization",
  "\\\\  -d {",
  "benes start --inject",
  "BENES sync",
  "codex login",
  "Codex exec",
  "Authorization: Bearer",
  "Bearer token",
  "Content-Type: application/json",
  "x-benes-api-key",
  "model=gpt-4o",
  "supportsTier=true",
  "{",
  "[",
  "]",
  "}",
  '"model": "gpt-4o"',
  "oauth",
  "passthrough",
  "forward",
  "local",
  "key",
  "openai-chat",
  "openai-responses",
  "anthropic",
  "google",
  "azure-openai",
  "cursor",
  "Local",
  "latest",
  "preview",
  "thinking",
  "effort",
  "beta",
  "metadata",
  "system",
  "model",
  "resolved",
  "requestedTier",
  "configuredTier",
  "responseTier",
  "supportsTier",
];

const UI_LITERALS = [
  "Search",
  "Search providers",
  "Default model",
  "No matching providers",
  "Save",
  "Cancel",
  "Edit",
  "Add provider",
  "Requests per minute",
  "Needs attention",
  "Logged in",
  "Custom provider",
  "Suchen",
  "Speichern",
  "Abbrechen",
  "Hello world",
  "OpenAI",
  "gpt-4o",
  "claude-sonnet-5",
  "FOO",
  "MS",
  "Free",
  "Paid",
  "Accounts",
];

test("technical literals stay allowlisted and UI copy stays flagged", () => {
  for (const value of TECHNICAL_LITERALS) {
    assert.equal(isTechnicalLiteral(value), true, `expected technical: ${JSON.stringify(value)}`);
  }
  for (const value of UI_LITERALS) {
    assert.equal(isTechnicalLiteral(value), false, `expected UI copy: ${JSON.stringify(value)}`);
  }
});

test("non-UI JSX attribute names skip chrome but keep title and visible aria copy", () => {
  for (const name of ["className", "id", "href", "src", "role", "viewBox", "data-id", "data-testid", "aria-hidden", "aria-expanded", "aria-labelledby"]) {
    assert.equal(isNonUiJsxAttributeName(name), true, name);
  }
  for (const name of ["title", "aria-label", "aria-description", "aria-roledescription", "placeholder", "alt"]) {
    assert.equal(isNonUiJsxAttributeName(name), false, name);
  }
});

test("code samples, CSS keys, and identifier callees are the non-UI ancestor policy", () => {
  assert.equal(isCodeSampleTag("pre"), true);
  assert.equal(isCodeSampleTag("kbd"), true);
  assert.equal(isCodeSampleTag("div"), false);
  assert.equal(isNonUiCssPropertyName("transform"), true);
  assert.equal(isNonUiCssPropertyName("color"), false);
  assert.equal(isNonUiCalleeIdentifier("fetch"), true);
  assert.equal(isNonUiCalleeIdentifier("String"), true);
  assert.equal(isNonUiCalleeIdentifier("JSON.stringify"), true);
  assert.equal(isNonUiCalleeIdentifier("t"), false);
});

test("non-UI ancestor walk skips t()/Trans/code/fetch and keeps title/label copy", () => {
  const preText = withParent({ type: "JSXText" }, jsxElement("pre"));
  assert.equal(isInsideNonUiContext(preText), true);

  const divText = withParent({ type: "JSXText" }, jsxElement("div"));
  assert.equal(isInsideNonUiContext(divText), false);

  const classNameLiteral = withParent({ type: "Literal" }, jsxAttr("className"));
  assert.equal(isInsideNonUiContext(classNameLiteral), true);

  const titleLiteral = withParent({ type: "Literal" }, jsxAttr("title"));
  assert.equal(isInsideNonUiContext(titleLiteral), false);

  const tCall = { type: "CallExpression", callee: { type: "Identifier", name: "t" } };
  assert.equal(isInsideNonUiContext(withParent({ type: "Literal" }, tCall)), true);

  const trans = jsxElement("Trans");
  assert.equal(isInsideNonUiContext(withParent({ type: "JSXText" }, trans)), true);

  const fetchCall = { type: "CallExpression", callee: { type: "Identifier", name: "fetch" } };
  assert.equal(isInsideNonUiContext(withParent({ type: "Literal" }, fetchCall)), true);

  const stringify = { type: "CallExpression", callee: { type: "MemberExpression" } };
  assert.equal(isInsideNonUiContext(withParent({ type: "Literal" }, stringify)), false);
});

test("quiet resource fetches skip only an existing in-flight request", () => {
  assert.equal(resourceReplaceInflight(undefined), true);
  assert.equal(resourceReplaceInflight(true), true);
  assert.equal(resourceReplaceInflight(false), false);
  assert.equal(shouldSkipQuietResourceFetch(true, false), true);
  assert.equal(shouldSkipQuietResourceFetch(true, true), false);
  assert.equal(shouldSkipQuietResourceFetch(false, false), false);
});

test("cached values including falsy stay on screen unless forceLoading", () => {
  const cachedFalse = snapshot({ data: false, loading: false });
  assert.deepEqual(resourceInFlightSnapshot(cachedFalse), {
    ...cachedFalse,
    refreshing: true,
  });
  assert.equal(resourceInFlightSnapshot(snapshot({ data: 0 })).loading, false);
  assert.equal(resourceInFlightSnapshot(snapshot({ data: "" })).loading, false);
  assert.equal(resourceInFlightSnapshot(snapshot({ data: undefined })).loading, true);
  assert.equal(resourceInFlightSnapshot(snapshot({ data: { ok: true } }), true).loading, true);
});

test("resource commit ignores owner aborts and stale generations, and settles timeout/errors", () => {
  const previous = snapshot({ data: { n: 1 }, loading: true, refreshing: true, hasSucceeded: true, lastAttemptOk: true });

  assert.equal(commitResourceFetch(identity({ aborted: true }), { status: "success", data: { n: 2 } }, previous).kind, "ignore");
  assert.equal(commitResourceFetch(identity({ storeGeneration: 2 }), { status: "success", data: { n: 2 } }, previous).kind, "ignore");
  assert.equal(commitResourceFetch(identity({ aborted: true }), { status: "error", error: new Error("nope") }, previous).kind, "ignore");
  assert.equal(commitResourceFetch(identity({ storeGeneration: 9 }), { status: "error", error: new Error("nope") }, previous).kind, "ignore");

  const success = commitResourceFetch(identity(), { status: "success", data: { n: 2 } }, previous);
  assert.deepEqual(success, {
    kind: "success",
    snapshot: {
      data: { n: 2 },
      error: undefined,
      loading: false,
      refreshing: false,
      hasSucceeded: true,
      lastAttemptOk: true,
    },
    seedNeedsRevalidate: false,
  });

  const timeout = commitResourceFetch(
    identity({ aborted: true, timedOut: true }),
    { status: "error", error: resourceTimeoutError(30_000) },
    previous,
  );
  assert.equal(timeout.kind, "failure");
  assert.equal(timeout.snapshot.data, previous.data);
  assert.equal(timeout.snapshot.loading, false);
  assert.equal(timeout.snapshot.refreshing, false);
  assert.equal(timeout.snapshot.lastAttemptOk, false);
  assert.equal(timeout.snapshot.hasSucceeded, true);
  assert.equal(timeout.snapshot.error.message, "resource request timed out after 30000ms");

  const normalized = commitResourceFetch(identity(), { status: "error", error: undefined }, previous);
  assert.equal(normalized.kind, "failure");
  assert.equal(resourceNormalizeLoadError(undefined).message, "resource load failed");
  assert.equal(normalized.snapshot.error.message, "resource load failed");
});

test("select keyboard and presentation policy keep combobox behavior", () => {
  assert.equal(selectSelectedIndex(0, -1), 0);
  assert.equal(selectSelectedIndex(3, -1), 0);
  assert.equal(selectSelectedIndex(3, 2), 2);
  assert.equal(selectActiveIndex(false, 4, 3, 1), 1);
  assert.equal(selectActiveIndex(true, 0, 3, 1), 1);
  assert.equal(selectActiveIndex(true, 4, null, 1), 1);
  assert.equal(selectActiveIndex(true, 4, 9, 1), 3);
  assert.equal(selectShouldRenderMenu(true, false), true);
  assert.equal(selectShouldRenderMenu(true, true), false);
  assert.equal(selectDropdownClassName(true, "right", "right"), "select-dropdown select-dropdown-portal");
  assert.equal(selectDropdownClassName(false, "right", "right"), "select-dropdown select-dropdown-right select-dropdown-beside");
  assert.equal(selectOptionClassName(true, true), "select-option active select-option-active");
  assert.equal(selectChevronTransform(true), "rotate(90deg)");
  assert.equal(selectChevronTransform(false, "down"), "rotate(90deg)");
  assert.equal(selectChevronTransform(true, "down"), "rotate(-90deg)");
  assert.equal(selectListboxControls(true, "lb"), "lb");
  assert.equal(selectListboxControls(false, "lb"), undefined);
  assert.equal(selectActiveOptionId(true, true, "lb-1"), "lb-1");
  assert.equal(selectActiveOptionId(true, false, "lb-1"), undefined);
  assert.deepEqual(selectDropdownStyle(false, { top: 1 }, { color: "red" }), { color: "red" });
  assert.deepEqual(selectDropdownStyle(true, { top: 1 }, { color: "red" }), { top: 1, zIndex: 60, color: "red" });

  assert.deepEqual(selectTriggerKeyCommand({ key: "ArrowDown", open: false, disabled: false, activeIndex: 2, selectedIndex: 1, optionCount: 4 }), { type: "move", index: 1 });
  assert.deepEqual(selectTriggerKeyCommand({ key: "ArrowDown", open: true, disabled: false, activeIndex: 2, selectedIndex: 1, optionCount: 4 }), { type: "move", index: 3 });
  assert.deepEqual(selectTriggerKeyCommand({ key: "ArrowUp", open: true, disabled: false, activeIndex: 0, selectedIndex: 1, optionCount: 4 }), { type: "move", index: 0 });
  assert.deepEqual(selectTriggerKeyCommand({ key: "Home", open: true, disabled: false, activeIndex: 2, selectedIndex: 1, optionCount: 4 }), { type: "move", index: 0 });
  assert.deepEqual(selectTriggerKeyCommand({ key: "End", open: true, disabled: false, activeIndex: 2, selectedIndex: 1, optionCount: 4 }), { type: "move", index: 3 });
  assert.deepEqual(selectTriggerKeyCommand({ key: "Enter", open: false, disabled: false, activeIndex: 2, selectedIndex: 1, optionCount: 4 }), { type: "open", index: 1 });
  assert.deepEqual(selectTriggerKeyCommand({ key: " ", open: true, disabled: false, activeIndex: 2, selectedIndex: 1, optionCount: 4 }), { type: "commit", index: 2 });
  assert.deepEqual(selectTriggerKeyCommand({ key: "Escape", open: true, disabled: false, activeIndex: 2, selectedIndex: 1, optionCount: 4 }), { type: "close" });
  assert.deepEqual(selectTriggerKeyCommand({ key: "Escape", open: false, disabled: false, activeIndex: 2, selectedIndex: 1, optionCount: 4 }), { type: "noop" });
  assert.deepEqual(selectTriggerKeyCommand({ key: "Tab", open: true, disabled: false, activeIndex: 2, selectedIndex: 1, optionCount: 4 }), { type: "tab-commit", index: 2 });
  assert.deepEqual(selectTriggerKeyCommand({ key: "a", open: true, disabled: false, activeIndex: 2, selectedIndex: 1, optionCount: 4 }), { type: "noop" });
  assert.deepEqual(selectTriggerKeyCommand({ key: "Enter", open: true, disabled: true, activeIndex: 2, selectedIndex: 1, optionCount: 4 }), { type: "noop" });

  const calls = [];
  applySelectTriggerKey(
    { key: "Enter", preventDefault: () => calls.push("prevent") },
    {
      disabled: false,
      open: true,
      activeIndex: 1,
      selectedIndex: 0,
      optionCount: 2,
      options: [{ value: "a" }, { value: "b" }],
      onChange: (value) => calls.push(value),
      openAt: (index) => calls.push(`open:${index}`),
      close: () => calls.push("close"),
      selectIndex: (index) => calls.push(`select:${index}`),
      setOpen: (open) => calls.push(`setOpen:${open}`),
    },
  );
  assert.deepEqual(calls, ["prevent", "select:1"]);
});

test("catalog picker policy preserves filter tables, account status, and empty states", () => {
  assert.equal(accessFromTier(), "all");
  assert.equal(accessFromTier("accounts"), "accounts");
  assert.equal(accessFromTier("free"), "free");
  assert.equal(accessFromTier("paid"), "paid");
  assert.deepEqual(CATALOG_ACCESS_FILTER_KEYS.map((row) => row.value), ["all", "paid", "free", "local", "accounts"]);
  assert.deepEqual(CATALOG_CONNECTION_FILTER_KEYS.map((row) => row.value), ["all", "oauth", "key", "local"]);
  assert.equal(isCatalogAccessFilter("accounts"), true);
  assert.equal(isCatalogAccessFilter("other"), false);
  assert.equal(isCatalogConnectionFilter("oauth"), true);
  assert.equal(isCatalogConnectionFilter("wire"), false);

  const row = { id: "openai", label: "OpenAI", kind: "oauth" };
  assert.equal(catalogAccountStatusText(row, { loggedIn: true, email: "a@b" }, t), "a@b");
  assert.equal(catalogAccountStatusText({ ...row, statusLabel: "ok" }, { loggedIn: true }, t), "ok");
  assert.equal(catalogAccountStatusText(row, { loggedIn: true }, t), "modal.accountLoggedIn");
  assert.equal(catalogAccountStatusText(row, { loggedIn: false, error: "expired" }, t), "expired");
  assert.equal(catalogAccountStatusText(row, undefined, t), "modal.accountLoggedOut");

  const accounts = [
    { id: "openai", label: "ChatGPT", kind: "oauth" },
    { id: "anthropic", label: "Claude", kind: "oauth" },
  ];
  assert.equal(filterAccountRows(accounts, "").length, 2);
  assert.deepEqual(filterAccountRows(accounts, "GPT").map((item) => item.id), ["openai"]);
  assert.deepEqual(filterAccountRows(accounts, "ANTH").map((item) => item.id), ["anthropic"]);

  assert.equal(catalogEmptyKind({ presetsLoading: true, showAccounts: false, accountCount: 0, rowCount: 0 }), "loading");
  assert.equal(catalogEmptyKind({ presetsLoading: false, showAccounts: true, accountCount: 0, rowCount: 0 }), "no-match");
  assert.equal(catalogEmptyKind({ presetsLoading: false, showAccounts: false, accountCount: 0, rowCount: 0 }), "no-match");
  assert.equal(catalogEmptyKind({ presetsLoading: false, showAccounts: false, accountCount: 0, rowCount: 1 }), null);

  assert.equal(railSubtitle({ auth: "key", local: false, tier: "paid" }, t), "modal.access.paid · modal.badge.apiKey");
  assert.equal(railSubtitle({ id: "custom", auth: "key", local: false, tier: "paid" }, t), "modal.access.manual · modal.badge.apiKey");
  assert.equal(railSubtitle({ auth: "oauth", local: false, tier: "paid" }, t), "modal.access.paid · modal.badge.oauth + modal.badge.apiKey");
  assert.equal(railSubtitle({ auth: "forward", local: false, tier: "accounts" }, t), "modal.access.paid · modal.badge.codexLogin");
  assert.equal(railSubtitle({ auth: "local", local: true, tier: "free" }, t), "modal.badge.local · modal.badge.local");
  assert.equal(catalogAccessLabel({ local: false, tier: "paid" }, t), "modal.access.paid");
  assert.equal(catalogAccessLabel({ local: false, tier: "free" }, t), "modal.badge.free");
  assert.equal(catalogAccessLabel({ local: true, tier: "paid" }, t), "modal.badge.local");
  assert.equal(catalogConnectionLabel({ auth: "oauth", local: false }, t), "modal.badge.oauth + modal.badge.apiKey");
  assert.equal(catalogConnectionLabel({ auth: "key", local: false }, t), "modal.badge.apiKey");
  assert.equal(catalogConnectionLabel({ auth: "local", local: true, id: "lm-studio" }, t), "modal.badge.local");
  assert.equal(catalogConnectionLabel({ auth: "local", local: true, id: "litellm", adapter: "openai-chat" }, t), "pws.type.selfHosted");
  assert.equal(catalogConnectionIsSelfHosted({ id: "litellm" }), true);
  assert.equal(catalogConnectionIsSelfHosted({ id: "lm-studio" }), false);
});

test("leftover utility and i18n files contain no structural debt", () => {
  assert.deepEqual(structuralDiagnostics(), []);
});
