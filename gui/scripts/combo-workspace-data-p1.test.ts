/** Benes dashboard client for the Go proxy (`internal/server`). */
import assert from "node:assert/strict";
import test from "node:test";
import {
  buildComboAttention,
  emptyDraft,
  parseComboList,
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

test("buildComboAttention skips one-target combos (valid informational)", () => {
  const attention = buildComboAttention([
    item({ id: "solo", targets: [{ provider: "openai", model: "gpt-4o" }] }),
    item({
      id: "pair",
      targets: [
        { provider: "openai", model: "gpt-4o" },
        { provider: "anthropic", model: "claude" },
      ],
    }),
  ]);
  assert.equal(attention.some((row) => row.reason === "few-targets"), false);
  assert.equal(attention.length, 0);
});

test("buildComboAttention still flags empty targets", () => {
  const attention = buildComboAttention([item({ id: "empty", targets: [] })]);
  assert.deepEqual(attention, [
    { id: "empty", model: "combo/empty", reason: "empty-targets" },
  ]);
});

test("buildComboAttention flags catalog omission without inventing modality causes", () => {
  const attention = buildComboAttention(
    [
      item({
        id: "gap",
        targets: [{ provider: "openai", model: "gpt-4o" }],
      }),
      item({
        id: "ok",
        targets: [{ provider: "openai", model: "gpt-4o" }],
      }),
    ],
    { cataloguedComboIds: new Set(["ok"]) },
  );
  assert.deepEqual(attention, [
    { id: "gap", model: "combo/gap", reason: "catalog-omitted" },
  ]);
});

test("hidden defaultEffort/imageInput still round-trip when present", () => {
  const items = parseComboList({
    combos: [
      {
        id: "effortful",
        model: "combo/effortful",
        strategy: "failover",
        defaultEffort: "high",
        imageInput: "disabled",
        targets: [{ provider: "openai", model: "gpt-4o" }],
      },
    ],
  });
  assert.equal(items[0]!.defaultEffort, "high");
  assert.equal(items[0]!.imageInput, "disabled");
  const body = toPutBody(items[0]!);
  assert.equal(body.combo.defaultEffort, "high");
  assert.equal(body.combo.imageInput, "disabled");
});
