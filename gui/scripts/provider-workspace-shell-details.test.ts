/**
 * #290 shell/details ownership and behaviour contracts.
 *
 * Two kinds of assertion live here: behaviour the re-authored shell/detail source
 * now owns as callable units (tab descriptors, listener-action decoding, fleet
 * board kind), and narrow ownership facts - that the Add Provider / provider-OAuth
 * source stayed out of the #290 modules, and that the detail tab id pair is minted
 * in one place.
 */
import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

import {
  diagnosticsFromOxlintJson,
  summarizeStructuralDiagnostics,
} from "./check-structural-baseline.ts";
import { detailTabDomIds, detailTabEntries } from "../src/provider-workspace/details-view.ts";
import { probeProviderConnection, syncProviderModels } from "../src/provider-workspace/detail-actions.ts";
import { workspaceBoardKind } from "../src/provider-workspace/workspace-shell.ts";
import { oauthLabel } from "../src/pages/providers-shared.ts";
import type { TFn } from "../src/i18n/shared.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");

const t: TFn = ((key: string) => key) as TFn;

/** #290-owned modules; none of them may depend on Add Provider / provider-OAuth source. */
const REAUTHORED_MODULES = [
  "src/provider-workspace/details-view.ts",
  "src/provider-workspace/detail-actions.ts",
  "src/components/provider-workspace/use-provider-workspace-reads.ts",
];

/** #291 owns these paths; #290 consumes none of them. */
const ADD_PROVIDER_OWNED = [
  "add-provider-account-setup",
  "add-provider-form-pane",
  "add-provider-form-sections",
  "add-provider-modal-body",
  "add-provider-modal-field",
  "AddProviderModal",
  "provider-presets",
  "use-providers-oauth",
  "use-add-provider-oauth",
  "cockpit-import",
];

const STRUCTURAL_TARGETS = [
  path.join(guiRoot, "src", "provider-workspace", "details-view.ts"),
  path.join(guiRoot, "src", "provider-workspace", "detail-actions.ts"),
  path.join(guiRoot, "src", "components", "provider-workspace", "use-provider-workspace-reads.ts"),
];

function readGuiSource(relative: string) {
  return readFile(path.join(guiRoot, relative), "utf8");
}

function structuralDiagnostics() {
  const result = spawnSync(
    process.execPath,
    [
      path.join(guiRoot, "node_modules", "oxlint", "bin", "oxlint"),
      "-c",
      path.join(guiRoot, ".oxlintrc.json"),
      "--format=json",
      ...STRUCTURAL_TARGETS,
    ],
    { cwd: guiRoot, encoding: "utf8" },
  );
  if (result.error) throw result.error;
  const stdout = String(result.stdout ?? "").trim();
  assert.ok(stdout, `Oxlint #290 scan produced no output:\n${String(result.stderr ?? "")}`);
  return summarizeStructuralDiagnostics(diagnosticsFromOxlintJson(JSON.parse(stdout), guiRoot));
}

async function withFetch<T>(impl: typeof fetch, run: () => Promise<T>): Promise<T> {
  const original = globalThis.fetch;
  globalThis.fetch = impl;
  try {
    return await run();
  } finally {
    globalThis.fetch = original;
  }
}

test("batch-five shell/details source contains no structural debt", () => {
  assert.deepEqual(structuralDiagnostics(), []);
});

test("detail tabs keep order and only offer Access when the provider has lanes", () => {
  assert.deepEqual(detailTabEntries(false, t), [
    { id: "overview", label: "pws.tab.overview" },
    { id: "configuration", label: "pws.tab.configuration" },
  ]);
  assert.deepEqual(detailTabEntries(true, t).map(entry => entry.id), ["overview", "access", "configuration"]);
  assert.deepEqual(detailTabEntries(true, t).map(entry => entry.label), [
    "pws.tab.overview",
    "pws.tab.access",
    "pws.tab.configuration",
  ]);
});

test("tab and panel ids are one pair so aria-controls cannot drift", () => {
  for (const tab of ["overview", "access", "configuration"] as const) {
    const ids = detailTabDomIds(tab);
    assert.deepEqual(ids, { tabId: `pws-tab-${tab}`, panelId: `pws-panel-${tab}` });
  }
});

test("connection probe ranks server text over the fallback keys and refreshes only on success", async () => {
  const spoken = await withFetch(
    async () => Response.json({ ok: true, message: "pong" }),
    () => probeProviderConnection("http://unit.test", "acme", t),
  );
  assert.deepEqual(spoken, { message: "pong", ok: true, refreshQuotas: true, refreshModels: false });

  const refused = await withFetch(
    async () => Response.json({ error: "boom" }, { status: 502 }),
    () => probeProviderConnection("http://unit.test", "acme", t),
  );
  assert.deepEqual(refused, { message: "boom", ok: false, refreshQuotas: false, refreshModels: false });

  const inapplicable = await withFetch(
    async () => Response.json({ applicable: false }),
    () => probeProviderConnection("http://unit.test", "acme", t),
  );
  assert.equal(inapplicable.message, "pws.connectionNotApplicable");

  const quiet = await withFetch(
    async () => Response.json({}),
    () => probeProviderConnection("http://unit.test", "acme", t),
  );
  assert.deepEqual(quiet, { message: "pws.connectionOk", ok: true, refreshQuotas: true, refreshModels: false });

  const offline = await withFetch(
    async () => { throw new Error("down"); },
    () => probeProviderConnection("http://unit.test", "acme", t),
  );
  assert.deepEqual(offline, { message: "prov.networkError", ok: false, refreshQuotas: false, refreshModels: false });
});

test("model sync re-reads the catalogue even when discovery is refused for the provider", async () => {
  const synced = await withFetch(
    async () => Response.json({ ok: true, models: ["gpt"] }),
    () => syncProviderModels("http://unit.test", "acme", t),
  );
  assert.deepEqual(synced, { message: "prov.health.models", ok: true, refreshQuotas: false, refreshModels: true });

  const disabled = await withFetch(
    async () => Response.json({ ok: false, applicable: false }),
    () => syncProviderModels("http://unit.test", "acme", t),
  );
  assert.deepEqual(disabled, {
    message: "models.emptyDiscoveryDisabled",
    ok: false,
    refreshQuotas: false,
    refreshModels: true,
  });

  const failed = await withFetch(
    async () => Response.json({ error: "no catalogue" }, { status: 502 }),
    () => syncProviderModels("http://unit.test", "acme", t),
  );
  assert.deepEqual(failed, { message: "no catalogue", ok: false, refreshQuotas: false, refreshModels: true });

  const offline = await withFetch(
    async () => { throw new Error("down"); },
    () => syncProviderModels("http://unit.test", "acme", t),
  );
  assert.deepEqual(offline, { message: "prov.networkError", ok: false, refreshQuotas: false, refreshModels: true });
});

test("the board is loading, empty, or ready from the aggregate alone", () => {
  assert.equal(workspaceBoardKind(null, 0), "loading");
  assert.equal(workspaceBoardKind(null, 3), "ready");
  const empty = {
    summary: { totalProviders: 0, healthy: 0, attention: 0, disabled: 0, exposedModels: 0 },
    providers: [],
    attention: [],
    availability: { modelsAvailable: 0, modelsUnavailable: 0, staleProviderCatalogues: null, lastModelSync: null },
    downstream: { harnessCount: null, routeCount: 0, subAgentModelCount: 0, affectedRouteCount: 0 },
    recentEvents: [],
  };
  assert.equal(workspaceBoardKind(empty, 0), "empty");
  assert.equal(workspaceBoardKind({
    ...empty,
    summary: { ...empty.summary, totalProviders: 1, healthy: 1 },
  }, 1), "ready");
});

test("OAuth provider labels fall back to the id without inheriting prototype names", () => {
  assert.equal(oauthLabel("anthropic"), "Anthropic (Claude)");
  assert.equal(oauthLabel("xai"), "xAI (Grok)");
  assert.equal(oauthLabel("some-new-provider"), "some-new-provider");
  assert.equal(oauthLabel("constructor"), "constructor");
  assert.equal(oauthLabel("toString"), "toString");
});

test("the re-authored #290 modules do not consume Add Provider or provider-OAuth source", async () => {
  for (const module of REAUTHORED_MODULES) {
    const source = await readGuiSource(module);
    for (const owned of ADD_PROVIDER_OWNED) {
      assert.ok(
        !source.includes(owned),
        `${module} must not depend on #291-owned ${owned}`,
      );
    }
  }
});

test("the detail tab id pair is minted once and consumed by name", async () => {
  const owner = await readGuiSource("src/provider-workspace/details-view.ts");
  assert.match(owner, /pws-tab-\$\{tab\}/);
  assert.match(owner, /pws-panel-\$\{tab\}/);
  for (const consumer of [
    "src/components/provider-workspace/ProviderDetails.tsx",
    "src/components/provider-workspace/details-tab-list.tsx",
  ]) {
    const source = await readGuiSource(consumer);
    assert.ok(source.includes("detailTabDomIds"), `${consumer} must mint ids through details-view`);
    assert.ok(!source.includes("pws-tab-"), `${consumer} must not hand-write the tab id`);
    assert.ok(!source.includes("pws-panel-"), `${consumer} must not hand-write the panel id`);
  }
});
