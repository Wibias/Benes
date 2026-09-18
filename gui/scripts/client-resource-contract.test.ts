import assert from "node:assert/strict";
import test from "node:test";

import { createElement } from "react";
import { renderToString } from "react-dom/server";

import {
  clearClientResourceStoresForTests,
  keyedResourceShouldForceLoad,
  describeClientResources,
  hasPollTimerForTests,
  inspectClientResource,
  listClientResourceKeys,
  pollBucketCountForTests,
  readClientResourceSnapshotForTests,
  seedClientResourceForTests,
  setClientResourceData,
  subscribeClientResourceForTests,
  useClientResource,
  visibilityListenerBoundForTests,
} from "../src/client-resource.ts";
import { installGuiDom, setDocumentHidden } from "./gui-dom-harness.ts";

async function waitUntil(predicate: () => boolean, timeoutMs = 500): Promise<void> {
  const start = Date.now();
  while (!predicate()) {
    if (Date.now() - start > timeoutMs) throw new Error("timed out waiting for resource condition");
    await new Promise((resolve) => setTimeout(resolve, 5));
  }
}

test("keyed stores, snapshots, quiet refresh, and seed revalidation", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    let loads = 0;
    const unsub = subscribeClientResourceForTests<string>("alpha", () => {}, {
      fetcher: async () => {
        loads += 1;
        return "one";
      },
    });
    await waitUntil(() => readClientResourceSnapshotForTests("alpha").data === "one");
    const snap = readClientResourceSnapshotForTests<string>("alpha");
    assert.equal(snap.loading, false);
    assert.equal(snap.refreshing, false);
    assert.equal(snap.hasSucceeded, true);
    assert.equal(snap.lastAttemptOk, true);
    assert.equal(loads, 1);

    setClientResourceData("beta", "seed");
    assert.equal(readClientResourceSnapshotForTests<string>("beta").data, "seed");
    let seedLoads = 0;
    const unsubSeed = subscribeClientResourceForTests<string>("beta", () => {}, {
      fetcher: async () => {
        seedLoads += 1;
        return "fresh";
      },
    });
    await waitUntil(() => readClientResourceSnapshotForTests("beta").data === "fresh");
    assert.equal(seedLoads, 1);
    unsub();
    unsubSeed();
    assert.deepEqual(listClientResourceKeys().sort(), ["alpha", "beta"].sort());
    const inspected = inspectClientResource("alpha");
    assert.equal(inspected?.subscribers, 0);
    assert.equal(describeClientResources().some((row) => row.key === "beta"), true);
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("quiet poll does not cancel in-flight work; replacement request does", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    let started = 0;
    let aborted = 0;
    const hang = async (signal: AbortSignal) => {
      started += 1;
      return await new Promise<string>((resolve, reject) => {
        signal.addEventListener("abort", () => {
          aborted += 1;
          reject(new DOMException("Aborted", "AbortError"));
        }, { once: true });
      });
    };
    const unsub = subscribeClientResourceForTests("hang", () => {}, { fetcher: hang, pollMs: 20 });
    await waitUntil(() => started === 1);
    await new Promise((resolve) => setTimeout(resolve, 40));
    assert.equal(started, 1);
    assert.equal(aborted, 0);

    setClientResourceData("hang", "override");
    assert.equal(aborted, 1);
    unsub();
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("deadline settles a signal-dropping fetcher as failure, not an owner abort", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    const unsub = subscribeClientResourceForTests("late", () => {}, {
      fetcher: async () => await new Promise<string>(() => {}),
      deadlineMs: 30,
    });
    await waitUntil(() => {
      const current = readClientResourceSnapshotForTests("late");
      return current.error !== undefined && current.refreshing === false;
    });
    const snap = readClientResourceSnapshotForTests("late");
    assert.equal(snap.loading, false);
    assert.equal(snap.refreshing, false);
    assert.equal(snap.lastAttemptOk, false);
    assert.match(String(snap.error), /timed out after 30ms/);
    unsub();
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("lowest positive poll interval wins and hidden tabs drop timers unless opted out", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    const fetcher = async () => "x";
    const a = subscribeClientResourceForTests("poll", () => {}, { fetcher, pollMs: 50 });
    const b = subscribeClientResourceForTests("poll", () => {}, { fetcher, pollMs: 20 });
    await waitUntil(() => hasPollTimerForTests("poll"));
    assert.equal(pollBucketCountForTests(), 1);

    setDocumentHidden(true);
    assert.equal(hasPollTimerForTests("poll"), false);

    const c = subscribeClientResourceForTests("poll", () => {}, { fetcher, pollMs: 20, pauseWhenHidden: false });
    assert.equal(hasPollTimerForTests("poll"), true);
    c();
    assert.equal(hasPollTimerForTests("poll"), false);

    setDocumentHidden(false);
    await waitUntil(() => hasPollTimerForTests("poll"));
    a();
    b();
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("unmounting one owner does not destroy another subscriber's in-flight request", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    let live = 0;
    const hanging = async (signal: AbortSignal) => {
      live += 1;
      return await new Promise<string>((resolve, reject) => {
        signal.addEventListener("abort", () => {
          live -= 1;
          reject(new DOMException("Aborted", "AbortError"));
        }, { once: true });
      });
    };
    const owner = subscribeClientResourceForTests("shared", () => {}, { fetcher: hanging });
    await waitUntil(() => live === 1);
    const peer = subscribeClientResourceForTests("shared", () => {}, { fetcher: async () => "ok" });
    peer();
    await new Promise((resolve) => setTimeout(resolve, 20));
    assert.equal(live, 1);
    owner();
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("empty buckets are deleted after the last poller leaves", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    const unsub = subscribeClientResourceForTests("gone", () => {}, { fetcher: async () => 1, pollMs: 25 });
    await waitUntil(() => pollBucketCountForTests() === 1);
    unsub();
    await waitUntil(() => pollBucketCountForTests() === 0);
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("same key shares state; different keys stay isolated", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    let loads = 0;
    const a = subscribeClientResourceForTests("shared-key", () => {}, {
      fetcher: async () => {
        loads += 1;
        return "one";
      },
    });
    await waitUntil(() => readClientResourceSnapshotForTests("shared-key").data === "one");
    const b = subscribeClientResourceForTests("shared-key", () => {}, { fetcher: async () => "two" });
    assert.equal(readClientResourceSnapshotForTests("shared-key").data, "one");
    assert.equal(loads, 1);
    subscribeClientResourceForTests("other-key", () => {}, { fetcher: async () => "other" });
    await waitUntil(() => readClientResourceSnapshotForTests("other-key").data === "other");
    assert.equal(readClientResourceSnapshotForTests("shared-key").data, "one");
    a();
    b();
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("quiet refresh keeps cached data visible", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    let loads = 0;
    const unsub = subscribeClientResourceForTests("quiet", () => {}, {
      fetcher: async (signal) => {
        loads += 1;
        if (loads === 1) return "cached";
        return await new Promise<string>((_, reject) => {
          signal.addEventListener("abort", () => reject(new DOMException("Aborted", "AbortError")), { once: true });
        });
      },
      pollMs: 20,
    });
    await waitUntil(() => readClientResourceSnapshotForTests("quiet").data === "cached");
    await waitUntil(() => loads >= 2);
    const snap = readClientResourceSnapshotForTests("quiet");
    assert.equal(snap.data, "cached");
    assert.equal(snap.loading, false);
    assert.equal(snap.refreshing, true);
    unsub();
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("a late signal-ignoring resolve cannot overwrite a newer generation", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    let finish: ((value: string) => void) | undefined;
    const unsub = subscribeClientResourceForTests("stale", () => {}, {
      fetcher: async () => await new Promise<string>((resolve) => {
        finish = resolve;
      }),
    });
    await waitUntil(() => finish !== undefined);
    setClientResourceData("stale", "override");
    finish?.("late");
    await new Promise((resolve) => setTimeout(resolve, 20));
    assert.equal(readClientResourceSnapshotForTests("stale").data, "override");
    unsub();
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("an unmounted owner cannot later overwrite shared state", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    let finish: ((value: string) => void) | undefined;
    const owner = subscribeClientResourceForTests("peer", () => {}, {
      fetcher: async () => await new Promise<string>((resolve) => {
        finish = resolve;
      }),
    });
    await waitUntil(() => finish !== undefined);
    const peer = subscribeClientResourceForTests("peer", () => {}, { fetcher: async () => "peer" });
    owner();
    finish?.("from-owner");
    await waitUntil(() => readClientResourceSnapshotForTests("peer").data === "peer");
    assert.notEqual(readClientResourceSnapshotForTests("peer").data, "from-owner");
    peer();
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("cadence recomputes when the fastest poller leaves", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    const fetcher = async () => "x";
    const slow = subscribeClientResourceForTests("cadence", () => {}, { fetcher, pollMs: 80 });
    const fast = subscribeClientResourceForTests("cadence", () => {}, { fetcher, pollMs: 20 });
    await waitUntil(() => hasPollTimerForTests("cadence"));
    assert.equal(inspectClientResource("cadence")?.pollMs, 20);
    fast();
    assert.equal(inspectClientResource("cadence")?.pollMs, 80);
    assert.equal(hasPollTimerForTests("cadence"), true);
    slow();
    await waitUntil(() => hasPollTimerForTests("cadence") === false);
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("becoming visible performs a quiet catch-up refresh", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    let loads = 0;
    const unsub = subscribeClientResourceForTests("wake", () => {}, {
      fetcher: async () => {
        loads += 1;
        return `n${loads}`;
      },
      pollMs: 5_000,
    });
    await waitUntil(() => {
      const snap = readClientResourceSnapshotForTests("wake");
      return loads === 1 && snap.data === "n1" && snap.refreshing === false;
    });
    setDocumentHidden(true);
    assert.equal(hasPollTimerForTests("wake"), false);
    setDocumentHidden(false);
    await waitUntil(() => readClientResourceSnapshotForTests("wake").data === "n2");
    assert.equal(loads, 2);
    unsub();
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("fresh initialData can skip the mount fetch; unknown age revalidates", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    seedClientResourceForTests("fresh-seed", "seed", Date.now(), 60_000);
    let loads = 0;
    const live = subscribeClientResourceForTests("fresh-seed", () => {}, {
      fetcher: async () => {
        loads += 1;
        return "net";
      },
      staleAfterMs: 60_000,
    });
    await new Promise((resolve) => setTimeout(resolve, 30));
    assert.equal(loads, 0);
    assert.equal(readClientResourceSnapshotForTests("fresh-seed").data, "seed");
    live();

    seedClientResourceForTests("stale-seed", "old", null, 60_000);
    let staleLoads = 0;
    const stale = subscribeClientResourceForTests("stale-seed", () => {}, {
      fetcher: async () => {
        staleLoads += 1;
        return "net";
      },
      staleAfterMs: 60_000,
    });
    await waitUntil(() => readClientResourceSnapshotForTests("stale-seed").data === "net");
    assert.equal(staleLoads, 1);
    stale();
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("enabled false does not subscribe; 0-to-1 resubscribe keeps the store; last watcher evicts", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    function Off() {
      const view = useClientResource("gated", async () => "nope", { enabled: false });
      return String(view.data ?? "empty");
    }
    assert.equal(renderToString(createElement(Off)), "empty");
    assert.equal(inspectClientResource("gated"), null);

    let loads = 0;
    const first = subscribeClientResourceForTests("keep", () => {}, {
      fetcher: async () => {
        loads += 1;
        return "kept";
      },
    });
    await waitUntil(() => readClientResourceSnapshotForTests("keep").data === "kept");
    first();
    const again = subscribeClientResourceForTests("keep", () => {}, {
      fetcher: async () => {
        loads += 1;
        return "again";
      },
    });
    await new Promise((resolve) => setTimeout(resolve, 20));
    assert.equal(readClientResourceSnapshotForTests("keep").data, "kept");
    assert.equal(loads, 1);
    again();
    await waitUntil(() => inspectClientResource("keep") === null);
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("poll timers and the page visibility listener do not leak", async () => {
  const dom = installGuiDom();
  clearClientResourceStoresForTests();
  try {
    const unsub = subscribeClientResourceForTests("leak", () => {}, { fetcher: async () => 1, pollMs: 30 });
    await waitUntil(() => visibilityListenerBoundForTests());
    unsub();
    await waitUntil(() => visibilityListenerBoundForTests() === false);
    assert.equal(pollBucketCountForTests(), 0);
  } finally {
    clearClientResourceStoresForTests();
    dom.restore();
  }
});

test("keyed deps force-load on the same key and skip when the key moved", () => {
  assert.equal(keyedResourceShouldForceLoad(null, null, ["a"], "k"), false);
  assert.equal(keyedResourceShouldForceLoad(["a"], "k", ["a"], "k"), false);
  assert.equal(keyedResourceShouldForceLoad(["a"], "k", ["b"], "k"), true);
  assert.equal(keyedResourceShouldForceLoad(["a"], "old", ["b"], "new"), false);
});
