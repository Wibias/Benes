/**
 * Provider visual identity contract.
 *
 * The registry is data, so the interesting assertions are the invariants around it: one row
 * per id, a mark for every id, catalog membership keyed off the label rather than the mark,
 * and brands that never borrow each other's mark.
 */
import assert from "node:assert/strict";
import test from "node:test";

import type { TFn } from "../src/i18n/shared.ts";
import {
  formatNamespacedModelId,
  formatProviderDisplayName,
  isCatalogProviderId,
  providerDisplaySlug,
  providerIconKnockout,
  providerIconSrc,
} from "../src/provider-icons.ts";

/** Echoes the key so a catalogue lookup is observable. */
const t = ((key: string) => key) as unknown as TFn;

test("a known provider resolves to its mark under the served icon path", () => {
  assert.equal(providerIconSrc("openai"), "/provider-icons/openai.svg");
  assert.equal(providerIconSrc("anthropic"), "/provider-icons/claude-color.svg");
  assert.equal(providerIconSrc("google"), "/provider-icons/gemini-color.svg");
  assert.equal(providerIconSrc("github"), "/provider-icons/github-copilot-color.svg");
  assert.equal(providerIconSrc("cursor"), "/provider-icons/cursor-color.svg");
});

test("a provider id is matched case-insensitively", () => {
  assert.equal(providerIconSrc("OPENAI"), providerIconSrc("openai"));
  assert.equal(formatProviderDisplayName("OPENAI", t), formatProviderDisplayName("openai", t));
});

test("an unknown provider has no mark and is not a catalog id", () => {
  for (const unknown of ["", "unknown", "myCustomProvider", "company-gateway"]) {
    assert.equal(providerIconSrc(unknown), undefined, unknown);
    assert.equal(providerIconKnockout(unknown), undefined, unknown);
    assert.equal(isCatalogProviderId(unknown), false, unknown);
  }
});

test("one mark can serve several ids of the same brand", () => {
  const families = [
    ["anthropic", "anthropic-apikey"],
    ["openai", "openai-apikey", "chatgpt", "azure-openai"],
    ["opencode-free", "opencode-go", "opencode-zen"],
    ["volcengine", "volcengine-coding-plan", "volcengine-agent-plan"],
    ["minimax", "minimax-cn"],
    ["xiaomi", "xiaomi-mimo", "mimo", "mimo-free"],
  ];
  for (const family of families) {
    const marks = new Set(family.map(id => providerIconSrc(id)));
    assert.equal(marks.size, 1, `${family.join("/")} split across ${marks.size} marks`);
    assert.equal(marks.has(undefined), false, family.join("/"));
  }
});

test("a brand mark is never borrowed by a different brand", () => {
  const distinct = [
    "openai.svg",
    "claude-color.svg",
    "gemini-color.svg",
    "deepseek-color.svg",
    "mistral-color.svg",
    "ollama-color.svg",
    "openrouter-color.svg",
    "grok.svg",
    "groq-color.svg",
  ];
  const seen = new Map();
  for (const mark of distinct) {
    assert.equal(seen.has(mark), false, `duplicate expectation for ${mark}`);
    seen.set(mark, true);
  }
  const pairs: ReadonlyArray<readonly [string, string]> = [
    ["openai", "openai.svg"],
    ["anthropic", "claude-color.svg"],
    ["google", "gemini-color.svg"],
    ["deepseek", "deepseek-color.svg"],
    ["mistral", "mistral-color.svg"],
    ["ollama", "ollama-color.svg"],
    ["openrouter", "openrouter-color.svg"],
    ["xai", "grok.svg"],
    ["groq", "groq-color.svg"],
  ];
  for (const [id, expected] of pairs) {
    assert.equal(providerIconSrc(id), `/provider-icons/${expected}`, id);
  }
});

test("the third-party OpenCode mark stays the OpenCode mark", () => {
  assert.equal(providerIconSrc("opencode-go"), "/provider-icons/opencode.svg");
  assert.equal(providerIconSrc("opencode-zen"), "/provider-icons/opencode.svg");
  assert.equal(providerIconSrc("opencode-free"), "/provider-icons/opencode.svg");
});

test("catalog membership follows the label, not the mark or the plate", () => {
  // A mark or a plate treatment is a visual detail a custom provider may borrow.
  const markOnly = ["kiro", "nous", "umans", "neuralwatt", "firepass", "cerebras", "deepinfra"];
  for (const id of markOnly) {
    assert.notEqual(providerIconSrc(id), undefined, `${id} should still have a mark`);
    assert.equal(isCatalogProviderId(id), false, `${id} must not be a catalog id`);
  }
  const catalog = ["openai", "anthropic", "google", "deepseek", "cursor", "command-code"];
  for (const id of catalog) {
    assert.equal(isCatalogProviderId(id), true, id);
  }
});

test("a plate knockout is only reported where the mark needs one", () => {
  assert.deepEqual(providerIconKnockout("cursor"), { lightPlate: "#14120B", lightPlateRadius: "22%" });
  assert.deepEqual(providerIconKnockout("bizrouter"), { lightPlate: "#000000" });
  assert.deepEqual(providerIconKnockout("parallel"), { invertDark: true });
  assert.equal(providerIconKnockout("openai"), undefined);
  assert.equal(providerIconKnockout("anthropic"), undefined);
});

test("a translated label goes through the catalogue and a literal one does not", () => {
  assert.equal(formatProviderDisplayName("command-code", t), "provider.name.commandCodeAuth");
  assert.equal(formatProviderDisplayName("commandcode", t), "provider.name.commandCodeApi");
  assert.equal(formatProviderDisplayName("volcengine", t), "provider.name.volcengine");
  assert.equal(formatProviderDisplayName("openai", t), "OpenAI (Codex login)");
  assert.equal(formatProviderDisplayName("chatgpt", t), "ChatGPT");
});

test("an unknown simple id is title-cased and a mixed-case name is left alone", () => {
  assert.equal(formatProviderDisplayName("my-provider", t), "My Provider");
  assert.equal(formatProviderDisplayName("a-b-c", t), "A B C");
  assert.equal(formatProviderDisplayName("myCustomProvider", t), "myCustomProvider");
  assert.equal(formatProviderDisplayName("", t), "");
});

test("only the two Command Code ids get a distinguishable slug", () => {
  assert.equal(providerDisplaySlug("command-code"), "commandcode-auth");
  assert.equal(providerDisplaySlug("commandcode"), "commandcode-api");
  assert.equal(providerDisplaySlug("openai"), "openai");
  assert.equal(providerDisplaySlug("command-code-other"), "command-code-other");
});

test("a namespaced route is rewritten only for the Command Code ids", () => {
  assert.equal(
    formatNamespacedModelId("command-code/deepseek-deepseek-v4-flash", t),
    "commandcode-auth/deepseek-v4-flash",
  );
  assert.equal(
    formatNamespacedModelId("commandcode/deepseek-deepseek-v4-flash", t),
    "commandcode-api/deepseek-v4-flash",
  );
  assert.equal(formatNamespacedModelId("command-code/plain-model", t), "commandcode-auth/plain-model");
  assert.equal(formatNamespacedModelId("opencode-go/hy3", t), "opencode-go/hy3");
  assert.equal(formatNamespacedModelId("no-slash", t), "no-slash");
  assert.equal(formatNamespacedModelId("/leading", t), "/leading");
});
