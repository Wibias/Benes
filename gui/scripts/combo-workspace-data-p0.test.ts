import assert from "node:assert/strict";
import test from "node:test";

import {
  draftEquals,
  emptyDraft,
  groupCombos,
  normalizeStickyLimit,
  normalizeStrategy,
  parseComboList,
  toPutBody,
  wireComboStrategy,
  type ComboItem,
} from "../src/combo-workspace-data.ts";
import { combosPageNotices } from "../src/components/combo-workspace-utils.ts";

function baseItem(partial: Partial<ComboItem> = {}): ComboItem {
  return {
    id: "x",
    model: "combo/x",
    alias: null,
    nativeAlias: false,
    displayName: null,
    strategy: "failover",
    defaultEffort: null,
    targets: [{ provider: "openai-apikey", model: "gpt-4o", clientKey: "a" }],
    ...partial,
  };
}

test("normalizeStrategy distinguishes failover, round-robin, and unsupported", () => {
  assert.equal(normalizeStrategy("failover"), "failover");
  assert.equal(normalizeStrategy(""), "failover");
  assert.equal(normalizeStrategy(undefined), "failover");
  assert.equal(normalizeStrategy("round-robin"), "round-robin");
  assert.equal(normalizeStrategy("weighted"), "unsupported");
});

test("normalizeStickyLimit is presence-sensitive and does not invent 1", () => {
  assert.equal(normalizeStickyLimit(undefined), undefined);
  assert.equal(normalizeStickyLimit(null), undefined);
  assert.equal(normalizeStickyLimit(0), undefined);
  assert.equal(normalizeStickyLimit(4), 4);
});

test("parseComboList preserves legacy RR strategy, weights, stickyLimit", () => {
  const items = parseComboList({
    combos: [{
      id: "legacy_rr",
      model: "combo/legacy_rr",
      strategy: "round-robin",
      stickyLimit: 4,
      targets: [
        { provider: "openai-apikey", model: "gpt-4o", weight: 3 },
        { provider: "anthropic", model: "claude-opus-4-6", weight: 1 },
      ],
    }],
  });
  assert.equal(items.length, 1);
  assert.equal(items[0]!.strategy, "round-robin");
  assert.equal(items[0]!.stickyLimit, 4);
  assert.equal(items[0]!.targets[0]!.weight, 3);
  assert.equal(items[0]!.targets[1]!.weight, 1);
});

test("parseComboList keeps sticky absent and stores unsupported wire strategy", () => {
  const items = parseComboList({
    combos: [
      {
        id: "plain",
        strategy: "failover",
        targets: [{ provider: "openai-apikey", model: "gpt-4o" }],
      },
      {
        id: "weird",
        strategy: "weighted",
        targets: [{ provider: "openai-apikey", model: "gpt-4o" }],
      },
    ],
  });
  const plain = items.find((item) => item.id === "plain")!;
  const weird = items.find((item) => item.id === "weird")!;
  assert.equal(plain.stickyLimit, undefined);
  assert.equal(weird.strategy, "unsupported");
  assert.equal(weird.storedStrategy, "weighted");
});

test("toPutBody preserves RR/sticky/weights and does not invent stickyLimit", () => {
  const rr = baseItem({
    id: "legacy_rr",
    strategy: "round-robin",
    stickyLimit: 4,
    targets: [
      { provider: "openai-apikey", model: "gpt-4o", weight: 3, clientKey: "a" },
      { provider: "anthropic", model: "claude-opus-4-6", weight: 1, clientKey: "b" },
    ],
  });
  const body = toPutBody(rr);
  assert.equal(body.combo.strategy, "round-robin");
  assert.equal(body.combo.stickyLimit, 4);
  assert.deepEqual(body.combo.targets, [
    { provider: "openai-apikey", model: "gpt-4o", weight: 3 },
    { provider: "anthropic", model: "claude-opus-4-6", weight: 1 },
  ]);

  const plain = toPutBody(emptyDraft("new"));
  assert.equal(plain.combo.strategy, "failover");
  assert.equal("stickyLimit" in plain.combo, false);
});

test("toPutBody weight matrix: unchanged, reorder, add, remove, edit identity keeps weight", () => {
  const weighted = baseItem({
    strategy: "round-robin",
    stickyLimit: 2,
    targets: [
      { provider: "openai-apikey", model: "gpt-4o", weight: 3, clientKey: "a" },
      { provider: "anthropic", model: "claude-opus-4-6", weight: 1, clientKey: "b" },
    ],
  });

  // (1) unchanged
  assert.deepEqual(toPutBody(weighted).combo.targets.map((t) => t.weight), [3, 1]);

  // (2) reorder
  const reordered = {
    ...weighted,
    targets: [weighted.targets[1]!, weighted.targets[0]!],
  };
  assert.deepEqual(toPutBody(reordered).combo.targets.map((t) => [t.provider, t.weight]), [
    ["anthropic", 1],
    ["openai-apikey", 3],
  ]);

  // (3) add target (no invented weight)
  const added = {
    ...weighted,
    targets: [...weighted.targets, { provider: "google", model: "gemini-2.5-flash", clientKey: "c" }],
  };
  assert.deepEqual(toPutBody(added).combo.targets.map((t) => t.weight), [3, 1, undefined]);

  // (4) remove unrelated target
  const removed = { ...weighted, targets: [weighted.targets[0]!] };
  assert.deepEqual(toPutBody(removed).combo.targets, [
    { provider: "openai-apikey", model: "gpt-4o", weight: 3 },
  ]);

  // (5) edit provider/model of weighted target — weight stays on the same object
  const editedTarget = { ...weighted.targets[0]!, model: "gpt-5.4" };
  const edited = { ...weighted, targets: [editedTarget, weighted.targets[1]!] };
  assert.equal(toPutBody(edited).combo.targets[0]!.weight, 3);
  assert.equal(toPutBody(edited).combo.targets[0]!.model, "gpt-5.4");
});

test("unsupported strategy wires through storedStrategy and stays non-failover in PUT", () => {
  const item = baseItem({
    strategy: "unsupported",
    storedStrategy: "weighted",
  });
  assert.equal(wireComboStrategy(item), "weighted");
  assert.equal(toPutBody(item).combo.strategy, "weighted");
});

test("groupCombos failover count excludes legacy RR and unsupported", () => {
  const sections = groupCombos([
    baseItem({ id: "a", strategy: "failover" }),
    baseItem({ id: "b", strategy: "round-robin" }),
    baseItem({ id: "c", strategy: "unsupported", storedStrategy: "weighted" }),
  ]);
  assert.deepEqual(sections.failover.map((item) => item.id), ["a"]);
  assert.deepEqual(sections.roundRobin.map((item) => item.id), ["b"]);
  assert.deepEqual(sections.unsupported.map((item) => item.id), ["c"]);
});

test("draftEquals treats absent stickyLimit as distinct from 1", () => {
  const absent = baseItem();
  const explicit = baseItem({ stickyLimit: 1 });
  assert.equal(draftEquals(absent, explicit), false);
  assert.equal(draftEquals(absent, baseItem()), true);
});

test("combosPageNotices: cold alert vs stale warn vs cleared", () => {
  assert.deepEqual(
    combosPageNotices({
      surfaceKind: "failed-cold",
      surfaceError: new Error("boom"),
      loadFailedLabel: "Could not load combos.",
      refreshFailedStaleLabel: "Could not refresh combos. Showing the last available data.",
    }),
    { loadError: "boom", staleRefreshWarning: null },
  );
  assert.deepEqual(
    combosPageNotices({
      surfaceKind: "failed-with-stale",
      surfaceError: new Error("boom"),
      loadFailedLabel: "Could not load combos.",
      refreshFailedStaleLabel: "Could not refresh combos. Showing the last available data.",
    }),
    {
      loadError: null,
      staleRefreshWarning: "Could not refresh combos. Showing the last available data.",
    },
  );
  assert.deepEqual(
    combosPageNotices({
      surfaceKind: "ready",
      surfaceError: null,
      loadFailedLabel: "Could not load combos.",
      refreshFailedStaleLabel: "Could not refresh combos. Showing the last available data.",
    }),
    { loadError: null, staleRefreshWarning: null },
  );
});
