/**
 * Focused contracts for the auto-switch state machine (Wave D of #236).
 *
 * The record transition is pure, so the stale-read and save-race behaviour is
 * asserted directly instead of through a rendered card.
 */
import assert from "node:assert/strict";
import test from "node:test";

import {
  autoSwitchReducer,
  initialAutoSwitchState,
  type AutoSwitchState,
  type AutoSwitchEvent,
} from "../src/hooks/useCodexAutoSwitch.ts";

function run(start: AutoSwitchState, ...events: AutoSwitchEvent[]): AutoSwitchState {
  return events.reduce(autoSwitchReducer, start);
}

test("the card seeds the default and refuses to write until /active confirms", () => {
  const state = initialAutoSwitchState();
  assert.equal(state.threshold, 80);
  assert.equal(state.draft, "80");
  assert.equal(state.hydrated, false);
});

test("a server value confirms the threshold and re-derives the draft", () => {
  const state = run(initialAutoSwitchState(), { kind: "server-value", value: 25 });
  assert.equal(state.threshold, 25);
  assert.equal(state.draft, "25");
  assert.equal(state.hydrated, true);
  assert.equal(state.lastEnabled, 25);
});

test("a disabled server value keeps the last enabled threshold in the draft", () => {
  const state = run(
    initialAutoSwitchState(),
    { kind: "server-value", value: 25 },
    { kind: "server-value", value: 0 },
  );
  assert.equal(state.threshold, 0);
  assert.equal(state.draft, "25");
});

test("a read landing mid-edit is held rather than applied", () => {
  const state = run(
    initialAutoSwitchState(),
    { kind: "server-value", value: 25 },
    { kind: "draft-changed", text: "60" },
    { kind: "server-value", value: 70 },
  );
  assert.equal(state.threshold, 25, "the confirmed value must not move under a draft");
  assert.equal(state.draft, "60", "the operator's draft wins");
  assert.equal(state.deferred, 70);
});

test("a read landing mid-save is held rather than applied", () => {
  const state = run(
    initialAutoSwitchState(),
    { kind: "server-value", value: 25 },
    { kind: "save-began" },
    { kind: "server-value", value: 70 },
  );
  assert.equal(state.threshold, 25);
  assert.equal(state.deferred, 70);
  assert.equal(state.saving, true);
});

test("flushing a held value confirms it and clears the hold", () => {
  const state = run(
    initialAutoSwitchState(),
    { kind: "draft-changed", text: "60" },
    { kind: "server-value-held", value: 40 },
    { kind: "hold-flushed" },
  );
  assert.equal(state.deferred, null);
  assert.equal(state.threshold, 40);
  assert.equal(state.draft, "40");
});

test("flushing with nothing held is a no-op", () => {
  const before = run(initialAutoSwitchState(), { kind: "server-value", value: 25 });
  assert.deepEqual(autoSwitchReducer(before, { kind: "hold-flushed" }), before);
});

test("a failed read only surfaces before the first successful confirmation", () => {
  const cold = autoSwitchReducer(initialAutoSwitchState(), { kind: "read-failed" });
  assert.equal(cold.loadError, true);
  const warm = autoSwitchReducer(run(initialAutoSwitchState(), { kind: "server-value", value: 5 }), { kind: "read-failed" });
  assert.equal(warm.loadError, false);
});

test("a save bumps the revision so older reads are superseded", () => {
  const started = run(initialAutoSwitchState(), { kind: "server-value", value: 5 }, { kind: "save-began" });
  assert.equal(started.revision, 1);
  const accepted = autoSwitchReducer(started, { kind: "save-accepted", value: 30 });
  assert.equal(accepted.revision, 2);
  assert.equal(accepted.threshold, 30);
});

test("a rejected save restores the confirmed value when nothing is held", () => {
  const state = run(
    initialAutoSwitchState(),
    { kind: "server-value", value: 25 },
    { kind: "save-began" },
    { kind: "save-rejected", previous: 25 },
  );
  assert.equal(state.threshold, 25);
  assert.equal(state.draft, "25");
  assert.equal(state.deferred, null);
});

test("a rejected save prefers a value that arrived while it was in flight", () => {
  const state = run(
    initialAutoSwitchState(),
    { kind: "server-value", value: 25 },
    { kind: "save-began" },
    { kind: "server-value-held", value: 40 },
    { kind: "save-rejected", previous: 25 },
  );
  assert.equal(state.threshold, 40);
  assert.equal(state.draft, "40");
});

test("cancelling a draft restores it and flags the cancellation", () => {
  const state = run(
    initialAutoSwitchState(),
    { kind: "server-value", value: 25 },
    { kind: "draft-changed", text: "60" },
    { kind: "draft-cancelled" },
  );
  assert.equal(state.draft, "25");
  assert.equal(state.editing, false);
  assert.equal(state.cancelledDraft, true);
});

test("cancelling while a read is held applies the held value instead", () => {
  const state = run(
    initialAutoSwitchState(),
    { kind: "server-value", value: 25 },
    { kind: "draft-changed", text: "60" },
    { kind: "server-value-held", value: 40 },
    { kind: "draft-cancelled" },
  );
  assert.equal(state.threshold, 40);
  assert.equal(state.draft, "40");
});

test("a rejected draft restores the confirmed draft", () => {
  const state = run(
    initialAutoSwitchState(),
    { kind: "server-value", value: 25 },
    { kind: "draft-changed", text: "nonsense" },
    { kind: "draft-rejected" },
  );
  assert.equal(state.draft, "25");
  assert.equal(state.editing, false);
});

test("toggling off restores the last enabled threshold into the draft", () => {
  const state = run(
    initialAutoSwitchState(),
    { kind: "server-value", value: 40 },
    { kind: "toggle-applied", plan: { threshold: 0, lastEnabled: 40 } },
  );
  assert.equal(state.lastEnabled, 40);
  assert.equal(state.draft, "40");
});

test("editing and notices are one record, so a clear cannot leave a stale tone", () => {
  const withNotice = run(
    initialAutoSwitchState(),
    { kind: "server-value", value: 25 },
    { kind: "notice-raised", message: "saved", error: false },
  );
  assert.deepEqual(withNotice.notice, { tone: "ok", message: "saved" });
  const draft = autoSwitchReducer(withNotice, { kind: "draft-changed", text: "30" });
  assert.equal(draft.notice, null, "editing clears the previous save notice");
  assert.equal(draft.editing, true);
});
