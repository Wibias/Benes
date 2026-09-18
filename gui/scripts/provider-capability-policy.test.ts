import assert from "node:assert/strict";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

import {
  diagnosticsFromOxlintJson,
  summarizeStructuralDiagnostics,
} from "./check-structural-baseline.ts";
import { accessDescriptorPolicy } from "../src/provider-workspace/access-policy.ts";
import { quotaAccessPolicy } from "../src/provider-workspace/quota-policy.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");
const oxlintBin = path.join(guiRoot, "node_modules", "oxlint", "bin", "oxlint");
const configPath = path.join(guiRoot, ".oxlintrc.json");
const targetFiles = [
  path.join(guiRoot, "src", "provider-workspace", "auth.ts"),
  path.join(guiRoot, "src", "provider-workspace", "access-policy.ts"),
  path.join(guiRoot, "src", "provider-workspace", "quota-access.ts"),
  path.join(guiRoot, "src", "provider-workspace", "quota-policy.ts"),
];

function structuralDiagnostics() {
  const result = spawnSync(
    process.execPath,
    [oxlintBin, "-c", configPath, "--format=json", ...targetFiles],
    { cwd: guiRoot, encoding: "utf8" },
  );
  if (result.error) throw result.error;
  const stdout = String(result.stdout ?? "").trim();
  assert.ok(stdout, `Oxlint provider-capability scan produced no output:\n${String(result.stderr ?? "")}`);
  return summarizeStructuralDiagnostics(diagnosticsFromOxlintJson(JSON.parse(stdout), guiRoot));
}

const emptyAccess = { methods: [], activitySupported: false };

function policyItem(overrides = {}) {
  return {
    name: "example",
    authMode: "key",
    hasApiKey: true,
    keyOptional: false,
    ...overrides,
  };
}

test("batch-three provider capability files contain no structural debt", () => {
  assert.deepEqual(structuralDiagnostics(), []);
});

test("access policy preserves existing descriptors and account overlays", () => {
  const existing = {
    methods: [{ id: "api", kind: "api-key", connectionId: "openai-apikey", connectionPresent: false }],
    selection: { supported: true, mode: "pool", methodId: "oauth" },
  };
  assert.equal(
    accessDescriptorPolicy(policyItem({ access: existing }), {
      accountProvider: false,
      localProvider: false,
      apiLanePresent: true,
    }),
    existing,
  );

  const account = accessDescriptorPolicy(
    policyItem({ access: existing, codexAccountMode: "direct" }),
    { accountProvider: true, localProvider: false, apiLanePresent: true },
  );
  assert.equal(account.methods[0]?.connectionPresent, true);
  assert.equal(account.selection?.mode, "direct");
  assert.equal(existing.methods[0]?.connectionPresent, false);
  assert.equal(existing.selection?.mode, "pool");
});

test("access policy preserves account fallback lanes", () => {
  const descriptor = accessDescriptorPolicy(
    policyItem({ access: undefined, codexAccountMode: "direct", defaultAccess: "api" }),
    { accountProvider: true, localProvider: false, apiLanePresent: false },
  );
  assert.equal(descriptor.defaultMethodId, "api");
  assert.equal(descriptor.selection?.mode, "direct");
  assert.equal(descriptor.methods.find(method => method.id === "api")?.connectionPresent, false);
  assert.equal(descriptor.methods.find(method => method.id === "oauth")?.connectionPresent, true);
});

test("access policy preserves non-account auth precedence", () => {
  const cases = [
    [policyItem({ authMode: "forward" }), { localProvider: false }, emptyAccess],
    [policyItem({ authMode: "key" }), { localProvider: true }, emptyAccess],
    [policyItem({ authMode: "oauth", hasApiKey: false }), { localProvider: false }, {
      methods: [{ id: "oauth", kind: "oauth", connectionId: "example", supportsMultiple: true, connectionPresent: true }],
      defaultMethodId: "oauth",
      activitySupported: true,
    }],
    [policyItem({ authMode: "key", keyOptional: true, hasApiKey: false }), { localProvider: false }, emptyAccess],
    [policyItem({ authMode: "key", hasApiKey: false }), { localProvider: false }, {
      methods: [{ id: "api-key", kind: "api-key", connectionId: "example", supportsMultiple: true, poolSupported: true, connectionPresent: true }],
      defaultMethodId: "api-key",
      activitySupported: true,
    }],
    [policyItem({ authMode: "future", hasApiKey: true }), { localProvider: false }, {
      methods: [{ id: "api-key", kind: "api-key", connectionId: "example", supportsMultiple: true, poolSupported: true, connectionPresent: true }],
      defaultMethodId: "api-key",
      activitySupported: true,
    }],
    [policyItem({ authMode: "future", hasApiKey: false }), { localProvider: false }, emptyAccess],
  ];
  for (const [item, context, expected] of cases) {
    assert.deepEqual(
      accessDescriptorPolicy(item, { accountProvider: false, apiLanePresent: false, ...context }),
      expected,
    );
  }
});

test("quota policy preserves canonical-name precedence", () => {
  assert.deepEqual(
    quotaAccessPolicy({ name: "openrouter", authMode: "oauth", baseUrl: "https://chatgpt.com/backend-api/codex" }),
    { kind: "custom" },
  );
  assert.deepEqual(
    quotaAccessPolicy({ name: "google-antigravity", authMode: "oauth", baseUrl: "https://example.invalid" }),
    { kind: "custom" },
  );
  assert.deepEqual(
    quotaAccessPolicy({ name: "commandcode", authMode: "future", baseUrl: "https://example.invalid" }),
    { kind: "windows", windows: ["fiveHour", "weekly"] },
  );
});

test("quota policy preserves auth-mode and base-url precedence", () => {
  assert.deepEqual(
    quotaAccessPolicy({ name: "custom", authMode: "future", baseUrl: "https://chatgpt.com/backend-api/codex" }),
    { kind: "windows", windows: ["fiveHour", "weekly", "monthly"] },
  );
  assert.deepEqual(
    quotaAccessPolicy({ name: "custom", authMode: "oauth", baseUrl: "https://api.deepseek.com/v1" }),
    { kind: "none" },
  );
  assert.deepEqual(
    quotaAccessPolicy({ name: "custom", authMode: "key", baseUrl: "https://api.deepseek.com/v1/" }),
    { kind: "custom" },
  );
  assert.deepEqual(
    quotaAccessPolicy({ name: "custom", authMode: "future", baseUrl: "https://api.deepseek.com/v1" }),
    { kind: "none" },
  );
  assert.deepEqual(
    quotaAccessPolicy({ name: "custom", authMode: "key", baseUrl: "https://api.commandcode.ai" }),
    { kind: "none" },
  );
});
