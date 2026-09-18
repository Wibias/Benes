import assert from "node:assert/strict";
import test from "node:test";

import { clearStaleViewKeys } from "../src/use-app-route-state.ts";

const RETIRED_LAYOUT_KEYS = [
  "benes-global-view",
  "benes-view",
  "benes-providers-view",
  "benes-subagents-view",
  "benes-storage-view",
  "benes-codexauth-view",
  "benes-apikeys-view",
  "benes-claudecode-view",
  "benes-usage-view",
  "benes-logs-view",
  "benes-models-view",
  "benes-dashboard-view",
];

test("the retired layout keys are swept exactly once each", () => {
  const removed = [];
  const storage = { removeItem: (key) => { removed.push(key); } };
  clearStaleViewKeys(storage);
  assert.deepEqual(removed, RETIRED_LAYOUT_KEYS);
});

test("a storage that refuses removal does not throw", () => {
  let calls = 0;
  const storage = { removeItem: () => { calls += 1; throw new Error("private mode"); } };
  assert.doesNotThrow(() => clearStaleViewKeys(storage));
  assert.equal(calls, RETIRED_LAYOUT_KEYS.length);
});
