import assert from "node:assert/strict";
import test from "node:test";

import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";

import {
  catalogFamilyForPreset,
  groupCatalogFamilies,
  preferredCatalogFamilyMember,
  visibleCatalogFamilies,
} from "../src/components/provider-catalog/catalog-families.ts";
import { providerIconKnockout } from "../src/provider-icons.ts";

const FEATURED = [
  "command-code",
  "anthropic",
  "openai-apikey",
  "openrouter",
  "deepseek",
  "groq",
  "mimo-free",
  "lm-studio",
  "litellm",
];

function preset(id, label) {
  return { id, label };
}

const catalog = [
  preset("command-code", "Command Code - Auth"),
  preset("commandcode", "Command Code - API"),
  preset("openai", "OpenAI (Codex login)"),
  preset("openai-apikey", "OpenAI API"),
  preset("anthropic", "Anthropic Claude"),
  preset("anthropic-apikey", "Anthropic (API key)"),
  preset("groq", "Groq"),
  preset("openrouter", "OpenRouter"),
  preset("deepseek", "DeepSeek"),
  preset("mimo-free", "MiMo Free"),
  preset("xiaomi", "Xiaomi MiMo"),
  preset("lm-studio", "LM Studio (local)"),
  preset("litellm", "LiteLLM (self-hosted)"),
  preset("custom", "Custom provider"),
];

test("sibling config ids collapse to one catalog family", () => {
  const families = groupCatalogFamilies(catalog);
  const command = families.find(row => row.id === "command-code");
  assert.ok(command);
  assert.equal(command.label, "Command Code");
  assert.deepEqual(command.members.map(row => row.id), ["command-code", "commandcode"]);
  assert.equal(families.some(row => row.id === "commandcode"), false);
  assert.equal(families.filter(row => row.members.some(m => m.id === "openai" || m.id === "openai-apikey")).length, 1);
  assert.equal(families.find(row => row.id === "groq")?.members.length, 1);
  assert.equal(families.some(row => row.id === "custom"), false);
});

test("featured families stay one row each and keep the featured-first order", () => {
  const rows = visibleCatalogFamilies(catalog, "", FEATURED, () => true);
  assert.deepEqual(rows.slice(0, 4).map(row => row.id), [
    "command-code",
    "anthropic",
    "openai",
    "openrouter",
  ]);
  assert.equal(rows.filter(row => row.id === "openai").length, 1);
});

test("search and connection filters keep the whole family when any member matches", () => {
  const byApi = visibleCatalogFamilies(catalog, "api", FEATURED, () => true);
  assert.deepEqual(byApi.map(row => row.id).toSorted(), ["anthropic", "command-code", "openai"]);
  const oauthIds = new Set(["command-code", "anthropic"]);
  const oauth = visibleCatalogFamilies(catalog, "", FEATURED, row => oauthIds.has(row.id));
  assert.ok(oauth.some(row => row.id === "command-code"));
  assert.equal(oauth.find(row => row.id === "command-code")?.members.length, 2);
  assert.equal(oauth.some(row => row.id === "groq"), false);
});

test("Connection filter keys omit login; matchesConnectionFilter has no login branch", () => {
  const policy = readFileSync(
    join(dirname(fileURLToPath(import.meta.url)), "../src/components/provider-catalog/catalog-policy.ts"),
    "utf8",
  );
  const presets = readFileSync(
    join(dirname(fileURLToPath(import.meta.url)), "../src/components/provider-catalog/provider-presets.ts"),
    "utf8",
  );
  assert.match(policy, /CATALOG_CONNECTION_FILTER_KEYS[\s\S]*?value: "all"[\s\S]*?value: "oauth"[\s\S]*?value: "key"[\s\S]*?value: "local"/);
  assert.equal(policy.includes('value: "login"'), false);
  assert.equal(presets.includes('filter === "login"'), false);
  assert.equal(presets.includes('preset.auth === "forward" || preset.auth === "oauth"'), false);
});

test("dark-mode plate knockout covers the boxed catalog marks", () => {
  assert.equal(providerIconKnockout("parallel")?.invertDark, true);
  assert.equal(providerIconKnockout("synthetic")?.invertDark, true);
  assert.equal(providerIconKnockout("bizrouter")?.lightPlate, "#000000");
  assert.equal(providerIconKnockout("orcarouter") != null, true);
  assert.equal(providerIconKnockout("openai"), undefined);
});

test("connection defaults to an unconfigured sibling so a second setup is possible", () => {
  const family = catalogFamilyForPreset("command-code", catalog);
  assert.ok(family);
  assert.equal(preferredCatalogFamilyMember(family, []).id, "command-code");
  assert.equal(preferredCatalogFamilyMember(family, ["command-code"]).id, "commandcode");
  assert.equal(preferredCatalogFamilyMember(family, ["command-code", "commandcode"]).id, "command-code");
  assert.equal(preferredCatalogFamilyMember(family, ["command-code"], new Set(["commandcode"])).id, "commandcode");
});
