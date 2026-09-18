/**
 * Focused contracts for the pool-rotation state machine (Wave E of #236).
 *
 * The transition is pure, so hydration, stale reads, held reads and save rollbacks are
 * asserted directly instead of through a rendered card.
 */
import assert from "node:assert/strict";
import test from "node:test";
import {
  DEFAULT_POOL_ROTATION,
  INITIAL_POOL_ROTATION_STATE,
  controlsLocked,
  errorKeyFor,
  planStickyCommit,
  poolRotationReducer,
  rotationFromActive,
  rotationWith,
  type PoolRotationEvent,
  type PoolRotationState,
} from "../src/codex-pool-rotation-machine.ts";

const SERVER = {
  strategy: "fill-first" as const,
  stickyLimit: 3,
  resetOrder: "most-headroom" as const,
};

function run(state: PoolRotationState, ...events: PoolRotationEvent[]): PoolRotationState {
  return events.reduce(poolRotationReducer, state);
}

function confirmed(): PoolRotationState {
  return run(INITIAL_POOL_ROTATION_STATE, {
    type: "server-values-arrived",
    rotation: SERVER,
    startedAtRevision: 0,
  });
}

test("the default rotation paints before any read confirms it, and the controls are inert", () => {
  assert.deepEqual(INITIAL_POOL_ROTATION_STATE.visible, DEFAULT_POOL_ROTATION);
  assert.equal(INITIAL_POOL_ROTATION_STATE.hydrated, false);
  assert.equal(controlsLocked(INITIAL_POOL_ROTATION_STATE), true);
});

test("rotationFromActive only claims payloads that actually carry rotation fields", () => {
  assert.equal(rotationFromActive(undefined), null);
  assert.equal(rotationFromActive({}), null);
  assert.equal(rotationFromActive({ activeCodexAccountId: "a" }), null);
  assert.deepEqual(rotationFromActive({ accountPoolStrategy: "fill-first" }), {
    strategy: "fill-first",
    stickyLimit: DEFAULT_POOL_ROTATION.stickyLimit,
    resetOrder: DEFAULT_POOL_ROTATION.resetOrder,
  });
});

test("a confirmed read unlocks the controls and adopts all three fields", () => {
  const state = confirmed();
  assert.deepEqual(state.confirmed, SERVER);
  assert.deepEqual(state.visible, SERVER);
  assert.equal(state.stickyDraft, "3");
  assert.equal(state.hydrated, true);
  assert.equal(controlsLocked(state), false);
});

test("a read that started before the current revision is ignored", () => {
  const before = confirmed();
  // A write bumps the revision, so the read that started before it carries a stale one.
  const afterWrite = run(before, { type: "write-started" });
  const stale = run(afterWrite, {
    type: "server-values-arrived",
    rotation: { strategy: "oldest-reset", stickyLimit: 9, resetOrder: "oldest-reset" },
    startedAtRevision: before.revision,
  });
  assert.equal(stale, afterWrite);
});

test("a read that lands during a write is held instead of applied", () => {
  const editing = run(confirmed(), { type: "strategy-chosen", strategy: "oldest-reset" });
  const saving = run(editing, { type: "write-started" });
  assert.equal(saving.saving, true);
  const held = run(saving, {
    type: "server-values-arrived",
    rotation: SERVER,
    startedAtRevision: saving.revision,
  });
  assert.equal(held.heldRead, true);
  // The held payload did not move the visible selection.
  assert.equal(held.visible.strategy, "oldest-reset");

  const consumed = run(
    held,
    { type: "write-accepted", rotation: { ...SERVER, strategy: "oldest-reset" } },
    { type: "held-read-consumed" },
  );
  assert.equal(consumed.heldRead, false);
  assert.equal(run(consumed, { type: "held-read-consumed" }), consumed);
});

test("a rejected write rolls the visible values back to the confirmed ones", () => {
  const rejected = run(
    confirmed(),
    { type: "strategy-chosen", strategy: "oldest-reset" },
    { type: "write-started" },
    { type: "write-rejected" },
  );
  assert.deepEqual(rejected.visible, SERVER);
  assert.equal(rejected.stickyDraft, "3");
  assert.equal(rejected.saving, false);
  assert.equal(errorKeyFor(rejected), "accountPool.strategyUpdateFailed");
  assert.equal(controlsLocked(rejected), false);
});

test("a second write cannot start while one is in flight", () => {
  const saving = run(confirmed(), { type: "write-started" });
  assert.equal(run(saving, { type: "write-started" }), saving);
});

test("a cold read failure shows the retry state and a later failure keeps working controls", () => {
  const failed = run(INITIAL_POOL_ROTATION_STATE, { type: "server-read-failed" });
  assert.equal(failed.readFailed, true);
  assert.equal(controlsLocked(failed), true);

  const laterMiss = run(confirmed(), { type: "server-read-failed" });
  assert.equal(laterMiss.readFailed, false);
  assert.deepEqual(laterMiss.visible, SERVER);
});

test("sticky commits classify invalid, unchanged and real edits", () => {
  const state = confirmed();
  assert.deepEqual(planStickyCommit(state, "abc"), { kind: "invalid" });
  assert.deepEqual(planStickyCommit(state, ""), { kind: "invalid" });
  assert.deepEqual(planStickyCommit(state, " 3 "), { kind: "rewrite-draft", text: "3" });
  assert.deepEqual(planStickyCommit(state, "4"), {
    kind: "write",
    rotation: { ...SERVER, stickyLimit: 4 },
  });
});

test("an invalid sticky draft restores the confirmed limit and reports its own copy", () => {
  const invalid = run(
    confirmed(),
    { type: "sticky-draft-typed", text: "oops" },
    { type: "sticky-draft-rejected" },
  );
  assert.equal(invalid.stickyDraft, "3");
  assert.equal(errorKeyFor(invalid), "accountPool.stickyLimitInvalid");
});

test("typed sticky text survives until it is committed", () => {
  const typed = run(INITIAL_POOL_ROTATION_STATE, { type: "sticky-draft-typed", text: "12" });
  assert.equal(typed.stickyDraft, "12");
  assert.equal(typed.visible.stickyLimit, DEFAULT_POOL_ROTATION.stickyLimit);
});

test("rotationWith patches only the named field of the visible rotation", () => {
  assert.deepEqual(rotationWith(confirmed(), { resetOrder: "oldest-reset" }), {
    ...SERVER,
    resetOrder: "oldest-reset",
  });
});

test("a new write clears the error slot and an accepted write clears it too", () => {
  const failedWrite = run(confirmed(), { type: "write-started" }, { type: "write-rejected" });
  const retried = run(failedWrite, { type: "write-started" });
  assert.equal(retried.error, null);
  const accepted = run(retried, {
    type: "write-accepted",
    rotation: { ...SERVER, strategy: "oldest-reset" },
  });
  assert.equal(accepted.error, null);
  assert.equal(accepted.visible.strategy, "oldest-reset");
  assert.deepEqual(accepted.confirmed, accepted.visible);
});
