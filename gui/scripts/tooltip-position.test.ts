import assert from "node:assert/strict";
import test from "node:test";
import { computeTooltipStyle } from "../src/tooltip-position.ts";

const viewport = { width: 1280, height: 800 };
const icon = { top: 84, bottom: 98, left: 12, right: 26, width: 14, height: 14 };
const bubble = { width: 280, height: 64 };

test("computeTooltipStyle grows a bottom tip to the right of a rail icon", () => {
  const trigger = { ...icon, left: 316, right: 330 };
  const style = computeTooltipStyle(trigger, "bottom", bubble, viewport);
  assert.equal(style.left, 316);
  assert.equal(style.top, 106);
});

test("computeTooltipStyle clamps a bottom tip off the left viewport edge", () => {
  const trigger = { ...icon, left: 2, right: 16 };
  const style = computeTooltipStyle(trigger, "bottom", bubble, viewport);
  assert.equal(style.left, 8);
  assert.equal(style.top, 106);
});

test("computeTooltipStyle clamps a bottom tip off the right edge", () => {
  const trigger = { ...icon, left: 1200, right: 1214 };
  const style = computeTooltipStyle(trigger, "bottom", bubble, viewport);
  assert.equal(style.left, 992);
});

test("computeTooltipStyle flips a top tip below the trigger near the viewport top", () => {
  const trigger = { top: 12, bottom: 26, left: 400, right: 414, width: 14, height: 14 };
  const style = computeTooltipStyle(trigger, "top", bubble, viewport);
  assert.equal(style.top, 34);
});
