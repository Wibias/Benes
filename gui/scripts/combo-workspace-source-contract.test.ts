/**
 * Combos workspace contract.
 *
 * These tests pin behaviour the Combos modules carry, plus the module
 * ownership the current product relies on: picker/catalog decode and page
 * notices live with the pure Combos helpers, and the combos wire decoder lives
 * with the combos domain module.
 */
import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import test from "node:test";
import { fileURLToPath } from "node:url";

import {
  comboMutationSucceeded,
  emptyDraft,
  validateComboDraft,
  type ComboItem,
} from "../src/combo-workspace-data.ts";
import {
  comboEditorSyncKey,
  comboEditorTitle,
  comboSavePayload,
  comboTargetCountCopy,
  comboRailNote,
  combosPageNotices,
  legacyTargetNote,
  parseComboWorkspaceModels,
  parseComboWorkspaceProviders,
} from "../src/components/combo-workspace-utils.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");

const PROVIDERS = {
  openai: { adapter: "openai-responses", baseUrl: "https://example.invalid" },
  disabled: { adapter: "openai-responses", baseUrl: "https://example.invalid", disabled: true },
};

function draft(partial: Partial<ComboItem> = {}): ComboItem {
  return {
    ...emptyDraft("alpha"),
    targets: [{ provider: "openai", model: "gpt-4o" }],
    ...partial,
  };
}

function errorFor(item: ComboItem, overrides: Partial<Parameters<typeof validateComboDraft>[1]> = {}) {
  return validateComboDraft(item, {
    existingIds: [],
    isCreate: true,
    providers: PROVIDERS,
    ...overrides,
  });
}

/* ---------------------------------------------------------------- rail copy */

test("rail note priority: broken members beat catalogue gap beat stored legacy", () => {
  const plain = draft();
  const roundRobin = draft({ strategy: "round-robin" });
  const unsupported = draft({ strategy: "unsupported", storedStrategy: "weighted" });
  const legacy = draft({ defaultEffort: "high" });

  // 1. empty targets outranks every other reason the row could carry.
  assert.equal(comboRailNote(draft({ targets: [] }), "empty-targets"), "cws.rail.noTargets");
  assert.equal(comboRailNote(roundRobin, "empty-targets"), "cws.rail.noTargets");
  assert.equal(comboRailNote(unsupported, "empty-targets"), "cws.rail.noTargets");

  // 2. catalogue omission outranks stored legacy state.
  assert.equal(comboRailNote(roundRobin, "catalog-omitted"), "cws.rail.notInCatalog");
  assert.equal(comboRailNote(unsupported, "catalog-omitted"), "cws.rail.notInCatalog");
  assert.equal(comboRailNote(legacy, "catalog-omitted"), "cws.rail.notInCatalog");

  // 3-5. strategy, then stored legacy config.
  assert.equal(comboRailNote(roundRobin, undefined), "cws.rail.legacyStrategy");
  assert.equal(comboRailNote(unsupported, undefined), "cws.rail.unsupportedStrategy");
  assert.equal(comboRailNote(legacy, undefined), "cws.rail.legacyConfig");

  // A plain failover combo with no stored extras needs no flag at all.
  assert.equal(comboRailNote(plain, undefined), null);
  assert.equal(comboRailNote(plain, "few-targets"), null);
});

test("rail target count reads singular for one member and counted otherwise", () => {
  assert.deepEqual(comboTargetCountCopy(1), { key: "cws.targetCountOne" });
  assert.deepEqual(comboTargetCountCopy(0), { key: "cws.targetCount", count: 0 });
  assert.deepEqual(comboTargetCountCopy(3), { key: "cws.targetCount", count: 3 });
});

test("editor legacy note names the strategy the failover editor cannot rewrite", () => {
  const noteOf = (item: ComboItem) => legacyTargetNote(item, (key) => `t:${key}`);
  assert.equal(noteOf(draft()), null);
  assert.equal(noteOf(draft({ strategy: "round-robin" })), "t:cws.legacy.editRoundRobin");
  assert.equal(
    noteOf(draft({ strategy: "unsupported", storedStrategy: "weighted" })),
    "t:cws.strategy.unsupportedHint",
  );
});

test("editor save payload trims identity and forces failover on create", () => {
  const stored = draft({
    id: "  beta  ",
    alias: "  Nick  ",
    displayName: "  Label  ",
    strategy: "round-robin",
    nativeAlias: true,
  });
  // A create always stores failover, whatever the draft carried.
  const created = comboSavePayload(stored, true);
  assert.equal(created.id, "beta");
  assert.equal(created.alias, "Nick");
  assert.equal(created.displayName, "Label");
  assert.equal(created.strategy, "failover");
  assert.equal(created.model, "Nick");
  // An edit preserves the stored strategy and the combo/<id> default.
  const edited = comboSavePayload({ ...stored, alias: null, nativeAlias: false }, false);
  assert.equal(edited.strategy, "round-robin");
  assert.equal(edited.alias, null);
  assert.equal(edited.model, "combo/beta");
});

test("editor sync key reacts to every stored field a refresh could change", () => {
  const base = draft();
  const key = comboEditorSyncKey(base);
  assert.equal(comboEditorSyncKey({ ...base }), key);
  const changes = [
    { id: "other" },
    { alias: "Nick" },
    { nativeAlias: true },
    { displayName: "Label" },
    { strategy: "round-robin" as const },
    { stickyLimit: 4 },
    { defaultEffort: "high" as const },
    { imageInput: "disabled" as const },
    { targets: [{ provider: "openai", model: "gpt-4o", weight: 3 }] },
  ];
  for (const change of changes) {
    assert.notEqual(comboEditorSyncKey({ ...base, ...change }), key, JSON.stringify(change));
  }
});

test("editor heading shows the stored title on edit and the pending id on create", () => {
  const named = draft({ id: "beta", alias: "Nickname" });
  assert.equal(comboEditorTitle(false, named, named, "New combo"), "Nickname");
  assert.equal(comboEditorTitle(true, draft({ id: "" }), named, "New combo"), "New combo");
  assert.equal(comboEditorTitle(true, named, named, "New combo"), "Nickname");
});

/* ------------------------------------------------------- module ownership */

test("combos load helpers live with the current Benes owners", () => {
  // The historical page module is gone; its behaviour is owned elsewhere.
  assert.equal(fs.existsSync(path.join(guiRoot, "src", "combo-workspace-page.ts")), false);
  // The config form was a single-consumer split of the editor pane.
  assert.equal(
    fs.existsSync(path.join(guiRoot, "src", "components", "combo-detail-config-form.tsx")),
    false,
  );
  // No source may still import either historical module.
  const offenders = [];
  const walk = (dir) => {
    for (const entry of fs.readdirSync(dir, { withFileTypes: true })) {
      const full = path.join(dir, entry.name);
      if (entry.isDirectory()) {
        walk(full);
        continue;
      }
      if (!/\.(ts|tsx)$/.test(entry.name)) continue;
      // This test names the forbidden modules on purpose.
      if (entry.name === "combo-workspace-source-contract.test.ts") continue;
      const source = fs.readFileSync(full, "utf8");
      if (source.includes("combo-workspace-page") || source.includes("combo-detail-config-form")) {
        offenders.push(path.relative(guiRoot, full));
      }
    }
  };
  walk(path.join(guiRoot, "src"));
  walk(path.join(guiRoot, "scripts"));
  assert.deepEqual(offenders, []);
});

/* --------------------------------------------------- write result decode */

test("comboMutationSucceeded treats anything but an explicit success as failure", () => {
  assert.deepEqual(comboMutationSucceeded({ success: true }), { ok: true });
  assert.deepEqual(comboMutationSucceeded({ success: true, error: "boom" }), { ok: false, error: "boom" });
  assert.deepEqual(comboMutationSucceeded({ success: false, error: "boom" }), { ok: false, error: "boom" });
  assert.deepEqual(comboMutationSucceeded({ error: "   " }), { ok: false });
  assert.deepEqual(comboMutationSucceeded({ success: false }), { ok: false });
  assert.deepEqual(comboMutationSucceeded(null), { ok: false });
  assert.deepEqual(comboMutationSucceeded([{ success: true }]), { ok: false });
  assert.deepEqual(comboMutationSucceeded("ok"), { ok: false });
});

/* ---------------------------------------------------------- page notices */

test("combosPageNotices maps surface kind to route copy", () => {
  const labels = {
    loadFailedLabel: "load failed",
    refreshFailedStaleLabel: "refresh failed",
  };
  assert.deepEqual(
    combosPageNotices({ surfaceKind: "failed-cold", surfaceError: new Error("boom"), ...labels }),
    { loadError: "boom", staleRefreshWarning: null },
  );
  // A failure that carries no message falls back to the route's own copy.
  assert.deepEqual(
    combosPageNotices({ surfaceKind: "failed-cold", surfaceError: new Error(""), ...labels }),
    { loadError: "load failed", staleRefreshWarning: null },
  );
  assert.deepEqual(
    combosPageNotices({ surfaceKind: "failed-cold", surfaceError: "not an error", ...labels }),
    { loadError: "load failed", staleRefreshWarning: null },
  );
  assert.deepEqual(
    combosPageNotices({ surfaceKind: "failed-with-stale", surfaceError: new Error("boom"), ...labels }),
    { loadError: null, staleRefreshWarning: "refresh failed" },
  );
  // Any other kind clears both notices, including a refresh after a stale failure.
  assert.deepEqual(
    combosPageNotices({ surfaceKind: "refreshing", surfaceError: null, ...labels }),
    { loadError: null, staleRefreshWarning: null },
  );
});

/* --------------------------------------------------------- catalog decode */

test("combos catalog decode accepts both payload shapes and skips unusable rows", () => {
  const rows = [
    { provider: "combo", id: "free" },
    { provider: "openai", id: "gpt-4o", reasoningEfforts: ["low", 7], inputModalities: [" image ", ""] },
    { provider: "openai", id: "retired", disabled: true },
    { provider: "openai", id: "   " },
    { provider: "", id: "orphan" },
    "not-a-row",
  ];
  const config = { openai: { adapter: "a", baseUrl: "b", defaultModel: "gpt-4o-mini" } };

  const parsed = parseComboWorkspaceModels(rows, config);
  assert.deepEqual(parsed.cataloguedComboIds, ["free"]);
  assert.deepEqual(parsed.models.map((row) => row.id), ["gpt-4o", "gpt-4o-mini"]);
  assert.deepEqual(parsed.models[0].reasoningEfforts, ["low"]);
  assert.deepEqual(parsed.models[0].inputModalities, ["image"]);

  // `/api/models` may also answer in the wrapped shape, and an unreadable
  // payload contributes nothing rather than throwing.
  assert.deepEqual(parseComboWorkspaceModels({ models: rows }, config).cataloguedComboIds, ["free"]);
  assert.deepEqual(parseComboWorkspaceModels(null, {}).models, []);
  assert.deepEqual(parseComboWorkspaceModels({ models: "nope" }, {}).models, []);
  // Configured defaults are independent of the catalog payload.
  assert.deepEqual(parseComboWorkspaceModels(null, config).models.map((row) => row.id), ["gpt-4o-mini"]);
});

test("combos catalog decode keeps a disabled provider's default out of the picker", () => {
  const parsed = parseComboWorkspaceModels([], {
    openai: { adapter: "a", baseUrl: "b", defaultModel: "gpt-4o-mini" },
    disabled: { adapter: "a", baseUrl: "b", disabled: true, defaultModel: "hidden" },
    blank: { adapter: "a", baseUrl: "b", defaultModel: "   " },
  });
  assert.deepEqual(parsed.models.map((row) => row.id), ["gpt-4o-mini"]);
});

test("combos catalog decode does not duplicate a default the catalog already lists", () => {
  const parsed = parseComboWorkspaceModels(
    [{ provider: "openai", id: "gpt-4o-mini" }],
    { openai: { adapter: "a", baseUrl: "b", defaultModel: "gpt-4o-mini" } },
  );
  assert.deepEqual(parsed.models, [{ provider: "openai", id: "gpt-4o-mini" }]);
});

test("combos provider rows carry routing facts and foregrounding", () => {
  const facts = {
    openai: { adapter: "openai-responses", baseUrl: "https://api.openai.com/v1", authMode: "forward" },
    hidden: { adapter: "a", baseUrl: "b", disabled: true },
  };
  const rows = parseComboWorkspaceProviders(facts, { openai: facts.openai });
  assert.deepEqual(rows, [
    {
      name: "openai",
      disabled: false,
      hiddenFromPicker: false,
      authMode: "forward",
      adapter: "openai-responses",
      baseUrl: "https://api.openai.com/v1",
    },
    {
      name: "hidden",
      disabled: true,
      hiddenFromPicker: true,
      authMode: undefined,
      adapter: "a",
      baseUrl: "b",
    },
  ]);
});

test("validateComboDraft reports the first broken rule in order", () => {
  // identity before alias
  assert.equal(errorFor(draft({ id: "", alias: "bad alias" })), "missingId");
  assert.equal(errorFor(draft({ id: "bad id", alias: "bad alias" })), "invalidId");
  assert.equal(errorFor(draft({ id: "alpha" }), { existingIds: ["alpha"] }), "duplicateId");
  assert.equal(
    errorFor(draft({ id: "alpha" }), { providers: { combo: { adapter: "x", baseUrl: "y" } } }),
    "reservedNamespace",
  );
  assert.equal(errorFor(draft({ id: "openai" })), "providerCollision");

  // alias before display name
  assert.equal(errorFor(draft({ alias: "has space" })), "invalidAlias");
  assert.equal(errorFor(draft({ alias: "combo/alpha" })), "aliasReservedNamespace");
  assert.equal(errorFor(draft({ alias: "gpt-5.4" })), "aliasNativeFamily");
  assert.equal(errorFor(draft({ alias: "taken" }), { existingAliases: ["taken"] }), "duplicateAlias");

  // display name before targets
  assert.equal(errorFor(draft({ displayName: "x".repeat(129) })), "invalidDisplayName");
  assert.equal(
    errorFor(draft({ nativeAlias: true, alias: "gpt-5.4", displayName: null })),
    "missingNativeAliasDisplayName",
  );
  assert.equal(errorFor(draft({ nativeAlias: true, alias: "not-native" })), "unsupportedNativeAlias");

  // targets: presence, completeness, membership, uniqueness, usability
  assert.equal(errorFor(draft({ targets: [] })), "noTargets");
  assert.equal(errorFor(draft({ targets: [{ provider: "openai", model: "" }] })), "incompleteTarget");
  assert.equal(errorFor(draft({ targets: [{ provider: "ghost", model: "gpt-4o" }] })), "unknownProvider");
  assert.equal(
    errorFor(draft({ targets: [
      { provider: "openai", model: "gpt-4o" },
      { provider: "openai", model: "gpt-4o" },
    ] })),
    "duplicateTarget",
  );
  assert.equal(
    errorFor(draft({ targets: [{ provider: "disabled", model: "gpt-4o" }] })),
    "noEnabledTarget",
  );
  assert.equal(errorFor(draft()), null);
});

test("legacy weight and sticky ranges apply only to round-robin drafts", () => {
  const weighted = { provider: "openai", model: "gpt-4o", weight: 0 };

  // A failover draft carries no legacy ranges, so out-of-range values are inert.
  assert.equal(errorFor(draft({ targets: [weighted] })), null);
  assert.equal(errorFor(draft({ stickyLimit: 0 })), null);

  // The same values on a stored round-robin draft are rejected.
  assert.equal(errorFor(draft({ strategy: "round-robin", targets: [weighted] })), "invalidWeight");
  assert.equal(errorFor(draft({ strategy: "round-robin", stickyLimit: 0 })), "invalidStickyLimit");
  assert.equal(
    errorFor(draft({
      strategy: "round-robin",
      targets: [{ provider: "openai", model: "gpt-4o", weight: 3 }],
    })),
    null,
  );
});