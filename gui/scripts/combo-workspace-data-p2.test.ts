/** Combos P2 IA helpers. */
import assert from "node:assert/strict";
import test from "node:test";
import {
  comboDisplaySecondaryIdentity,
  comboDisplayTitle,
  comboHasLegacyConfig,
  emptyDraft,
  filterCombos,
  draftEquals,
  formatLegacySparseWeights,
  shouldAdoptComboBaselineOnSync,
  toPutBody,
  type ComboItem,
} from "../src/combo-workspace-data.ts";

function item(partial: Partial<ComboItem> & Pick<ComboItem, "id">): ComboItem {
  return {
    ...emptyDraft(),
    model: `combo/${partial.id}`,
    ...partial,
  };
}

test("filterCombos matches identity fields only, not target internals", () => {
  const items = [
    item({
      id: "production",
      alias: "Prod Chain",
      model: "Prod Chain",
      targets: [{ provider: "openai", model: "gpt-secret-target" }],
    }),
    item({
      id: "native",
      alias: "gpt-4o",
      nativeAlias: true,
      model: "gpt-4o",
      targets: [{ provider: "anthropic", model: "claude-hidden" }],
    }),
  ];
  assert.equal(filterCombos(items, "production").length, 1);
  assert.equal(filterCombos(items, "Prod").length, 1);
  assert.equal(filterCombos(items, "gpt-4o").length, 1);
  assert.equal(filterCombos(items, "combo/native").length, 0);
  assert.equal(filterCombos(items, "gpt-secret-target").length, 0);
  assert.equal(filterCombos(items, "claude-hidden").length, 0);
  assert.equal(filterCombos(items, "openai").length, 0);
});

test("comboDisplayTitle prefers nickname; secondary identity only when different", () => {
  const nick = item({ id: "a", alias: "Friendly", model: "Friendly" });
  assert.equal(comboDisplayTitle(nick), "Friendly");
  assert.equal(comboDisplaySecondaryIdentity(nick), "combo/a");
  const plain = item({ id: "b", model: "combo/b" });
  assert.equal(comboDisplayTitle(plain), "combo/b");
  assert.equal(comboDisplaySecondaryIdentity(plain), null);
  const native = item({ id: "c", alias: "gpt-4o", nativeAlias: true, model: "gpt-4o" });
  assert.equal(comboDisplayTitle(native), "gpt-4o");
  assert.equal(comboDisplaySecondaryIdentity(native), null);
});

test("comboHasLegacyConfig detects RR/sticky/weights/effort/image", () => {
  assert.equal(comboHasLegacyConfig(item({ id: "x" })), false);
  assert.equal(comboHasLegacyConfig(item({ id: "rr", strategy: "round-robin" })), true);
  assert.equal(comboHasLegacyConfig(item({ id: "st", stickyLimit: 4 })), true);
  assert.equal(
    comboHasLegacyConfig(item({
      id: "w",
      targets: [{ provider: "openai", model: "gpt-4o", weight: 3 }],
    })),
    true,
  );
  assert.equal(comboHasLegacyConfig(item({ id: "e", defaultEffort: "high" })), true);
  assert.equal(comboHasLegacyConfig(item({ id: "i", imageInput: "disabled" })), true);
});

test("edit-shaped toPutBody still preserves round-robin spelling (P0)", () => {
  const rr = item({
    id: "legacy_rr",
    strategy: "round-robin",
    stickyLimit: 4,
    targets: [
      { provider: "openai", model: "a", weight: 3 },
      { provider: "anthropic", model: "b", weight: 1 },
    ],
  });
  // Simulate DetailPanel save payload without forcing failover.
  const body = toPutBody({ ...rr, strategy: "round-robin" });
  assert.equal(body.combo.strategy, "round-robin");
  assert.equal(body.combo.stickyLimit, 4);
  assert.deepEqual(body.combo.targets.map((t) => t.weight), [3, 1]);
});

test("formatLegacySparseWeights keeps absence and associates positions", () => {
  assert.equal(formatLegacySparseWeights([]), null);
  assert.equal(
    formatLegacySparseWeights([
      { weight: undefined },
      { weight: undefined },
    ]),
    null,
  );
  assert.equal(
    formatLegacySparseWeights([
      {},
      { weight: 3 },
      {},
    ]),
    "#2: 3",
  );
  assert.equal(
    formatLegacySparseWeights([
      { weight: 2 },
      {},
      { weight: 1 },
    ]),
    "#1: 2, #3: 1",
  );
  // Never invent weight 1 for absent slots when another target stores a weight.
  assert.equal(
    formatLegacySparseWeights([
      {},
      { weight: 5 },
    ]),
    "#2: 5",
  );
  assert.doesNotMatch(formatLegacySparseWeights([{}, { weight: 5 }]) ?? "", /(?:^|, )1(?:,|$)/);
});

test("shouldAdoptComboBaselineOnSync mirrors Routing Profiles dirty contract", () => {
  assert.equal(shouldAdoptComboBaselineOnSync(false), true);
  assert.equal(shouldAdoptComboBaselineOnSync(true), false);
});

test("dirty refresh contract keeps preserve-only legacy draft fields", () => {
  const baseline = item({
    id: "legacy_rr",
    strategy: "round-robin",
    stickyLimit: 4,
    defaultEffort: "high",
    imageInput: "disabled",
    targets: [
      { provider: "openai", model: "a", weight: 3 },
      { provider: "anthropic", model: "b" },
    ],
  });
  let draft: ComboItem = { ...baseline, displayName: "edited-nickname" };
  let dirty = !draftEquals(draft, baseline);
  assert.equal(dirty, true);

  // List refresh that would otherwise wipe preserve-only fields / in-progress edits.
  const refreshed = item({
    id: "legacy_rr",
    strategy: "failover",
    targets: [
      { provider: "openai", model: "a" },
      { provider: "anthropic", model: "b" },
    ],
  });

  if (shouldAdoptComboBaselineOnSync(dirty)) {
    draft = refreshed;
    dirty = false;
  }
  assert.equal(draft.displayName, "edited-nickname");
  assert.equal(draft.strategy, "round-robin");
  assert.equal(draft.stickyLimit, 4);
  assert.equal(draft.defaultEffort, "high");
  assert.equal(draft.imageInput, "disabled");
  assert.deepEqual(draft.targets.map((t) => t.weight), [3, undefined]);
  assert.equal(dirty, true);

  // Clean editor may adopt the refreshed baseline.
  dirty = false;
  if (shouldAdoptComboBaselineOnSync(dirty)) {
    draft = refreshed;
  }
  assert.equal(draft.strategy, "failover");
  assert.equal(draft.displayName, null);
});
