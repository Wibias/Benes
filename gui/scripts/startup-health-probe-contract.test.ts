import assert from "node:assert/strict";
import test from "node:test";

import {
  beginPollEpoch,
  mapStartupHealthProbe,
  probeNeedsFastRetry,
  settingsPollMayCommit,
} from "../src/startup-health-ui.ts";

test("a chip status passes through unchanged", () => {
  assert.equal(mapStartupHealthProbe({ status: "native" }), "native");
  assert.equal(mapStartupHealthProbe({ status: "protected" }), "protected");
  assert.equal(mapStartupHealthProbe({ status: "at-risk" }), "at-risk");
});

test("a stale-while-revalidate answer keeps its real status", () => {
  assert.equal(mapStartupHealthProbe({ status: "at-risk", diagnosticStale: true }), "at-risk");
  assert.equal(mapStartupHealthProbe({ status: "protected", diagnosticStale: true }), "protected");
});

test("only a hard read failure is unknown", () => {
  assert.equal(mapStartupHealthProbe({ status: "error" }), null);
  assert.equal(mapStartupHealthProbe({}), null);
  assert.equal(mapStartupHealthProbe({ status: "weird", ok: false }), null);
});

test("an older listener payload maps from ok and proxy", () => {
  assert.equal(mapStartupHealthProbe({ ok: true, proxy: "running" }), "protected");
  assert.equal(mapStartupHealthProbe({ ok: true, proxy: "stopped" }), "at-risk");
});

test("only an in-progress server refresh asks for a fast retry", () => {
  assert.equal(probeNeedsFastRetry(undefined), false);
  assert.equal(probeNeedsFastRetry(null), false);
  assert.equal(probeNeedsFastRetry({ status: "native", stale: false }), false);
  assert.equal(probeNeedsFastRetry({ status: "at-risk", stale: true }), true);
  assert.equal(probeNeedsFastRetry({ status: "error", stale: true }), false);
});

test("a poll commits only when nothing moved and no mutation is running", () => {
  const atFetch = { request: 3, mutation: 7 };
  const unchanged = { request: 3, mutation: 7, mutationInFlight: false };
  assert.equal(settingsPollMayCommit(atFetch, unchanged), true);
  assert.equal(settingsPollMayCommit(atFetch, { ...unchanged, mutationInFlight: true }), false);
  assert.equal(settingsPollMayCommit(atFetch, { ...unchanged, request: 4 }), false);
  assert.equal(settingsPollMayCommit(atFetch, { ...unchanged, mutation: 8 }), false);
});

test("beginPollEpoch bumps the request epoch and snapshots the mutation epoch", () => {
  const requestRef = { current: 4 };
  const mutationRef = { current: 9 };
  assert.deepEqual(beginPollEpoch(requestRef, mutationRef), { request: 5, mutation: 9 });
  assert.equal(requestRef.current, 5);
  assert.equal(mutationRef.current, 9);
});
