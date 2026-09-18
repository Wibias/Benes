import assert from "node:assert/strict";
import test from "node:test";

import { createBoundedFetch } from "../src/bounded-fetch.ts";

function withoutNativeTimeout<T>(run: () => T): T {
  const anyDesc = Object.getOwnPropertyDescriptor(AbortSignal, "any");
  const timeoutDesc = Object.getOwnPropertyDescriptor(AbortSignal, "timeout");
  Object.defineProperty(AbortSignal, "any", { configurable: true, value: undefined });
  Object.defineProperty(AbortSignal, "timeout", { configurable: true, value: undefined });
  try {
    return run();
  } finally {
    if (anyDesc) Object.defineProperty(AbortSignal, "any", anyDesc);
    if (timeoutDesc) Object.defineProperty(AbortSignal, "timeout", timeoutDesc);
  }
}

test("the owner controller can abort the effective signal", () => {
  const bounded = createBoundedFetch(60_000);
  assert.equal(bounded.signal.aborted, false);
  bounded.controller.abort();
  assert.equal(bounded.signal.aborted, true);
  bounded.clear();
});

test("elapsed timeout aborts the effective signal on the native path", async () => {
  const bounded = createBoundedFetch(20);
  await new Promise((resolve) => setTimeout(resolve, 50));
  assert.equal(bounded.signal.aborted, true);
  assert.equal(bounded.controller.signal.aborted, false);
  bounded.clear();
});

test("fallback runtimes abort on timeout and keep manual abort distinct", async () => {
  const bounded = withoutNativeTimeout(() => createBoundedFetch(20));
  assert.equal(bounded.signal.aborted, false);
  await new Promise((resolve) => setTimeout(resolve, 50));
  assert.equal(bounded.signal.aborted, true);
  assert.equal(bounded.controller.signal.aborted, false);
  bounded.clear();
});

test("clear cancels fallback timer ownership without aborting", async () => {
  const bounded = withoutNativeTimeout(() => createBoundedFetch(80));
  bounded.clear();
  await new Promise((resolve) => setTimeout(resolve, 120));
  assert.equal(bounded.signal.aborted, false);
  assert.equal(bounded.controller.signal.aborted, false);
});

test("invalid timeout input is rejected", () => {
  assert.throws(() => createBoundedFetch(Number.NaN), RangeError);
  assert.throws(() => createBoundedFetch(-1), RangeError);
  assert.throws(() => createBoundedFetch(Number.POSITIVE_INFINITY), RangeError);
});

test("zero timeout is an immediate deadline", async () => {
  const bounded = createBoundedFetch(0);
  await new Promise((resolve) => setTimeout(resolve, 10));
  assert.equal(bounded.signal.aborted, true);
  bounded.clear();
});
