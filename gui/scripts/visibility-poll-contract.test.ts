import assert from "node:assert/strict";
import test from "node:test";

import { startVisibilityPoll } from "../src/visibility-poll.ts";

function stubHost(options = {}) {
  const timers = new Map();
  const listeners = new Set();
  const intervals = [];
  let nextId = 1;
  const documentStub = {
    visibilityState: options.hidden ? "hidden" : "visible",
    addEventListener: (_type, listener) => { listeners.add(listener); },
    removeEventListener: (_type, listener) => { listeners.delete(listener); },
  };
  const windowStub = {
    setInterval: (fn, ms) => { const id = nextId; nextId += 1; timers.set(id, fn); intervals.push(ms); return id; },
    clearInterval: (id) => { timers.delete(id); },
  };
  const previousWindow = Object.getOwnPropertyDescriptor(globalThis, "window");
  const previousDocument = Object.getOwnPropertyDescriptor(globalThis, "document");
  Object.defineProperty(globalThis, "window", { value: windowStub, configurable: true, writable: true });
  Object.defineProperty(globalThis, "document", { value: documentStub, configurable: true, writable: true });
  return {
    fireTimers: () => { for (const fn of [...timers.values()]) fn(); },
    timerCount: () => timers.size,
    listenerCount: () => listeners.size,
    intervals,
    setHidden: (hidden) => {
      documentStub.visibilityState = hidden ? "hidden" : "visible";
      for (const listener of [...listeners]) listener();
    },
    restore: () => {
      if (previousWindow) Object.defineProperty(globalThis, "window", previousWindow);
      else delete globalThis.window;
      if (previousDocument) Object.defineProperty(globalThis, "document", previousDocument);
      else delete globalThis.document;
    },
  };
}

test("a visible tab holds one interval and ticks on cadence", () => {
  const host = stubHost();
  let ticks = 0;
  const stop = startVisibilityPoll(() => { ticks += 1; }, 5000);
  assert.equal(host.timerCount(), 1);
  assert.equal(host.listenerCount(), 1);
  assert.deepEqual(host.intervals, [5000]);
  assert.equal(ticks, 0);
  host.fireTimers();
  assert.equal(ticks, 1);
  stop();
  assert.equal(host.timerCount(), 0);
  assert.equal(host.listenerCount(), 0);
  host.fireTimers();
  assert.equal(ticks, 1);
  host.restore();
});

test("a hidden tab holds no timer but still listens", () => {
  const host = stubHost({ hidden: true });
  let ticks = 0;
  const stop = startVisibilityPoll(() => { ticks += 1; }, 5000);
  assert.equal(host.timerCount(), 0);
  assert.equal(host.listenerCount(), 1);
  host.setHidden(false);
  assert.equal(ticks, 1);
  assert.equal(host.timerCount(), 1);
  host.setHidden(true);
  assert.equal(host.timerCount(), 0);
  host.setHidden(false);
  assert.equal(ticks, 2);
  stop();
  host.restore();
});

test("pauseWhenHidden false keeps polling off-screen and binds no listener", () => {
  const host = stubHost({ hidden: true });
  let ticks = 0;
  const stop = startVisibilityPoll(() => { ticks += 1; }, 5000, { pauseWhenHidden: false });
  assert.equal(host.timerCount(), 1);
  assert.equal(host.listenerCount(), 0);
  host.fireTimers();
  assert.equal(ticks, 1);
  stop();
  host.restore();
});

test("immediate fires once on start without disturbing the cadence", () => {
  const host = stubHost();
  let ticks = 0;
  const stop = startVisibilityPoll(() => { ticks += 1; }, 5000, { immediate: true });
  assert.equal(ticks, 1);
  assert.equal(host.timerCount(), 1);
  host.fireTimers();
  assert.equal(ticks, 2);
  stop();
  host.restore();
});

test("stop is idempotent and a throwing callback keeps the poll alive", () => {
  const host = stubHost();
  const errors = [];
  const previousError = console.error;
  console.error = (...args) => { errors.push(args); };
  let seen = 0;
  const stop = startVisibilityPoll(() => { seen += 1; throw new Error("poll blew up"); }, 5000);
  try {
    host.fireTimers();
    assert.equal(seen, 1);
    assert.equal(host.timerCount(), 1);
    assert.equal(errors.length, 1);
    assert.equal(errors[0][0], "[visibility-poll]");
    host.fireTimers();
    assert.equal(seen, 2);
    stop();
    stop();
    assert.equal(host.timerCount(), 0);
  } finally {
    console.error = previousError;
    host.restore();
  }
});
