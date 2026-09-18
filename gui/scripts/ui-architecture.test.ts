/**
 * The shared UI layer's decisions, asserted without a renderer.
 *
 * Everything with a branch in it was deliberately moved out of the components: the Select's menu
 * state machine, its row projection, the ARIA relationship bundle, the tooltip's restartable
 * delay, the floating layer's two geometry helpers and the presentation vocabulary. What is left
 * in the projections is markup, which the browser suite covers.
 */
import assert from "node:assert/strict";
import test from "node:test";

import {
  CLOSED_SELECT_MENU,
  selectMenuState,
  selectRows,
  selectTriggerRelations,
} from "../src/components/ui/select-controller.ts";
import {
  TOOLTIP_SHOW_DELAY_MS,
  createDelayTimer,
  tooltipState,
  type TimerClock,
} from "../src/components/ui/tooltip-controller.ts";
import { pointerLeftControl, samePlacement } from "../src/components/ui/floating-layer.ts";
import {
  EMPTY_TEXT_CLASS,
  EMPTY_TITLE_CLASS,
  SWITCH_KNOB_CLASS,
  TOAST_DISMISS_CLASS,
  TOAST_HOST_CLASS,
  emptyStateClassName,
  noticeBodyClassName,
  noticeContainerClassName,
  switchClassName,
} from "../src/ui-presentation.ts";

test("a closed menu has no highlight to remember", () => {
  assert.deepEqual(CLOSED_SELECT_MENU, { open: false, highlight: null });
});

test("opening lands on one option and highlighting moves without closing", () => {
  const opened = selectMenuState(CLOSED_SELECT_MENU, { kind: "open-at", index: 3 });
  assert.deepEqual(opened, { open: true, highlight: 3 });
  assert.deepEqual(selectMenuState(opened, { kind: "highlight", index: 1 }), { open: true, highlight: 1 });
});

test("a dismissal forgets the highlight but a Tab commit keeps it", () => {
  const opened = selectMenuState(CLOSED_SELECT_MENU, { kind: "open-at", index: 2 });
  assert.deepEqual(selectMenuState(opened, { kind: "close" }), CLOSED_SELECT_MENU);
  assert.deepEqual(selectMenuState(opened, { kind: "hide" }), { open: false, highlight: 2 });
});

test("highlighting while closed does not open the menu", () => {
  const state = selectMenuState(CLOSED_SELECT_MENU, { kind: "highlight", index: 4 });
  assert.equal(state.open, false);
  assert.equal(state.highlight, 4);
});

test("the row projection carries the id, the selection and the active class", () => {
  const options = [{ value: "b", label: "Bee" }, { value: "a", label: "Ay" }];
  const rows = selectRows(options, "a", 1, false, index => `list-${index}`);
  assert.deepEqual(rows.map(row => row.id), ["list-0", "list-1"]);
  assert.deepEqual(rows.map(row => row.selected), [false, true]);
  assert.deepEqual(rows.map(row => row.label), ["Bee", "Ay"]);
  assert.equal(rows[0]?.className, "select-option");
  assert.equal(rows[1]?.className, "select-option active select-option-active");
});

test("rows inherit the control's disabled state so a late disable cannot commit", () => {
  const rows = selectRows([{ value: "a", label: "Ay" }], "a", 0, true, () => "id");
  assert.equal(rows[0]?.disabled, true);
  const enabled = selectRows([{ value: "a", label: "Ay" }], "a", 0, false, () => "id");
  assert.equal(enabled[0]?.disabled, false);
});

test("the combobox only claims a listbox it is actually showing", () => {
  const closed = selectTriggerRelations({ open: false, listboxId: "list", activeOptionId: undefined });
  assert.equal(closed.role, "combobox");
  assert.equal(closed["aria-haspopup"], "listbox");
  assert.equal(closed["aria-expanded"], false);
  assert.equal(closed["aria-controls"], undefined);
  assert.equal(closed["aria-activedescendant"], undefined);

  const open = selectTriggerRelations({ open: true, listboxId: "list", activeOptionId: "list-1" });
  assert.equal(open["aria-expanded"], true);
  assert.equal(open["aria-controls"], "list");
  assert.equal(open["aria-activedescendant"], "list-1");
});

test("an open menu with no option under the cursor names no active descendant", () => {
  const relations = selectTriggerRelations({ open: true, listboxId: "list", activeOptionId: undefined });
  assert.equal(relations["aria-controls"], "list");
  assert.equal(relations["aria-activedescendant"], undefined);
});

test("the caller's naming and tooltip pass straight through the trigger relations", () => {
  const relations = selectTriggerRelations({
    open: false,
    listboxId: "list",
    activeOptionId: undefined,
    label: "Model",
    describedBy: "hint",
    title: "Pick one",
  });
  assert.equal(relations["aria-label"], "Model");
  assert.equal(relations["aria-describedby"], "hint");
  assert.equal(relations.title, "Pick one");
});

function fakeClock() {
  const scheduled = new Map<number, () => void>();
  let next = 1;
  const clock: TimerClock = {
    setTimeout(run) {
      const handle = next;
      next += 1;
      scheduled.set(handle, run);
      return handle;
    },
    clearTimeout(handle) {
      scheduled.delete(handle);
    },
  };
  return {
    clock,
    fire(handle: number) {
      const run = scheduled.get(handle);
      scheduled.delete(handle);
      run?.();
    },
    handles: () => [...scheduled.keys()],
  };
}

test("the tooltip delay is a real delay, not an immediate show", () => {
  const fake = fakeClock();
  const timer = createDelayTimer(TOOLTIP_SHOW_DELAY_MS, fake.clock);
  let shown = 0;
  timer.schedule(() => { shown += 1; });
  assert.equal(shown, 0);
  assert.equal(timer.pending, true);
  fake.fire(fake.handles()[0]!);
  assert.equal(shown, 1);
  assert.equal(timer.pending, false);
});

test("a repeated show replaces the pending run instead of stacking one", () => {
  const fake = fakeClock();
  const timer = createDelayTimer(TOOLTIP_SHOW_DELAY_MS, fake.clock);
  let shown = 0;
  timer.schedule(() => { shown += 1; });
  timer.schedule(() => { shown += 1; });
  assert.equal(fake.handles().length, 1);
  fake.fire(fake.handles()[0]!);
  assert.equal(shown, 1);
});

test("cancelling leaves nothing to fire, which is what unmount relies on", () => {
  const fake = fakeClock();
  const timer = createDelayTimer(TOOLTIP_SHOW_DELAY_MS, fake.clock);
  let shown = 0;
  timer.schedule(() => { shown += 1; });
  timer.cancel();
  assert.equal(timer.pending, false);
  assert.deepEqual(fake.handles(), []);
  assert.equal(shown, 0);
  timer.cancel();
  assert.equal(timer.pending, false);
});

test("tooltip visibility is only what the last event said", () => {
  assert.deepEqual(tooltipState({ open: false }, { kind: "show" }), { open: true });
  assert.deepEqual(tooltipState({ open: true }, { kind: "hide" }), { open: false });
  assert.deepEqual(tooltipState({ open: true }, { kind: "show" }), { open: true });
});

test("identical placements are recognised so a settled layer stops re-rendering", () => {
  assert.equal(samePlacement(undefined, undefined), true);
  assert.equal(samePlacement(undefined, { top: 1 }), false);
  assert.equal(samePlacement({ top: 1 }, undefined), false);
  assert.equal(samePlacement({ top: 1, left: 2 }, { left: 2, top: 1 }), true);
  assert.equal(samePlacement({ top: 1 }, { top: 2 }), false);
  assert.equal(samePlacement({ top: 1 }, { left: 1 }), false);
  assert.equal(samePlacement({ width: undefined }, {}), true);
});

/** A node that answers containment the way the DOM does. */
function fakeNode(label: string, contains: string[] = []) {
  return {
    label,
    contains: (other: unknown) => contains.includes((other as { label?: string })?.label ?? ""),
  } as unknown as Node;
}

test("a press inside the control or its layer is not a dismissal", () => {
  const target = fakeNode("option");
  assert.equal(pointerLeftControl(target, fakeNode("wrapper", ["option"]), null), false);
  assert.equal(pointerLeftControl(target, fakeNode("wrapper"), fakeNode("menu", ["option"])), false);
  assert.equal(pointerLeftControl(target, fakeNode("wrapper"), null), true);
  assert.equal(pointerLeftControl(target, null, null), true);
});

test("the notice vocabulary keeps each tone on its own class", () => {
  assert.equal(noticeContainerClassName("inline", "ok"), "notice notice-ok");
  assert.equal(noticeContainerClassName("inline", "warn"), "notice notice-warn");
  assert.equal(noticeContainerClassName("inline", "err"), "notice notice-err");
  assert.equal(noticeContainerClassName("toast", "ok"), "toast-notice notice notice-ok");
  assert.equal(noticeBodyClassName("toast"), "toast-notice-copy");
  assert.equal(noticeBodyClassName("inline"), undefined);
});

test("the standalone primitives keep the class names the stylesheet keys off", () => {
  assert.equal(switchClassName(true), "switch on");
  assert.equal(switchClassName(false), "switch");
  assert.equal(SWITCH_KNOB_CLASS, "knob");
  assert.equal(TOAST_HOST_CLASS, "toast-notice-host");
  assert.equal(TOAST_DISMISS_CLASS, "toast-notice-dismiss");
  assert.equal(EMPTY_TITLE_CLASS, "title");
  assert.equal(EMPTY_TEXT_CLASS, "text-control");
  assert.equal(emptyStateClassName(), "empty");
  assert.equal(emptyStateClassName("wide"), "empty wide");
});
