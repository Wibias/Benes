import assert from "node:assert/strict";
import { readFile } from "node:fs/promises";
import test from "node:test";

const overlaysSource = await readFile(
  new URL("../src/components/provider-workspace/provider-overlays.tsx", import.meta.url),
  "utf8",
);

test("provider decision dialog wires native Escape cancellation to dismiss when unlocked", () => {
  assert.match(overlaysSource, /node\.addEventListener\("cancel", onEscape\)/);
  assert.match(overlaysSource, /const onEscape = \(event: Event\) => \{\s*event\.preventDefault\(\);\s*if \(!lockDismiss\) onDismiss\(\);\s*\}/s);
});

test("provider decision dialog dismisses only genuine outside-dialog backdrop clicks", () => {
  assert.match(overlaysSource, /if \(lockDismiss \|\| event\.target !== node\) return;/);
  assert.match(overlaysSource, /const inside = event\.clientX >= box\.left\s*&& event\.clientX <= box\.right\s*&& event\.clientY >= box\.top\s*&& event\.clientY <= box\.bottom;/s);
  assert.match(overlaysSource, /if \(!inside\) onDismiss\(\);/);
});

test("unsaved-leave dialog locks Escape, backdrop, and buttons while save is pending", () => {
  assert.match(overlaysSource, /lockDismiss=\{saving\}/);
  assert.match(overlaysSource, /onClick=\{onCancel\} disabled=\{saving\}/);
  assert.match(overlaysSource, /onClick=\{onDiscard\} disabled=\{saving\}/);
  assert.match(overlaysSource, /onClick=\{onSave\} disabled=\{saving\}/);
});
