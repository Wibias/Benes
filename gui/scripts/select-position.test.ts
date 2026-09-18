import assert from "node:assert/strict";
import test from "node:test";
import { computeSelectMenuStyle } from "../src/select-position.ts";

const trigger = {
  top: 192,
  bottom: 224,
  left: 280,
  right: 360,
  width: 80,
  height: 32,
};

const chip = {
  top: 188,
  bottom: 224,
  left: 221,
  right: 366,
  width: 145,
  height: 36,
};

test("computeSelectMenuStyle sizes the menu to the trigger when nothing else is matched", () => {
  const style = computeSelectMenuStyle(trigger, { align: "left" });
  assert.equal(style.left, 280);
  assert.equal(style.minWidth, 80);
  assert.equal(style.width, undefined);
  assert.equal(style.top, 228);
});

test("computeSelectMenuStyle match uses the inactive control box", () => {
  const style = computeSelectMenuStyle(trigger, { align: "left", match: chip });
  assert.equal(style.left, 221);
  assert.equal(style.minWidth, 145);
  assert.equal(style.width, 145);
  assert.equal(style.top, 228);
});

/**
 * The suite above only pins one left-aligned trigger and one matched box. The geometry
 * also has to hold for right alignment, the automatic alignment fallback, the flip above
 * the trigger, viewport clamping on both edges, the height band, and beside placement.
 * Node has no `window`, so these run against the 1024x800 fallback viewport.
 */
const VIEWPORT = { width: 1024, height: 800 } as const;

function rect(overrides: Record<string, number> = {}) {
  const box = { top: 192, bottom: 224, left: 280, right: 360, width: 80, height: 32, ...overrides };
  return box;
}

test("computeSelectMenuStyle pins right alignment to the trigger's right edge", () => {
  const style = computeSelectMenuStyle(trigger, { align: "right" });
  assert.equal(style.right, VIEWPORT.width - trigger.right);
  assert.equal(style.left, undefined);
  assert.equal(style.top, trigger.bottom + 4);
});

test("computeSelectMenuStyle chooses the alignment from the box centre when unset", () => {
  const nearLeft = computeSelectMenuStyle(rect({ left: 40, right: 120, width: 80 }), {});
  assert.equal(nearLeft.left, 40);
  const nearRight = computeSelectMenuStyle(rect({ left: 520, right: 600, width: 80 }), {});
  assert.equal(nearRight.right, VIEWPORT.width - 600);
});

test("computeSelectMenuStyle flips above a trigger with no room below", () => {
  const low = rect({ top: 700, bottom: 732, left: 100, right: 180, width: 80 });
  const style = computeSelectMenuStyle(low, { align: "left" });
  assert.equal(style.top, undefined);
  assert.equal(style.bottom, VIEWPORT.height - low.top + 8);
  assert.equal(style.left, 100);
  assert.equal(style.maxHeight, 280);
});

test("computeSelectMenuStyle stays below when the space above is not larger", () => {
  const mid = rect({ top: 400, bottom: 432, left: 100, right: 180, width: 80 });
  const style = computeSelectMenuStyle(mid, { align: "left" });
  assert.equal(style.top, mid.bottom + 4);
  assert.equal(style.bottom, undefined);
  assert.equal(style.maxHeight, 280);
});

test("computeSelectMenuStyle clamps the menu to the viewport edges", () => {
  const offLeft = computeSelectMenuStyle(
    rect({ left: -50, right: 30, width: 80, top: 100, bottom: 132 }),
    { align: "left" },
  );
  assert.equal(offLeft.left, 8);
  const offRight = computeSelectMenuStyle(
    rect({ left: 1000, right: 1080, width: 80, top: 100, bottom: 132 }),
    { align: "left" },
  );
  assert.equal(offRight.left, VIEWPORT.width - 8 - 80);
});

test("computeSelectMenuStyle floors the menu height when neither side has room", () => {
  const squeezed = rect({ top: 10, bottom: 790, left: 10, right: 90, width: 80, height: 780 });
  const style = computeSelectMenuStyle(squeezed, { align: "left" });
  assert.equal(style.top, 794);
  assert.equal(style.maxHeight, 0);
});

test("computeSelectMenuStyle keeps the requested height inside the 120-280 band", () => {
  const tall = computeSelectMenuStyle(trigger, { align: "left", menuHeight: 5000 });
  assert.equal(tall.maxHeight, 280);
  const short = computeSelectMenuStyle(trigger, { align: "left", menuHeight: 10 });
  assert.equal(short.maxHeight, 280);
});

test("computeSelectMenuStyle does not repeat the matched width without a match box", () => {
  const unmatched = computeSelectMenuStyle(trigger, { align: "left" });
  assert.equal(unmatched.width, undefined);
});

test("computeSelectMenuStyle places a beside menu past the trigger's right edge", () => {
  const style = computeSelectMenuStyle(trigger, { placement: "right" });
  assert.equal(style.left, trigger.right + 12);
  assert.equal(style.top, trigger.top);
  assert.equal(style.minWidth, 160);
  assert.equal(style.maxHeight, 280);
});

test("computeSelectMenuStyle opens a beside menu upward against a low trigger", () => {
  const low = rect({ top: 650, bottom: 682, left: 280, right: 360, width: 80 });
  const style = computeSelectMenuStyle(low, { placement: "right" });
  assert.equal(style.bottom, VIEWPORT.height - low.top + 8);
  assert.equal(style.top, undefined);
  assert.equal(style.left, low.right + 12);
  assert.equal(style.minWidth, 160);
  assert.equal(style.maxHeight, 280);
});

test("computeSelectMenuStyle clamps a beside menu away from the right viewport edge", () => {
  const style = computeSelectMenuStyle(rect({ right: 1000 }), { placement: "right" });
  assert.equal(style.left, VIEWPORT.width - 8 - 160);
  const offLeft = computeSelectMenuStyle(rect({ right: -500 }), { placement: "right" });
  assert.equal(offLeft.left, 8);
});
