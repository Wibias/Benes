import assert from "node:assert/strict";
import test from "node:test";

import {
  clearSessionListCache,
  readSessionListCache,
  readSessionListCacheEntry,
  writeSessionListCache,
  writeSessionListCacheEntry,
} from "../src/session-list-cache.ts";
import { classifyDataSurface } from "../src/data-surface.ts";

function fakeStorage() {
  const map = new Map();
  return {
    getItem: (key) => (map.has(key) ? map.get(key) : null),
    setItem: (key, value) => { map.set(key, String(value)); },
    removeItem: (key) => { map.delete(key); },
    keys: () => [...map.keys()],
  };
}

function withStorage(store, run) {
  const previous = Object.getOwnPropertyDescriptor(globalThis, "sessionStorage");
  Object.defineProperty(globalThis, "sessionStorage", { value: store, configurable: true, writable: true });
  try {
    return run();
  } finally {
    if (previous) Object.defineProperty(globalThis, "sessionStorage", previous);
    else delete globalThis.sessionStorage;
  }
}

test("a written seed carries its write time and reads back with its age", () => {
  const store = fakeStorage();
  withStorage(store, () => {
    writeSessionListCacheEntry("k", { rows: 2 });
    const entry = readSessionListCacheEntry("k");
    assert.deepEqual(entry?.data, { rows: 2 });
    assert.equal(typeof entry?.cachedAt, "number");
    assert.deepEqual(readSessionListCache("k"), { rows: 2 });
  });
});

test("a legacy untimestamped value reads as unknown age", () => {
  const store = fakeStorage();
  store.setItem("k", JSON.stringify({ rows: 1 }));
  withStorage(store, () => {
    assert.deepEqual(readSessionListCacheEntry("k"), { data: { rows: 1 }, cachedAt: null });
    assert.deepEqual(readSessionListCache("k"), { rows: 1 });
  });
});

test("a raw write stays raw and a falsy payload survives the round trip", () => {
  const store = fakeStorage();
  withStorage(store, () => {
    writeSessionListCache("raw", { rows: 3 });
    assert.deepEqual(readSessionListCache("raw"), { rows: 3 });
    writeSessionListCacheEntry("falsy", false);
    assert.equal(readSessionListCache("falsy"), false);
    assert.deepEqual(readSessionListCacheEntry("falsy"), { data: false, cachedAt: readSessionListCacheEntry("falsy").cachedAt });
  });
});

test("corrupt, missing, and cleared entries read as no cache", () => {
  const store = fakeStorage();
  store.setItem("broken", "{not json");
  withStorage(store, () => {
    assert.equal(readSessionListCacheEntry("broken"), null);
    assert.equal(readSessionListCache("broken"), null);
    assert.equal(readSessionListCacheEntry("missing"), null);
    writeSessionListCacheEntry("gone", 1);
    clearSessionListCache("gone");
    assert.equal(readSessionListCacheEntry("gone"), null);
    assert.deepEqual(store.keys(), ["broken"]);
  });
});

test("a host without a store reads null and writes nothing", () => {
  const store = fakeStorage();
  withStorage(undefined, () => {
    assert.equal(readSessionListCacheEntry("k"), null);
    assert.equal(readSessionListCache("k"), null);
    writeSessionListCacheEntry("k", 1);
    writeSessionListCache("k", 1);
    clearSessionListCache("k");
  });
  assert.deepEqual(store.keys(), []);
});

function snapshot(overrides = {}) {
  return {
    data: undefined,
    error: undefined,
    loading: false,
    refreshing: false,
    hasSucceeded: false,
    lastAttemptOk: false,
    ...overrides,
  };
}

const notEmpty = () => false;

test("a disabled surface shows nothing and never a skeleton", () => {
  const state = classifyDataSurface(snapshot({ data: [1] }), notEmpty, false);
  assert.equal(state.kind, "disabled");
  assert.equal(state.data, undefined);
  assert.equal(state.showSkeleton, false);
  assert.equal(state.refreshing, false);
  assert.equal(state.showError, false);
});

test("a cold first attempt shows a skeleton without an error", () => {
  const state = classifyDataSurface(snapshot({ refreshing: true }), notEmpty, true);
  assert.equal(state.kind, "cold");
  assert.equal(state.showSkeleton, true);
  assert.equal(state.refreshing, true);
  assert.equal(state.showError, false);
  assert.equal(state.error, undefined);
});

test("a cold retry keeps the prior error available but not visible", () => {
  const failure = new Error("boom");
  const state = classifyDataSurface(snapshot({ refreshing: true, error: failure }), notEmpty, true);
  assert.equal(state.kind, "retrying-cold");
  assert.equal(state.showSkeleton, true);
  assert.equal(state.showError, false);
  assert.equal(state.error, failure);
});

test("an in-flight refresh keeps stale content and shows progress", () => {
  const rows = [1, 2];
  const state = classifyDataSurface(snapshot({ data: rows, refreshing: true }), notEmpty, true);
  assert.equal(state.kind, "loading-with-stale-data");
  assert.equal(state.data, rows);
  assert.equal(state.showSkeleton, false);
  assert.equal(state.refreshing, true);
  assert.equal(state.showError, false);
});

test("an in-flight retry over stale data still shows the error banner", () => {
  const state = classifyDataSurface(
    snapshot({ data: [1], refreshing: true, error: new Error("nope") }),
    notEmpty,
    true,
  );
  assert.equal(state.kind, "loading-with-stale-data");
  assert.equal(state.showError, true);
});

test("a settled payload splits into ready-empty and ready-populated", () => {
  const empty = classifyDataSurface(snapshot({ data: [], lastAttemptOk: true }), (data) => data.length === 0, true);
  assert.equal(empty.kind, "ready-empty");
  assert.equal(empty.showSkeleton, false);
  assert.equal(empty.error, undefined);
  const full = classifyDataSurface(snapshot({ data: [1], lastAttemptOk: true }), (data) => data.length === 0, true);
  assert.equal(full.kind, "ready-populated");
});

test("failed-cold and failed-with-stale stay distinct", () => {
  const cold = classifyDataSurface(snapshot({ error: new Error("cold") }), notEmpty, true);
  assert.equal(cold.kind, "failed-cold");
  assert.equal(cold.data, undefined);
  assert.equal(cold.showError, true);
  assert.equal(cold.showSkeleton, false);
  const stale = classifyDataSurface(snapshot({ data: [1], error: new Error("stale") }), notEmpty, true);
  assert.equal(stale.kind, "failed-with-stale");
  assert.deepEqual(stale.data, [1]);
  assert.equal(stale.showError, true);
});

test("a settled no-data snapshot without an error is a cold skeleton", () => {
  const state = classifyDataSurface(snapshot({ lastAttemptOk: true }), notEmpty, true);
  assert.equal(state.kind, "cold");
  assert.equal(state.showSkeleton, true);
  assert.equal(state.refreshing, false);
});
