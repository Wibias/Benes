/**
 * Executable contracts for createDelegationMutationController.
 * Transport is DI'd with deferred-promise fakes — no source-grep as proof of semantics.
 */
import assert from "node:assert/strict";
import test from "node:test";

import type { DelegationPatch } from "../src/pages/subagents-delegation-contract.ts";
import {
  createDelegationMutationController,
  type DelegationMutationSnapshot,
  type DelegationMutationTransport,
} from "../src/pages/subagents-delegation-mutations.ts";

type Deferred<T> = {
  promise: Promise<T>;
  resolve: (value: T | PromiseLike<T>) => void;
  reject: (reason?: unknown) => void;
};

function deferred<T>(): Deferred<T> {
  let resolve!: (value: T | PromiseLike<T>) => void;
  let reject!: (reason?: unknown) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

function snapshot(partial: Partial<DelegationMutationSnapshot> = {}): DelegationMutationSnapshot {
  return {
    guidanceEnabled: false,
    syncCodexDefaults: false,
    model: "",
    effort: "",
    efforts: [],
    available: [],
    ...partial,
  };
}

function tick(): Promise<void> {
  return new Promise((resolve) => setImmediate(resolve));
}

type Harness = {
  controller: ReturnType<typeof createDelegationMutationController>;
  puts: DelegationPatch[];
  published: DelegationMutationSnapshot[];
  savingLog: boolean[];
  putQueue: Array<Deferred<void>>;
  readQueue: Array<Deferred<DelegationMutationSnapshot>>;
  enqueuePut: () => Deferred<void>;
  enqueueRead: () => Deferred<DelegationMutationSnapshot>;
  getSaving: () => boolean;
};

function createHarness(): Harness {
  const puts: DelegationPatch[] = [];
  const published: DelegationMutationSnapshot[] = [];
  const savingLog: boolean[] = [];
  const putQueue: Array<Deferred<void>> = [];
  const readQueue: Array<Deferred<DelegationMutationSnapshot>> = [];
  let saving = false;
  let putCount = 0;
  let readCount = 0;

  const enqueuePut = () => {
    const d = deferred<void>();
    putQueue.push(d);
    return d;
  };
  const enqueueRead = () => {
    const d = deferred<DelegationMutationSnapshot>();
    readQueue.push(d);
    return d;
  };

  const transport: DelegationMutationTransport = {
    put: async (patch) => {
      const index = putCount++;
      puts.push(structuredClone(patch));
      const d = putQueue[index] ?? enqueuePut();
      await d.promise;
    },
    readAuthority: async () => {
      const index = readCount++;
      const d = readQueue[index] ?? enqueueRead();
      return d.promise;
    },
    publish: (next) => {
      published.push(structuredClone(next));
    },
    setSaving: (value) => {
      saving = value;
      savingLog.push(value);
    },
  };

  return {
    controller: createDelegationMutationController(transport),
    puts,
    published,
    savingLog,
    putQueue,
    readQueue,
    enqueuePut,
    enqueueRead,
    getSaving: () => saving,
  };
}

test("1. at most one PUT is active at a time", async () => {
  const h = createHarness();
  const put0 = h.enqueuePut();
  const read0 = h.enqueueRead();

  const first = h.controller.save({ model: "a/m1" });
  await tick();
  assert.equal(h.puts.length, 1);

  const second = h.controller.save({ effort: "high" });
  await tick();
  assert.equal(h.puts.length, 1, "second save must coalesce, not open another PUT");

  put0.resolve();
  await tick();
  read0.resolve(snapshot({ model: "a/m1", effort: "medium" }));
  await tick();

  assert.equal(h.puts.length, 2);
  const put1 = h.putQueue[1]!;
  const read1 = h.readQueue[1] ?? h.enqueueRead();
  put1.resolve();
  await tick();
  read1.resolve(snapshot({ model: "a/m1", effort: "medium" }));
  await Promise.all([first, second]);
});

test("2. pending patches coalesce latest-field-wins with exact payloads", async () => {
  const h = createHarness();
  const put0 = h.enqueuePut();
  const read0 = h.enqueueRead();

  const p0 = h.controller.save({ model: "p/a", effort: "low" });
  await tick();
  const p1 = h.controller.save({ effort: "high" });
  const p2 = h.controller.save({ model: "p/b", multiAgentGuidanceEnabled: true });
  await tick();

  assert.deepEqual(h.puts[0], { model: "p/a", effort: "low" });

  put0.resolve();
  await tick();
  read0.resolve(snapshot({ model: "p/a", effort: "low" }));
  await tick();

  assert.deepEqual(h.puts[1], {
    effort: "high",
    model: "p/b",
    multiAgentGuidanceEnabled: true,
  });
  const put1 = h.putQueue[1]!;
  const read1 = h.readQueue[1] ?? h.enqueueRead();
  put1.resolve();
  await tick();
  read1.resolve(snapshot({ model: "p/b", effort: "medium", guidanceEnabled: true }));
  await Promise.all([p0, p1, p2]);
});

test("3. in-flight PUT body is not rewritten by later patches", async () => {
  const h = createHarness();
  const put0 = h.enqueuePut();
  const read0 = h.enqueueRead();

  const p0 = h.controller.save({ model: "in-flight" });
  await tick();
  assert.deepEqual(h.puts[0], { model: "in-flight" });

  void h.controller.save({ model: "later" });
  await tick();
  assert.deepEqual(h.puts[0], { model: "in-flight" }, "first PUT body stays frozen");
  assert.equal(h.puts.length, 1);

  put0.resolve();
  await tick();
  read0.resolve(snapshot({ model: "in-flight" }));
  await tick();
  assert.deepEqual(h.puts[1], { model: "later" });
  const put1 = h.putQueue[1]!;
  const read1 = h.readQueue[1] ?? h.enqueueRead();
  put1.resolve();
  await tick();
  read1.resolve(snapshot({ model: "later" }));
  await p0;
});

test("4. successful PUT publishes reload snapshot (not submitted patch); clamps effort high→medium", async () => {
  const h = createHarness();
  const put0 = h.enqueuePut();
  const read0 = h.enqueueRead();

  const done = h.controller.save({ model: "x/y", effort: "high" });
  await tick();
  put0.resolve();
  await tick();
  read0.resolve(snapshot({ model: "x/y", effort: "medium", efforts: ["low", "medium"] }));
  await done;

  assert.equal(h.published.length, 1);
  assert.equal(h.published[0]?.effort, "medium");
  assert.equal(h.published[0]?.model, "x/y");
  assert.notDeepEqual(h.published[0], h.puts[0]);
});

test("5. sequence PUT A → reload A → PUT B → reload B (do not skip reload)", async () => {
  const events: string[] = [];
  let putN = 0;
  let readN = 0;
  const putGates = [deferred<void>(), deferred<void>()];
  const readGates = [deferred<DelegationMutationSnapshot>(), deferred<DelegationMutationSnapshot>()];

  const transport: DelegationMutationTransport = {
    put: async (patch) => {
      const i = putN++;
      events.push(`put:${JSON.stringify(patch)}`);
      await putGates[i]!.promise;
    },
    readAuthority: async () => {
      const i = readN++;
      events.push(`read:${i}`);
      return readGates[i]!.promise;
    },
    publish: (snap) => {
      events.push(`publish:${snap.model}`);
    },
    setSaving: () => {},
  };
  const controller = createDelegationMutationController(transport);

  const a = controller.save({ model: "A" });
  await tick();
  void controller.save({ model: "B" });
  await tick();

  putGates[0]!.resolve();
  await tick();
  assert.deepEqual(events, ['put:{"model":"A"}', "read:0"]);
  readGates[0]!.resolve(snapshot({ model: "A" }));
  await tick();
  assert.ok(events.includes("publish:A"));
  assert.ok(events.includes('put:{"model":"B"}'));
  putGates[1]!.resolve();
  await tick();
  assert.ok(events.includes("read:1"));
  readGates[1]!.resolve(snapshot({ model: "B" }));
  await a;
  assert.deepEqual(events, [
    'put:{"model":"A"}',
    "read:0",
    "publish:A",
    'put:{"model":"B"}',
    "read:1",
    "publish:B",
  ]);
});

test("6. publish goes through transport.publish (production → setClientResourceData)", async () => {
  const published: DelegationMutationSnapshot[] = [];
  const put0 = deferred<void>();
  const read0 = deferred<DelegationMutationSnapshot>();
  const transport: DelegationMutationTransport = {
    put: async () => put0.promise,
    readAuthority: async () => read0.promise,
    publish: (s) => published.push(s),
    setSaving: () => {},
  };
  const controller = createDelegationMutationController(transport);
  const done = controller.save({ model: "pub" });
  await tick();
  put0.resolve();
  await tick();
  const snap = snapshot({ model: "pub-server" });
  read0.resolve(snap);
  await done;
  assert.equal(published.length, 1);
  assert.equal(published[0], snap);
});

test("7. PUT failure: no synthetic publish; last committed kept; saving cleared; not permanently busy", async () => {
  const published: DelegationMutationSnapshot[] = [];
  const savingLog: boolean[] = [];
  let fail = true;
  const transport: DelegationMutationTransport = {
    put: async () => {
      if (fail) throw new Error("injection save failed");
    },
    readAuthority: async () => snapshot({ model: "should-not-read" }),
    publish: (s) => published.push(s),
    setSaving: (v) => savingLog.push(v),
  };
  const controller = createDelegationMutationController(transport);

  await controller.save({ model: "bad" });
  assert.equal(published.length, 0);
  assert.deepEqual(savingLog, [true, false]);

  fail = false;
  let putSeen = false;
  transport.put = async () => {
    putSeen = true;
  };
  transport.readAuthority = async () => snapshot({ model: "recovered" });
  await controller.save({ model: "ok" });
  assert.equal(putSeen, true);
  assert.equal(published.at(-1)?.model, "recovered");
});

test("8. pending-on-failure: coalesced pending is dropped when the loop exits", async () => {
  const puts: DelegationPatch[] = [];
  let shouldFail = true;
  const putGate = deferred<void>();
  const transport: DelegationMutationTransport = {
    put: async (patch) => {
      puts.push(structuredClone(patch));
      if (shouldFail) {
        await putGate.promise;
        throw new Error("fail");
      }
    },
    readAuthority: async () => snapshot({ model: "ok" }),
    publish: () => {},
    setSaving: () => {},
  };
  const controller = createDelegationMutationController(transport);

  const first = controller.save({ model: "A" });
  await tick();
  void controller.save({ model: "pending-B" });
  await tick();
  putGate.resolve();
  await first;

  shouldFail = false;
  await controller.save({ model: "C" });
  assert.deepEqual(puts, [{ model: "A" }, { model: "C" }], "pending-B must be dropped on failure");
});

test("9. reload() uses the same authority path; failure preserves last committed", async () => {
  const published: DelegationMutationSnapshot[] = [];
  let reads = 0;
  const transport: DelegationMutationTransport = {
    put: async () => {},
    readAuthority: async () => {
      reads += 1;
      if (reads === 2) throw new Error("reload failed");
      return snapshot({ model: `r${reads}` });
    },
    publish: (s) => published.push(s),
    setSaving: () => {},
  };
  const controller = createDelegationMutationController(transport);

  await controller.reload();
  assert.equal(published.at(-1)?.model, "r1");
  await controller.reload();
  assert.equal(published.length, 1, "failed reload must not publish");
  assert.equal(published[0]?.model, "r1");
});

test("10. saving true before first PUT, stays true across coalesced follow-ups, false only when settled", async () => {
  const savingLog: boolean[] = [];
  const putGates = [deferred<void>(), deferred<void>()];
  const readGates = [deferred<DelegationMutationSnapshot>(), deferred<DelegationMutationSnapshot>()];
  let putN = 0;
  let readN = 0;
  const transport: DelegationMutationTransport = {
    put: async () => {
      const i = putN++;
      await putGates[i]!.promise;
    },
    readAuthority: async () => {
      const i = readN++;
      return readGates[i]!.promise;
    },
    publish: () => {},
    setSaving: (v) => savingLog.push(v),
  };
  const controller = createDelegationMutationController(transport);

  const done = controller.save({ model: "A" });
  await tick();
  assert.deepEqual(savingLog, [true]);
  void controller.save({ model: "B" });
  await tick();
  assert.deepEqual(savingLog, [true], "still saving across coalesce");

  putGates[0]!.resolve();
  await tick();
  readGates[0]!.resolve(snapshot({ model: "A" }));
  await tick();
  assert.deepEqual(savingLog, [true], "still saving during follow-up PUT");

  putGates[1]!.resolve();
  await tick();
  readGates[1]!.resolve(snapshot({ model: "B" }));
  await done;
  assert.deepEqual(savingLog, [true, false]);
});
