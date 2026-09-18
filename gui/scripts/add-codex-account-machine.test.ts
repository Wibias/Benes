/**
 * Focused contracts for the add-account phase machine (Wave B of #236).
 *
 * The machine is pure, so illegal combinations and stale-authority transitions are
 * asserted directly rather than through a rendered dialog.
 */
import assert from "node:assert/strict";
import test from "node:test";

import {
  addAccountReducer,
  initialAddAccountState,
  type AddAccountState,
  type AddAccountEvent,
} from "../src/components/add-codex-account-reducer.ts";

function reduce(start: AddAccountState, ...events: AddAccountEvent[]): AddAccountState {
  return events.reduce(addAccountReducer, start);
}

test("a fresh add starts in the choosing phase with nothing carried over", () => {
  assert.deepEqual(initialAddAccountState(), { phase: "choosing", accountId: "", error: null });
});

test("a re-authentication opens straight into the authorizing phase", () => {
  const state = initialAddAccountState("acct-1");
  assert.equal(state.phase, "authorizing");
  assert.deepEqual(state, {
    phase: "authorizing",
    accountId: "",
    error: null,
    flowId: null,
    authUrl: "",
    manualCode: "",
    manualCodeStatus: "idle",
    notice: null,
  });
});

test("the account id is shared by both phases", () => {
  const choosing = reduce(initialAddAccountState(), { kind: "account-id-changed", accountId: "work" });
  assert.deepEqual(choosing, { phase: "choosing", accountId: "work", error: null });
  const authorizing = reduce(initialAddAccountState("acct"), { kind: "account-id-changed", accountId: "work" });
  assert.equal(authorizing.phase, "authorizing");
  assert.equal(authorizing.accountId, "work");
});

test("an opened authorization moves choosing into authorizing with a URL and flow", () => {
  const state = reduce(
    initialAddAccountState(),
    { kind: "account-id-changed", accountId: "work" },
    { kind: "authorization-opened", flowId: "flow-1", authUrl: "https://auth" },
  );
  assert.deepEqual(state, {
    phase: "authorizing",
    accountId: "work",
    error: null,
    flowId: "flow-1",
    authUrl: "https://auth",
    manualCode: "",
    manualCodeStatus: "idle",
    notice: null,
  });
});

test("authorizing-phase-only events are ignored while choosing", () => {
  const choosing = initialAddAccountState();
  const ignored: AddAccountEvent[] = [
    { kind: "manual-code-changed", manualCode: "abc" },
    { kind: "manual-code-submitted" },
    { kind: "manual-code-awaiting" },
    { kind: "manual-code-submit-failed", error: "x" },
    { kind: "manual-code-reset" },
    { kind: "notice-raised", notice: { message: "hi", tone: "ok" } },
    { kind: "notice-cleared" },
    { kind: "authorization-closed" },
  ];
  for (const event of ignored) {
    assert.deepEqual(addAccountReducer(choosing, event), choosing);
  }
});

test("submitting a manual code clears any notice and error first", () => {
  const state = reduce(
    initialAddAccountState("acct"),
    { kind: "notice-raised", notice: { message: "retrying", tone: "warn" } },
    { kind: "authorization-failed", error: "boom" },
    { kind: "manual-code-submitted" },
  );
  assert.equal(state.phase, "authorizing");
  assert.equal(state.error, null);
  assert.equal(state.notice, null);
  assert.equal(state.manualCodeStatus, "submitting");
});

test("a submitted code moves to awaiting and drops the value from the field", () => {
  const state = reduce(
    initialAddAccountState("acct"),
    { kind: "manual-code-changed", manualCode: "code-123" },
    { kind: "manual-code-awaiting" },
  );
  assert.equal(state.phase, "authorizing");
  assert.equal(state.manualCode, "");
  assert.equal(state.manualCodeStatus, "awaiting");
});

test("a failed manual code keeps what was typed but stops the spinner", () => {
  const state = reduce(
    initialAddAccountState("acct"),
    { kind: "manual-code-changed", manualCode: "code-123" },
    { kind: "manual-code-submitted" },
    { kind: "manual-code-submit-failed", error: "network" },
  );
  assert.equal(state.phase, "authorizing");
  assert.equal(state.manualCode, "code-123");
  assert.equal(state.manualCodeStatus, "idle");
  assert.equal(state.error, "network");
});

test("closing a flow leaves the phase but removes the URL and flow id", () => {
  const state = reduce(
    initialAddAccountState(),
    { kind: "authorization-opened", flowId: "flow-1", authUrl: "https://auth" },
    { kind: "authorization-closed" },
  );
  assert.equal(state.phase, "authorizing");
  assert.equal(state.flowId, null);
  assert.equal(state.authUrl, "");
});

test("returning to choosing makes the abandoned authorization unreachable", () => {
  const state = reduce(
    initialAddAccountState("acct"),
    { kind: "authorization-opened", flowId: "flow-1", authUrl: "https://auth" },
    { kind: "authorization-failed", error: "bad" },
    { kind: "returned-to-choosing" },
  );
  assert.deepEqual(state, { phase: "choosing", accountId: "", error: "bad" });
  assert.equal("authUrl" in state, false);
  assert.equal("flowId" in state, false);
});

test("a retry starts clean so the previous attempt cannot leak into the new one", () => {
  const carried = reduce(
    initialAddAccountState("acct"),
    { kind: "authorization-opened", flowId: "flow-old", authUrl: "https://old" },
    { kind: "manual-code-changed", manualCode: "old-code" },
    { kind: "notice-raised", notice: { message: "retrying", tone: "warn" } },
    { kind: "authorization-requested" },
  );
  assert.deepEqual(carried, {
    phase: "authorizing",
    accountId: "",
    error: null,
    flowId: null,
    authUrl: "",
    manualCode: "",
    manualCodeStatus: "idle",
    notice: null,
  });
});

test("closing the dialog twice is idempotent", () => {
  const closed = reduce(
    initialAddAccountState("acct"),
    { kind: "authorization-opened", flowId: "flow-1", authUrl: "https://auth" },
    { kind: "authorization-closed" },
    { kind: "authorization-closed" },
  );
  assert.equal(closed.phase, "authorizing");
  assert.equal(closed.authUrl, "");
});

test("an opened authorization replaces the previous flow rather than stacking", () => {
  const state = reduce(
    initialAddAccountState(),
    { kind: "authorization-opened", flowId: "flow-a", authUrl: "https://a" },
    { kind: "authorization-opened", flowId: "flow-b", authUrl: "https://b" },
  );
  assert.equal(state.phase, "authorizing");
  assert.equal(state.flowId, "flow-b");
  assert.equal(state.authUrl, "https://b");
});

