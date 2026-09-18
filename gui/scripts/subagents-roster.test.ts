import { readFileSync } from "node:fs";
import assert from "node:assert/strict";
import test from "node:test";
import {
  FEATURED_MAX,
  ROLE_CHOICES,
  applyRole,
  availableIds,
  clampGhostY,
  insertIndexFromY,
  moveToIndex,
  reorderShiftY,
  providerOf,
  reconcileOrder,
  roleChoiceOf,
  roleFromChoice,
  roleOf,
  rosterIds,
  scrollEdgeDelta,
  shiftSlotsY,
  splitRosterOrder,
} from "../src/components/subagents-workspace/roster.ts";

test("rosterIds lists featured first, then leftover fallbacks", () => {
  assert.deepEqual(
    rosterIds(["openai/sol", "openai/luna"], ["grok/4.6", "openai/sol"]),
    ["openai/sol", "openai/luna", "grok/4.6"],
  );
});

test("availableIds drops anything already on the roster", () => {
  assert.deepEqual(
    availableIds(["a", "b", "c"], ["b"]),
    ["a", "c"],
  );
});

test("roleOf is exclusive: first call wins over featured", () => {
  const chosen = new Set(["openai/sol", "openai/luna"]);
  const fallbacks = new Set(["grok/4.6"]);
  assert.equal(roleOf("openai/sol", chosen, "openai/sol", fallbacks), "firstCall");
  assert.equal(roleOf("openai/luna", chosen, "openai/sol", fallbacks), "featured");
  assert.equal(roleOf("grok/4.6", chosen, "openai/sol", fallbacks), "fallback");
  assert.equal(roleOf("google/pro", chosen, "openai/sol", fallbacks), "unassigned");
});

test("applyRole featured adds to the spawn roster and clears fallback", () => {
  const next = applyRole("grok/4.6", "featured", ["openai/sol"], ["grok/4.6"], "openai/sol");
  assert.deepEqual(next.chosen, ["openai/sol", "grok/4.6"]);
  assert.deepEqual(next.fallbacks, []);
  assert.equal(next.firstCall, "openai/sol");
});

test("applyRole firstCall keeps roster position and sets injection", () => {
  const next = applyRole("openai/luna", "firstCall", ["openai/sol", "openai/luna"], [], "openai/sol");
  assert.deepEqual(next.chosen, ["openai/sol", "openai/luna"]);
  assert.equal(next.firstCall, "openai/luna");
});

test("applyRole fallback removes from featured and demotes first call", () => {
  const next = applyRole("openai/sol", "fallback", ["openai/sol", "openai/luna"], [], "openai/sol");
  assert.deepEqual(next.chosen, ["openai/luna"]);
  assert.deepEqual(next.fallbacks, ["openai/sol"]);
  assert.equal(next.firstCall, "");
});

test("applyRole unassigned drops both lists", () => {
  const next = applyRole("openai/sol", "unassigned", ["openai/sol"], ["openai/sol"], "openai/sol");
  assert.deepEqual(next.chosen, []);
  assert.deepEqual(next.fallbacks, []);
  assert.equal(next.firstCall, "");
});

test("applyRole featured is a no-op when the spawn roster is full", () => {
  const chosen = Array.from({ length: FEATURED_MAX }, (_, i) => `m/${i}`);
  const next = applyRole("extra/1", "featured", chosen, [], "");
  assert.deepEqual(next.chosen, chosen);
  assert.deepEqual(next.fallbacks, []);
});

test("moveToIndex and splitRosterOrder keep roles while reordering", () => {
  const order = ["a", "b", "c", "d"];
  const moved = moveToIndex(order, "d", 0);
  assert.deepEqual(moved, ["d", "a", "b", "c"]);
  const split = splitRosterOrder(moved, new Set(["a", "b", "c"]), new Set(["d"]));
  assert.deepEqual(split.chosen, ["a", "b", "c"]);
  assert.deepEqual(split.fallbacks, ["d"]);
});

test("reconcileOrder keeps a mixed featured/fallback display order", () => {
  assert.deepEqual(
    reconcileOrder(["fallback/a", "featured/b"], ["featured/b"], ["fallback/a"]),
    ["fallback/a", "featured/b"],
  );
});

test("reconcileOrder drops removed ids and appends new members", () => {
  assert.deepEqual(
    reconcileOrder(["gone", "keep"], ["keep", "added"], []),
    ["keep", "added"],
  );
});

test("reorderShiftY slides crossed rows by one row height", () => {
  assert.equal(reorderShiftY(0, 0, 1, 56), 0);
  assert.equal(reorderShiftY(0, 2, 1, 56), -56);
  assert.equal(reorderShiftY(0, 2, 2, 56), -56);
  assert.equal(reorderShiftY(0, 2, 0, 56), 0);
  assert.equal(reorderShiftY(2, 0, 1, 56), 56);
  assert.equal(reorderShiftY(2, 0, 0, 56), 56);
  assert.equal(reorderShiftY(2, 0, 2, 56), 0);
});

test("insertIndexFromY stays put until the pointer enters another row", () => {
  const rows = [
    { top: 0, bottom: 40 },
    { top: 40, bottom: 80 },
  ];
  assert.equal(insertIndexFromY(20, rows, 0), 0);
  assert.equal(insertIndexFromY(40, rows, 0), 1);
  assert.equal(insertIndexFromY(70, rows, 1), 1);
  assert.equal(insertIndexFromY(39, rows, 1), 0);
});

test("clampGhostY stops at the section top divider and bottom", () => {
  assert.equal(clampGhostY(-40, 100, 400, 48), 100);
  assert.equal(clampGhostY(200, 100, 400, 48), 200);
  assert.equal(clampGhostY(390, 100, 400, 48), 352);
});

test("scrollEdgeDelta scrolls while the pointer sits in the top or bottom band", () => {
  assert.equal(scrollEdgeDelta(200, 100, 400), 0);
  assert.ok(scrollEdgeDelta(100, 100, 400) < 0);
  assert.ok(scrollEdgeDelta(136, 100, 400) < 0);
  assert.ok(scrollEdgeDelta(364, 100, 400) > 0);
  assert.ok(scrollEdgeDelta(400, 100, 400) > 0);
  assert.ok(Math.abs(scrollEdgeDelta(100, 100, 400)) >= Math.abs(scrollEdgeDelta(130, 100, 400)));
  assert.equal(scrollEdgeDelta(150, 100, 160), 0);
});

test("shiftSlotsY keeps row hit targets aligned after the list scrolls", () => {
  const slots = [
    { top: 100, bottom: 140 },
    { top: 140, bottom: 180 },
  ];
  assert.deepEqual(shiftSlotsY(slots, 20), [
    { top: 80, bottom: 120 },
    { top: 120, bottom: 160 },
  ]);
  assert.equal(shiftSlotsY(slots, 0), slots);
});

test("applyRole firstCall is exclusive: the previous primary stays assigned", () => {
  const next = applyRole("openai/luna", "firstCall", ["openai/sol", "openai/luna"], [], "openai/sol");
  assert.deepEqual(next.chosen, ["openai/sol", "openai/luna"]);
  assert.equal(next.firstCall, "openai/luna");
  const chosenSet = new Set(next.chosen);
  const fallbackSet = new Set(next.fallbacks);
  assert.equal(roleOf("openai/luna", chosenSet, next.firstCall, fallbackSet), "firstCall");
  assert.equal(roleOf("openai/sol", chosenSet, next.firstCall, fallbackSet), "featured");
});

test("assigned roster remove is applyRole unassigned; Model order is not a second remove API", () => {
  const next = applyRole("openai/sol", "unassigned", ["openai/sol", "openai/luna"], [], "openai/sol");
  assert.deepEqual(next.chosen, ["openai/luna"]);
  assert.equal(next.firstCall, "");
  const detail = readFileSync(new URL("../src/components/subagents-workspace/SubagentsDetail.tsx", import.meta.url), "utf8");
  const orderStart = detail.indexOf("function OrderRow");
  const orderBody = detail.slice(orderStart, detail.indexOf("\nfunction ", orderStart + 1));
  assert.equal(orderBody.includes("SubagentsRemove"), false);
  const rail = readFileSync(new URL("../src/components/subagents-workspace/SubagentsRail.tsx", import.meta.url), "utf8");
  assert.equal(rail.includes("SubagentsRemove"), true);
});

test("providerOf reads the namespace", () => {
  assert.equal(providerOf("openai/gpt-5.6-sol"), "openai");
  assert.equal(providerOf("grok-4.6"), "grok-4.6");
});

test("role radios are Assigned / Fallback / Unassigned; Primary is not a role", () => {
  assert.deepEqual(ROLE_CHOICES, ["assigned", "fallback", "unassigned"]);
  assert.equal(roleChoiceOf("firstCall"), "assigned");
  assert.equal(roleChoiceOf("featured"), "assigned");
  assert.equal(roleChoiceOf("fallback"), "fallback");
  assert.equal(roleChoiceOf("unassigned"), "unassigned");
  assert.equal(roleFromChoice("assigned", "firstCall"), "firstCall");
  assert.equal(roleFromChoice("assigned", "featured"), "featured");
  assert.equal(roleFromChoice("assigned", "unassigned"), "featured");
  assert.equal(roleFromChoice("assigned", "fallback"), "featured");
  assert.equal(roleFromChoice("fallback", "firstCall"), "fallback");
  assert.equal(roleFromChoice("unassigned", "featured"), "unassigned");
});

test("Role column does not list Featured or First call", () => {
  const detail = readFileSync(new URL("../src/components/subagents-workspace/SubagentsDetail.tsx", import.meta.url), "utf8");
  const roleStart = detail.indexOf("function RoleColumn");
  const roleBody = detail.slice(roleStart, detail.indexOf("\nfunction ", roleStart + 1));
  assert.equal(roleBody.includes('"featured"'), false);
  assert.equal(roleBody.includes('"firstCall"'), false);
  assert.equal(roleBody.includes("ROLE_CHOICES"), true);
  assert.equal(roleBody.includes("roleChoiceOf"), true);
  assert.equal(detail.includes('["featured", "firstCall", "fallback", "unassigned"]'), false);
});

test("Assigned and Model order share pointer reorder with sliding rows", () => {
  const detail = readFileSync(new URL("../src/components/subagents-workspace/SubagentsDetail.tsx", import.meta.url), "utf8");
  const rail = readFileSync(new URL("../src/components/subagents-workspace/SubagentsRail.tsx", import.meta.url), "utf8");
  const css = readFileSync(new URL("../src/styles-subagents.css", import.meta.url), "utf8");
  const hook = readFileSync(new URL("../src/components/subagents-workspace/use-pointer-reorder.ts", import.meta.url), "utf8");
  assert.equal(detail.includes("usePointerReorder"), true);
  assert.equal(rail.includes("usePointerReorder"), true);
  assert.equal(rail.includes("draggable"), false);
  assert.equal(rail.includes("dataTransfer"), false);
  assert.equal(hook.includes("reorderShiftY"), true);
  assert.equal(hook.includes("prefers-reduced-motion"), true);
  assert.match(css, /\.subagents-order tbody tr\s*\{[^}]*display:\s*grid/);
  assert.equal(css.includes(".subagents-assigned-ghost"), true);
  assert.equal(css.includes(".subagents-group--assigned.is-reordering"), true);
  const assignedReorder = css.slice(
    css.indexOf(".subagents-group--assigned.is-reordering"),
    css.indexOf(".subagents-assigned-ghost"),
  );
  const orderReorder = css.slice(
    css.indexOf(".subagents-order-scroll.is-reordering"),
    css.indexOf(".subagents-order-ghost"),
  );
  assert.equal(assignedReorder.includes("pointer-events: none"), true);
  assert.equal(orderReorder.includes("pointer-events: none"), true);
  assert.equal(css.includes(".subagents-group--assigned.is-reordering .subagents-assigned:hover"), true);
  assert.equal(css.includes(".subagents-order-scroll.is-reordering tbody tr:hover"), true);
  assert.equal(hook.includes("pointerEvents"), true);
});

test("Assigned and Model order share selected fill and corner radius", () => {
  const css = readFileSync(new URL("../src/styles-subagents.css", import.meta.url), "utf8");
  assert.equal(css.includes("--sub-selected: var(--raised)"), true);
  assert.equal(css.includes("--sub-selected-radius: var(--radius-2xs)"), true);
  const assigned = css.slice(css.indexOf(".subagents-assigned,"), css.indexOf(".subagents-assigned {"));
  assert.equal(assigned.includes("border-radius: var(--sub-selected-radius)"), true);
  assert.equal(css.includes(".subagents-assigned.is-active"), true);
  assert.match(css, /\.subagents-assigned\.is-active[\s\S]{0,80}background:\s*var\(--sub-selected\)/);
  assert.match(css, /\.subagents-order tbody tr\s*\{[^}]*border-radius:\s*var\(--sub-selected-radius\)/);
  assert.match(css, /\.subagents-order tr\.is-active[\s\S]{0,80}background:\s*var\(--sub-selected\)/);
});

test("Effort select greys out when disabled", () => {
  const css = readFileSync(new URL("../src/styles-subagents.css", import.meta.url), "utf8");
  const start = css.indexOf(".subagents-fact-row .select-trigger:disabled");
  assert.ok(start >= 0);
  const block = css.slice(start, css.indexOf("}", start) + 1);
  assert.equal(block.includes("opacity: 1"), false);
  assert.equal(block.includes("color: var(--text)"), false);
  const detail = readFileSync(new URL("../src/components/subagents-workspace/SubagentsDetail.tsx", import.meta.url), "utf8");
  assert.equal(detail.includes("!isPrimary"), true);
  assert.equal(detail.includes("delegation.efforts.length === 0"), true);
});
