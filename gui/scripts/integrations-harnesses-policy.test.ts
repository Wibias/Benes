import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

import {
  diagnosticsFromOxlintJson,
  summarizeStructuralDiagnostics,
} from "./check-structural-baseline.ts";
import { decodeKeysPayload, validCachedKeys } from "../src/pages/api-keys-decode.ts";
import {
  apiProtocolLabel,
  apiSourceLabel,
  modelSourceKind,
  readModelsCatalog,
} from "../src/api-access/model-catalog-view.ts";
import { modelTestErrorMessage, modelTestRequest } from "../src/api-access/model-tests.ts";
import { modelTestProtocols } from "../src/api-access/model-tests.ts";
import { DEFAULT_ENDPOINTS } from "../src/api-access/endpoints.ts";
import { overlayHarnessesAt } from "../src/pages/harnesses/live-overlay.ts";
import {
  isFileId,
  isNativeId,
  overlayAuth,
  overlayFile,
  overlayNative,
} from "../src/pages/harnesses/live-overlay.ts";
import { FILE_MANAGED_HARNESS_IDS } from "../src/pages/harnesses/types.ts";
import type { HarnessId } from "../src/pages/harnesses/types.ts";
import {
  isIntegrationRefusalEnvelope,
  loadIntegrationStates,
} from "../src/pages/integrations/integration-api.ts";
import { authTokenKey, badgeTone, listBadge } from "../src/pages/harnesses/presentation.ts";
import { decodeStoredSettings } from "../src/pages/harnesses/settings-decode.ts";
import { HARNESS_SIDECAR_SELECTIONS } from "../src/pages/harnesses/types.ts";
import {
  decodeHarnessProbe,
  harnessSidecarConfiguredKeys,
  harnessSidecarModalities,
  harnessSidecarPatchBody,
  harnessSidecarRequestActive,
  harnessSidecarSelection,
  saveHarnessSidecarOverride,
} from "../src/pages/harnesses/harness-api.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");
const oxlintBin = path.join(guiRoot, "node_modules", "oxlint", "bin", "oxlint");
const configPath = path.join(guiRoot, ".oxlintrc.json");
const targetFiles = [
  "src/components/apikeys-workspace/ApiKeysWorkspace.tsx",
  "src/components/apikeys-workspace/ApiKeysDetailPane.tsx",
  "src/components/apikeys-workspace/ApiKeysListPanel.tsx",
  "src/components/apikeys-workspace/ApiKeysOverviewSections.tsx",
  "src/components/apikeys-workspace/ClientConfigDetail.tsx",
  "src/components/apikeys-workspace/ClientConfigPanel.tsx",
  "src/components/subagents-workspace/SubagentDelegationSection.tsx",
  "src/components/subagents-workspace/delegation-slices.tsx",
  "src/components/subagents-workspace/SubagentsWorkspace.tsx",
  "src/components/subagents-workspace/SubagentsRail.tsx",
  "src/components/subagents-workspace/SubagentsDetail.tsx",
  "src/pages/ApiKeys.tsx",
  "src/pages/api-tab.ts",
  "src/pages/api-tab-strip.tsx",
  "src/pages/api-key-create-dialog.tsx",
  "src/pages/use-api-workspace-chrome.ts",
  "src/pages/api-keys-decode.ts",
  "src/pages/api-keys-actions.ts",
  "src/pages/api-keys-page-body.tsx",
  "src/pages/use-api-keys-page.ts",
  "src/pages/harnesses/Harnesses.tsx",
  "src/pages/harnesses/HarnessDetail.tsx",
  "src/pages/harnesses/live.ts",
  "src/pages/harnesses/live-overlay.ts",
  "src/pages/harnesses/presentation.ts",
  "src/pages/harnesses/SidecarPolicy.tsx",
  "src/pages/harnesses/settings-decode.ts",
  "src/pages/integrations/integration-api.ts",
  "src/pages/integrations/native-api.ts",
  "src/pages/integrations/refusal-copy.ts",
].map((relative) => path.join(guiRoot, relative));

function structuralDiagnostics() {
  const existing = targetFiles.filter((file) => existsSync(file));
  assert.ok(existing.length > 0, "integrations/harnesses policy scan found no target files");
  const result = spawnSync(
    process.execPath,
    [oxlintBin, "-c", configPath, "--format=json", ...existing],
    { cwd: guiRoot, encoding: "utf8" },
  );
  if (result.error) throw result.error;
  const stdout = String(result.stdout ?? "").trim();
  assert.ok(stdout, `Oxlint integrations/harnesses scan produced no output:\n${String(result.stderr ?? "")}`);
  return summarizeStructuralDiagnostics(diagnosticsFromOxlintJson(JSON.parse(stdout), guiRoot));
}

const AUTH_MATRIX = [
  { endpoint: "/v1/responses", bearer: "rejected", dedicated: "accepted", xApiKey: "required" },
];

function fileStatus(overrides = {}) {
  return {
    clientId: "opencode",
    state: "current",
    installed: true,
    configPath: "/tmp/opencode.json",
    snapshotCount: 1,
    retentionDegraded: false,
    ...overrides,
  };
}

test("stored harness settings reject arrays, unknown ids, and non-booleans", () => {
  assert.deepEqual(decodeStoredSettings(null), {});
  assert.deepEqual(decodeStoredSettings([]), {});
  assert.deepEqual(decodeStoredSettings({ nope: { autoDetect: true } }), {});
  assert.deepEqual(decodeStoredSettings({ claude: { autoDetect: "yes", autoApply: false } }), {
    claude: { autoApply: false },
  });
});

test("file overlay maps stale to update-needed and conflict to conflict", () => {
  const stale = overlayFile(fileStatus({ state: "stale", lastOpId: "op-1", appliedAt: "2026-01-01T00:00:00Z" }), "now");
  assert.equal(stale.applied, true);
  assert.equal(stale.issue, "update-needed");
  assert.equal(stale.drift, true);
  assert.equal(stale.snapshotId, "op-1");
  const conflict = overlayFile(fileStatus({ state: "conflict" }), "now");
  assert.equal(conflict.applied, false);
  assert.equal(conflict.issue, "conflict");
  const missing = overlayFile(undefined, "now");
  assert.equal(missing.installed, false);
  assert.equal(missing.lastDetectedAt, null);
});

test("native overlay and auth overlays preserve Claude subscription and Codex fallback", () => {
  const unsafe = overlayNative({
    clientId: "claude",
    state: "unsafe",
    installed: true,
    configPath: "/tmp/claude.json",
    desiredEnabled: true,
    disableBlocked: null,
  }, "now");
  assert.equal(unsafe.issue, "conflict");
  assert.equal(unsafe.applied, false);
  const seed = { kind: "none", methodKey: "harnesses.auth.none", clientId: "cid", scopes: ["a"], token: "none" };
  const oauth = overlayAuth(seed, "claude", { enabled: true, authMode: "subscription" });
  assert.equal(oauth.kind, "oauth");
  assert.equal(oauth.methodKey, "harnesses.auth.claudeOAuth");
  const codex = overlayAuth(seed, "codex", null);
  assert.equal(codex.token, "missing");
});

test("live overlay applies file, desktop drift, grok presence, and Codex routing", () => {
  const rows = overlayHarnessesAt({
    files: [fileStatus({ state: "stale", lastOpId: "op-9" })],
    natives: [],
    journal: [],
    claude: { enabled: true, authMode: "subscription" },
    desktop: {
      desiredEnabled: true,
      installed: true,
      observedKind: "ok",
      applied: true,
      stale: true,
      activeProfile: true,
      appliedAt: "2026-01-02T00:00:00Z",
    },
    registrations: {
      grok: { configPath: "/tmp/grok.toml", present: true, catalogue: 1, registered: 1, current: true },
    },
    codex: { routingInjected: true, recommendedCommand: null },
    settings: {},
    probes: [],
  }, "now");
  const opencode = rows.find((row) => row.id === "opencode");
  const desktop = rows.find((row) => row.id === "claude-desktop");
  const grok = rows.find((row) => row.id === "grok");
  const codex = rows.find((row) => row.id === "codex");
  assert.equal(opencode?.applied, true);
  assert.equal(opencode?.issue, "update-needed");
  assert.equal(desktop?.issue, "update-needed");
  assert.equal(desktop?.drift, true);
  assert.equal(desktop?.lastAppliedAt, "2026-01-02T00:00:00Z");
  assert.equal(grok?.installed, true);
  assert.equal(codex?.applied, true);
  assert.equal(listBadge(opencode), "harnesses.badge.applied");
  assert.equal(badgeTone(desktop), "update");
  assert.equal(authTokenKey("missing"), "harnesses.auth.missing");
});

test("API key decode rejects a missing matrix and malformed usage", () => {
  assert.equal(decodeKeysPayload({ keys: [] }), null);
  assert.equal(decodeKeysPayload({
    keys: [{ id: "k1", name: "n", prefix: "sk", createdAt: "2026-01-01", usage: { requests7d: 1 } }],
    authMatrix: AUTH_MATRIX,
  }), null);
  const decoded = decodeKeysPayload({
    keys: [{ id: "k1", name: "n", prefix: "sk", createdAt: "2026-01-01", usage: { requests7d: 1, totalRequests: 2 } }],
    authMatrix: AUTH_MATRIX,
    endpoint: "http://127.0.0.1:23100/v1/responses",
    attributionSince: "2026-01-01T00:00:00Z",
    historyTruncated: true,
  });
  assert.equal(decoded?.keys.length, 1);
  assert.equal(decoded?.historyTruncated, true);
  assert.equal(decoded?.attributionSince, "2026-01-01T00:00:00Z");
  assert.equal(validCachedKeys(decoded), decoded);
  assert.equal(validCachedKeys({ ...decoded, authMatrix: [] }), null);
});

test("models catalog decode classifies owned_by and builds protocol-specific tests", () => {
  assert.equal(readModelsCatalog(null), null);
  const rows = readModelsCatalog({
    data: [
      { id: "gpt-4o", owned_by: "openai" },
      { id: "combo/fast" },
      { id: 12 },
    ],
  });
  assert.equal(rows?.[0]?.id, "combo/fast");
  assert.equal(modelSourceKind(rows[0]), "combo");
  assert.equal(modelSourceKind(rows[1]), "native");
  const request = modelTestRequest("responses", "gpt-4o", DEFAULT_ENDPOINTS);
  assert.equal(request.url, DEFAULT_ENDPOINTS.responses);
  assert.equal(request.body.model, "gpt-4o");
  assert.equal(apiSourceLabel(rows[1], (key) => key, (provider) => provider), "api.sourceNative");
  assert.equal(apiProtocolLabel("chat", (key) => key), "api.protocolChatCompletions");
  assert.deepEqual(modelTestProtocols({ provider: "openai" }), ["responses", "chat"]);
  assert.deepEqual(modelTestProtocols({ provider: "combo" }), ["responses", "chat"]);
  assert.deepEqual(modelTestProtocols({ provider: "anthropic" }), ["responses", "chat", "messages"]);
  assert.deepEqual(modelTestProtocols({ provider: "anthropic-api" }), ["responses", "chat", "messages"]);
  assert.equal(
    modelTestErrorMessage(`{"error":{"message":"provider returned HTTP 400: Unknown parameter: 'max_output_tokens'."}}`, 502),
    "provider returned HTTP 400: Unknown parameter: 'max_output_tokens'.",
  );
  assert.equal(modelTestErrorMessage("not-json", 502), "not-json");
  assert.equal(modelTestErrorMessage("{", 502), "HTTP 502");
});

const SIDECAR_PROBE = {
  clientId: "opencode",
  detectPath: null,
  logPath: null,
  running: null,
  token: "none",
  settings: {
    autoDetect: true,
    autoApply: false,
    retainSnapshot: true,
    allowRestart: false,
    sidecars: { webSearch: "enabled" },
  },
  sidecarPolicy: {
    capabilities: { identityStampable: true, webSearch: true, vision: true },
    configured: {
      webSearch: { enabled: true, source: "harness_override" },
      vision: { enabled: true, source: "global" },
    },
  },
};

function sidecarProbe(overrides = {}) {
  const probe = decodeHarnessProbe({ ...SIDECAR_PROBE, ...overrides });
  assert.ok(probe, "sidecar probe must decode");
  return probe;
}

test("each supported sidecar modality offers Use global, On, and Off", () => {
  assert.deepEqual(HARNESS_SIDECAR_SELECTIONS, ["global", "enabled", "disabled"]);
  const probe = sidecarProbe();
  assert.deepEqual(harnessSidecarModalities(probe.sidecarPolicy), ["webSearch", "vision"]);
  assert.equal(harnessSidecarSelection(probe.sidecarPolicy, "webSearch"), "enabled");
  assert.equal(harnessSidecarSelection(probe.sidecarPolicy, "vision"), "global");
});

test("web search and vision overrides stay independent", () => {
  const probe = sidecarProbe({
    settings: { ...SIDECAR_PROBE.settings, sidecars: { webSearch: "enabled", vision: "disabled" } },
    sidecarPolicy: {
      capabilities: { identityStampable: true, webSearch: true, vision: true },
      configured: {
        webSearch: { enabled: true, source: "harness_override" },
        vision: { enabled: false, source: "harness_override" },
      },
    },
  });
  assert.equal(harnessSidecarSelection(probe.sidecarPolicy, "webSearch"), "enabled");
  assert.equal(harnessSidecarSelection(probe.sidecarPolicy, "vision"), "disabled");
});

test("unsupported and non-stampable modalities expose no controls", () => {
  const partial = sidecarProbe({
    sidecarPolicy: {
      capabilities: { identityStampable: true, webSearch: true, vision: false },
      configured: {
        webSearch: { enabled: true, source: "global" },
        vision: { enabled: true, source: "global" },
      },
    },
  });
  assert.deepEqual(harnessSidecarModalities(partial.sidecarPolicy), ["webSearch"]);
  const nonStampable = sidecarProbe({
    sidecarPolicy: {
      capabilities: { identityStampable: false, webSearch: true, vision: true },
      configured: { webSearch: { enabled: true, source: "global" } },
    },
  });
  assert.deepEqual(harnessSidecarModalities(nonStampable.sidecarPolicy), []);
  assert.deepEqual(harnessSidecarModalities(null), []);
});

test("the configured line is derived from the server response", () => {
  const probe = sidecarProbe();
  assert.deepEqual(harnessSidecarConfiguredKeys(probe.sidecarPolicy, "webSearch"), {
    state: "harnesses.sidecar.mode.enabled",
    source: "harnesses.sidecar.source.harnessOverride",
  });
  assert.deepEqual(harnessSidecarConfiguredKeys(probe.sidecarPolicy, "vision"), {
    state: "harnesses.sidecar.mode.enabled",
    source: "harnesses.sidecar.source.global",
  });
  const globalOff = sidecarProbe({
    settings: { ...SIDECAR_PROBE.settings, sidecars: {} },
    sidecarPolicy: {
      capabilities: { identityStampable: true, webSearch: true, vision: true },
      configured: { webSearch: { enabled: false, source: "global" } },
    },
  });
  assert.deepEqual(harnessSidecarConfiguredKeys(globalOff.sidecarPolicy, "webSearch"), {
    state: "harnesses.sidecar.mode.disabled",
    source: "harnesses.sidecar.source.global",
  });
  assert.equal(harnessSidecarConfiguredKeys(globalOff.sidecarPolicy, "vision"), null);
});

test("a stale or unapplied Harness must not claim request-active identity", () => {
  assert.equal(harnessSidecarRequestActive({ installed: true, applied: true, issue: "none" }), true);
  assert.equal(harnessSidecarRequestActive({ installed: true, applied: true, issue: "update-needed" }), false);
  assert.equal(harnessSidecarRequestActive({ installed: true, applied: true, issue: "conflict" }), false);
  assert.equal(harnessSidecarRequestActive({ installed: true, applied: false, issue: "none" }), false);
  assert.equal(harnessSidecarRequestActive({ installed: false, applied: false, issue: "none" }), false);
});

test("Use global sends JSON null for one modality and never both", () => {
  assert.deepEqual(harnessSidecarPatchBody("opencode", "webSearch", "global"), {
    clientId: "opencode",
    sidecars: { webSearch: null },
  });
  assert.deepEqual(harnessSidecarPatchBody("opencode", "webSearch", "enabled"), {
    clientId: "opencode",
    sidecars: { webSearch: "enabled" },
  });
  assert.deepEqual(harnessSidecarPatchBody("opencode", "webSearch", "disabled"), {
    clientId: "opencode",
    sidecars: { webSearch: "disabled" },
  });
  assert.deepEqual(harnessSidecarPatchBody("opencode", "vision", "global"), {
    clientId: "opencode",
    sidecars: { vision: null },
  });
  assert.deepEqual(Object.keys(harnessSidecarPatchBody("opencode", "vision", "enabled").sidecars), ["vision"]);
  assert.deepEqual(Object.keys(harnessSidecarPatchBody("opencode", "webSearch", "disabled").sidecars), ["webSearch"]);
});

test("a sidecar save sends the precise patch and always reloads canonical state", async () => {
  const originalFetch = globalThis.fetch;
  const requests = [];
  let reloads = 0;
  const reload = async () => { reloads += 1; };
  try {
    globalThis.fetch = (async (_url, init) => {
      requests.push({ method: String(init?.method ?? "GET"), body: JSON.parse(String(init?.body ?? "{}")) });
      return new Response(JSON.stringify({ ok: true }), { status: 200 });
    });
    await saveHarnessSidecarOverride({
      apiBase: "",
      clientId: "opencode",
      modality: "webSearch",
      selection: "global",
      reload,
    });
    assert.deepEqual(requests, [{
      method: "PUT",
      body: { clientId: "opencode", sidecars: { webSearch: null } },
    }]);
    assert.equal(reloads, 1);

    globalThis.fetch = (async () => new Response(
      JSON.stringify({ error: { code: "write_failed", message: "sidecar override was not stored" } }),
      { status: 500 },
    ));
    reloads = 0;
    await assert.rejects(
      () => saveHarnessSidecarOverride({
        apiBase: "",
        clientId: "opencode",
        modality: "vision",
        selection: "disabled",
        reload,
      }),
      /harness sidecar policy could not be saved/,
    );
    assert.equal(reloads, 1, "a failed save must still refetch canonical server state");
  } finally {
    globalThis.fetch = originalFetch;
  }
});

test("sidecar policy never enters browser storage", () => {
  const probe = sidecarProbe();
  assert.equal("sidecars" in probe.settings, false);
  assert.equal(probe.sidecarPolicy.webSearch.override, "enabled");
  assert.equal(probe.sidecarPolicy.vision.override, null);
  assert.deepEqual(
    decodeStoredSettings({ opencode: { autoDetect: true, sidecars: { webSearch: "enabled" } } }),
    { opencode: { autoDetect: true } },
  );
  const component = readFileSync(path.join(guiRoot, "src", "pages", "harnesses", "SidecarPolicy.tsx"), "utf8");
  assert.equal(component.includes("localStorage"), false);
  assert.equal(component.includes("benes.harnesses.settings.v1"), false);
  for (const name of ["live.ts", "settings-decode.ts"]) {
    const source = readFileSync(path.join(guiRoot, "src", "pages", "harnesses", name), "utf8");
    assert.equal(/sidecar/i.test(source), false, name + " owns no sidecar policy state");
  }
});

test("integrations and harness files contain no structural debt", () => {
  assert.deepEqual(structuralDiagnostics(), []);
});

test("file-managed identity is exactly the ten managed clients, in order", () => {
  assert.deepEqual(FILE_MANAGED_HARNESS_IDS, [
    "opencode",
    "pi",
    "prime",
    "omp",
    "hermes",
    "openclaw",
    "kimi",
    "gajae",
    "dsh",
    "mcode",
  ]);
});

test("native Harnesses are not file-managed and vice versa", () => {
  const nativeOnly: HarnessId[] = ["claude", "claude-desktop", "codex", "grok"];
  for (const id of FILE_MANAGED_HARNESS_IDS) {
    assert.equal(isFileId(id), true, `${id} is file-managed`);
    assert.equal(isNativeId(id), false, `${id} is not native`);
  }
  for (const id of nativeOnly) {
    assert.equal(isNativeId(id), true, `${id} is native`);
    assert.equal(isFileId(id), false, `${id} is not file-managed`);
  }
});

test("the integration API refuses any client id it does not manage", async () => {
  const refusal = (clientId: unknown) => isIntegrationRefusalEnvelope({
    error: "conflict",
    code: "integration_conflict",
    clientId,
    state: "conflict",
    reason: "conflict",
    message: "the file changed underneath Benes",
  });
  for (const id of FILE_MANAGED_HARNESS_IDS) assert.equal(refusal(id), true, `${id} is addressable`);
  for (const id of ["claude", "claude-desktop", "codex", "grok", "nope", "", undefined]) {
    assert.equal(refusal(id), false, `${String(id)} is not addressable`);
  }

  const status = (clientId: unknown) => ({
    clientId,
    state: "current",
    installed: true,
    configPath: "C:/config.json",
    snapshotCount: 0,
    retentionDegraded: false,
  });
  const originalFetch = globalThis.fetch;
  try {
    globalThis.fetch = (async () => new Response(JSON.stringify({
      clients: [status("opencode"), status("claude"), status("codex"), status("grok"), status("nope")],
    }), { status: 200 }));
    const listed = await loadIntegrationStates("");
    assert.deepEqual(listed.clients.map((row) => row.clientId), ["opencode"]);
  } finally {
    globalThis.fetch = originalFetch;
  }
});
