import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import { dirname, join } from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

const root = dirname(fileURLToPath(import.meta.url));

test("applied harnesses without conflict are eligible for a model-list sync", async () => {
  const { harnessesEligibleForModelSync } = await import("../src/lib/provider-harness-sync.ts");
  assert.deepEqual(
    harnessesEligibleForModelSync([
      { id: "codex", applied: true, issue: "none" },
      { id: "opencode", applied: true, issue: "conflict" },
      { id: "claude", applied: false, issue: "none" },
      { id: "pi", applied: true, issue: "update-needed" },
    ]),
    ["codex", "pi"],
  );
});

test("model-list sync applies every eligible harness and counts failures", async () => {
  const { applyHarnessModelLists } = await import("../src/lib/provider-harness-sync.ts");
  const calls = [];
  const result = await applyHarnessModelLists(["codex", "opencode", "claude"], async (id) => {
    calls.push(id);
    if (id === "opencode") throw new Error("conflict");
  });
  assert.deepEqual(calls, ["codex", "opencode", "claude"]);
  assert.deepEqual(result, { attempted: 3, failed: 1 });
});

test("provider catalog-change toasts hold for one minute and split an underscored sync action", async () => {
  const {
    PROVIDER_HARNESS_SYNC_TOAST_MS,
    providerNoticeActionParts,
    providerToastDismissMs,
  } = await import("../src/lib/provider-notice-policy.ts");
  assert.equal(PROVIDER_HARNESS_SYNC_TOAST_MS, 60_000);
  assert.equal(providerToastDismissMs({ ok: true, offerHarnessSync: true }), 60_000);
  assert.equal(providerToastDismissMs({ ok: true, offerHarnessSync: false }), 4_500);
  assert.equal(providerToastDismissMs({ ok: false, offerHarnessSync: true }), 15_000);
  assert.deepEqual(
    providerNoticeActionParts('Enabled "command-code". Do you want to {action}?'),
    { pre: 'Enabled "command-code". Do you want to ', post: "?" },
  );
  assert.equal(providerNoticeActionParts("Enabled only."), null);
});

test("enable, disable, add, and remove toasts offer harness sync instead of claiming Codex already updated", () => {
  const en = readFileSync(join(root, "../src/i18n/en-base.mts"), "utf8");
  for (const key of ["prov.enabled", "prov.disabled", "prov.added", "prov.removed", "prov.removedDefault"]) {
    assert.match(en, new RegExp(`"${key}": ".*\\{action\\}`));
  }
  assert.match(en, /"prov.syncNow": "sync now"/);
  const catalogToasts = ["prov.enabled", "prov.disabled", "prov.added", "prov.removed", "prov.removedDefault"]
    .map((key) => en.match(new RegExp(`"${key}": "[^"]+"`))?.[0] ?? "")
    .join("\n");
  assert.doesNotMatch(catalogToasts, /Codex/);
  assert.doesNotMatch(catalogToasts, /hidden/);

  const registry = readFileSync(join(root, "../src/pages/provider-registry.ts"), "utf8");
  assert.match(registry, /offerHarnessSync:\s*true/);
  const providers = readFileSync(join(root, "../src/pages/Providers.tsx"), "utf8");
  assert.match(providers, /offerHarnessSync:\s*true/);
  assert.match(providers, /syncEnabledHarnessModelLists/);
  const apply = readFileSync(join(root, "../src/lib/provider-harness-sync-apply.ts"), "utf8");
  assert.match(apply, /applyEnabledHarnessModelLists/);
  assert.match(apply, /syncEnabledHarnessModelLists/);
});
