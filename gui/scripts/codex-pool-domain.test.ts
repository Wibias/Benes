import assert from "node:assert/strict";
import test from "node:test";
import {
  BUSY_RESULT,
  MAIN_ACCOUNT_ID,
  acceptObserverReads,
  beginObserverReads,
  createPoolApi,
  normalizeAccountList,
  planPauseAccepted,
  planPauseExhaustedAccepted,
  planSwitchAccepted,
  poolActiveNeedsReauth,
  readActiveId,
  readGoodPool,
  readStoredPriority,
  reconcileActiveRead,
  rejectObserverReads,
  rememberGoodPool,
  runPoolMutation,
  withPausedFlag,
  withPausedIds,
  withPriority,
  withUsage,
  type CodexAccountEntry,
  type CodexAccountLoadObserver,
} from "../src/codex-account-pool-domain.ts";

function account(overrides: Partial<CodexAccountEntry> & { id: string }): CodexAccountEntry {
  return {
    email: `${overrides.id}@example.test`,
    isMain: false,
    paused: false,
    priority: 0,
    hasCredential: true,
    quota: null,
    ...overrides,
  };
}

test("normalizeAccountList normalizes order, quota and the app-login log label", () => {
  const rows = normalizeAccountList({
    accounts: [
      { id: "a", email: "a@x", isMain: false, paused: false, hasCredential: true, quota: undefined, priority: undefined },
      { id: MAIN_ACCOUNT_ID, email: "main@x", isMain: true, logLabel: "ignored", paused: false, hasCredential: true, quota: null, priority: 7 },
    ],
  });
  assert.equal(rows[0].priority, 0);
  assert.equal(rows[0].quota, null);
  assert.equal(rows[1].priority, 7);
  assert.equal(rows[1].logLabel, "main");
});

test("normalizeAccountList tolerates a malformed payload instead of throwing", () => {
  assert.deepEqual(normalizeAccountList(undefined), []);
  assert.deepEqual(normalizeAccountList({ accounts: "nope" }), []);
});

test("readActiveId separates an explicit null from an absent field", () => {
  assert.equal(readActiveId({ activeCodexAccountId: "a" }), "a");
  assert.equal(readActiveId({ activeCodexAccountId: null }), null);
  assert.equal(readActiveId({}), undefined);
  assert.equal(readActiveId(null), undefined);
});

test("reconcileActiveRead refuses a stale read while an accepted id is unreconciled", () => {
  const stale = reconcileActiveRead("other", { id: "chosen" });
  assert.equal(stale.accept, false);
  assert.deepEqual(stale.nextPending, { id: "chosen" });

  const agreed = reconcileActiveRead("chosen", { id: "chosen" });
  assert.equal(agreed.accept, true);
  assert.equal(agreed.nextPending, null);

  assert.equal(reconcileActiveRead("anything", null).accept, true);
});

test("planPauseAccepted reports the flag change and only a present active id", () => {
  const applied = planPauseAccepted({ activeCodexAccountId: "b" }, "a", true);
  assert.equal(applied.activeId, "b");
  assert.equal(applied.accounts([account({ id: "a" }), account({ id: "b" })])[0].paused, true);

  const silent = planPauseAccepted({}, "a", false);
  assert.equal(silent.activeId, undefined);
});

test("planPauseExhaustedAccepted falls back to the id count and reports its active id", () => {
  const applied = planPauseExhaustedAccepted({
    pausedAccountIds: ["a", "b"],
    activeCodexAccountId: "c",
  });
  assert.equal(applied.pausedCount, 2);
  assert.equal(applied.activeId, "c");
  const rows = applied.accounts([account({ id: "a" }), account({ id: "c" })]);
  assert.equal(rows[0].paused, true);
  assert.equal(rows[1].paused, false);

  assert.equal(planPauseExhaustedAccepted({ pausedCount: 5 }).pausedCount, 5);
  assert.equal(planPauseExhaustedAccepted({}).activeId, undefined);
});

test("row patches treat the __main__ sentinel as the app-login row", () => {
  const rows = [account({ id: "a" }), account({ id: MAIN_ACCOUNT_ID, isMain: true })];
  assert.deepEqual(withPausedFlag(rows, MAIN_ACCOUNT_ID, true).map(row => row.paused), [false, true]);
  assert.deepEqual(withPriority(rows, "a", 3).map(row => row.priority), [3, 0]);
  assert.deepEqual(withPausedIds(rows, new Set([MAIN_ACCOUNT_ID])).map(row => row.paused), [false, true]);
  assert.equal(withPausedIds(rows, new Set()), rows);
});

test("readStoredPriority prefers the stored value over the request", () => {
  assert.equal(readStoredPriority({ priority: 4 }, 9), 4);
  assert.equal(readStoredPriority({}, 9), 9);
  assert.equal(readStoredPriority({}, null), 0);
  // A stored value that is not an integer is not a priority at all; normalization floors it.
  assert.equal(readStoredPriority({ priority: "4" }, 9), 0);
});

test("withUsage attaches usage by log label and leaves unmatched rows untouched", () => {
  const rows = [account({ id: "a", logLabel: "label-a" }), account({ id: MAIN_ACCOUNT_ID, isMain: true })];
  const merged = withUsage(rows, {
    accounts: [{ accountLogLabel: "label-a", totalTokens: 5, usageCoverageRatio: 1 }],
  });
  assert.equal(merged[0].usage30d?.totalTokens, 5);
  assert.equal(merged[1].usage30d, undefined);
  assert.equal(withUsage(rows, undefined), rows);
});

test("poolActiveNeedsReauth ignores a paused account and falls back to the app-login row", () => {
  const healthy = account({ id: "a", health: { status: "healthy" } });
  const reauth = account({ id: "b", needsReauth: true });
  assert.equal(poolActiveNeedsReauth([healthy, reauth], "a"), false);
  assert.equal(poolActiveNeedsReauth([healthy, reauth], "b"), true);
  assert.equal(poolActiveNeedsReauth([reauth], null), false);
  assert.equal(poolActiveNeedsReauth([account({ id: MAIN_ACCOUNT_ID, isMain: true, needsReauth: true })], null), true);
  assert.equal(poolActiveNeedsReauth([account({ id: "b", needsReauth: true, paused: true })], "b"), false);
});

test("rememberGoodPool keeps rows on a soft failure but an empty success still counts", () => {
  const base = "http://pool-remember.test";
  assert.equal(readGoodPool(base), undefined);
  rememberGoodPool(base, [account({ id: "a" })], "a");
  assert.deepEqual(readGoodPool(base)?.accounts.map(row => row.id), ["a"]);
  assert.equal(readGoodPool(base)?.activeId, "a");
  // A failed accounts read passes null and an undefined active id: both prior values stay.
  rememberGoodPool(base, null, undefined);
  assert.deepEqual(readGoodPool(base)?.accounts.map(row => row.id), ["a"]);
  assert.equal(readGoodPool(base)?.activeId, "a");
  // A successful empty read is still a success: rows empty, the active id is replaced.
  rememberGoodPool(base, [], "b");
  assert.deepEqual(readGoodPool(base)?.accounts, []);
  assert.equal(readGoodPool(base)?.activeId, "b");
});

test("observer reads pair begin/accept per subscriber and survive a mid-flight unsubscribe", () => {
  const registry = new Set<CodexAccountLoadObserver>();
  const events: string[] = [];
  const first: CodexAccountLoadObserver = {
    beginActiveRead: () => { events.push("begin-1"); return 4; },
    acceptActiveRead: (value, revision) => { events.push(`accept-1:${revision}:${JSON.stringify(value)}`); },
    rejectActiveRead: () => { events.push("reject-1"); },
  };
  const second: CodexAccountLoadObserver = {
    beginActiveRead: () => { events.push("begin-2"); return 9; },
    acceptActiveRead: (_value, revision) => { events.push(`accept-2:${revision}`); },
    rejectActiveRead: () => { events.push("reject-2"); },
  };
  registry.add(first);
  registry.add(second);
  const read = beginObserverReads(registry);
  registry.delete(second);
  acceptObserverReads(read, { accountPoolStrategy: "fill-first" });
  assert.deepEqual(events, [
    "begin-1", "begin-2",
    'accept-1:4:{"accountPoolStrategy":"fill-first"}', "accept-2:9",
  ]);

  const seen: string[] = [];
  const failing = new Set<CodexAccountLoadObserver>([{
    beginActiveRead: () => 0,
    acceptActiveRead: () => { seen.push("accept"); },
    rejectActiveRead: () => { seen.push("reject"); },
  }]);
  rejectObserverReads(beginObserverReads(failing));
  assert.deepEqual(seen, ["reject"]);
});

test("runPoolMutation releases the gate on success, rejection and transport failure", async () => {
  let held = 0;
  const claim = () => { held += 1; return true; };
  const release = () => { held -= 1; };

  const accepted = await runPoolMutation(claim, release, async () => ({ ok: true, payload: { a: 1 } }));
  assert.deepEqual(accepted, { status: "accepted", payload: { a: 1 } });
  const rejected = await runPoolMutation(claim, release, async () => ({ ok: false, payload: null }));
  assert.equal(rejected.status, "rejected");
  const threw = await runPoolMutation(claim, release, async () => { throw new Error("offline"); });
  assert.equal(threw.status, "rejected");
  assert.equal(held, 0);
});

test("runPoolMutation refuses to start while the gate is held", async () => {
  let sent = 0;
  const busy = await runPoolMutation(
    () => false,
    () => { throw new Error("release must not run for a refused claim"); },
    async () => { sent += 1; return { ok: true, payload: null }; },
  );
  assert.equal(busy.status, "busy");
  assert.equal(sent, 0);
  assert.deepEqual(BUSY_RESULT, { ok: false, reason: "busy" });
});

test("a newer operation cannot be affected by an older one finishing later", async () => {
  const gate = { held: false };
  const committed: string[] = [];
  let releaseFirst: (() => void) | undefined;
  const first = runPoolMutation(
    () => { if (gate.held) return false; gate.held = true; return true; },
    () => { gate.held = false; },
    () => new Promise(resolve => {
      releaseFirst = () => { committed.push("first"); resolve({ ok: true, payload: null }); };
    }),
  );
  // A second mutation while the first is in flight is refused, so it cannot interleave.
  const second = await runPoolMutation(
    () => { if (gate.held) return false; gate.held = true; return true; },
    () => { gate.held = false; },
    async () => { committed.push("second"); return { ok: true, payload: null }; },
  );
  assert.equal(second.status, "busy");
  assert.deepEqual(committed, []);

  releaseFirst?.();
  assert.equal((await first).status, "accepted");
  // The first mutation settled and released the gate, so the next attempt may run.
  const third = await runPoolMutation(
    () => { if (gate.held) return false; gate.held = true; return true; },
    () => { gate.held = false; },
    async () => { committed.push("third"); return { ok: true, payload: null }; },
  );
  assert.equal(third.status, "accepted");
  assert.deepEqual(committed, ["first", "third"]);
});

test("createPoolApi keeps the documented endpoint, method and body shapes", async () => {
  const calls: { url: string; init: RequestInit }[] = [];
  const original = globalThis.fetch;
  globalThis.fetch = (async (url: string, init: RequestInit) => {
    calls.push({ url, init });
    return { ok: true, json: async () => ({ activeCodexAccountId: "a" }) } as unknown as Response;
  }) as typeof fetch;
  try {
    const api = createPoolApi("http://base.test");
    const signal = new AbortController().signal;
    await api.listAccounts(false, signal);
    await api.listAccounts(true, signal);
    await api.readActive(signal);
    await api.readUsage(signal);
    await api.pinAccount("a");
    await api.setPaused("a", true);
    await api.setPriority("a", 5);
    await api.setAlias("a", "label");
    await api.pauseExhausted();
    await api.remove("a/b");
  } finally {
    globalThis.fetch = original;
  }
  assert.deepEqual(calls.map(call => call.url), [
    "http://base.test/api/codex-auth/accounts",
    "http://base.test/api/codex-auth/accounts?refresh=1",
    "http://base.test/api/codex-auth/active",
    "http://base.test/api/usage?range=30d",
    "http://base.test/api/codex-auth/active",
    "http://base.test/api/codex-auth/accounts/pause",
    "http://base.test/api/codex-auth/accounts/priority",
    "http://base.test/api/codex-auth/accounts/alias",
    "http://base.test/api/codex-auth/accounts/pause-exhausted",
    "http://base.test/api/codex-auth/accounts?id=a%2Fb",
  ]);
  assert.equal(calls[4].init.method, "PUT");
  assert.deepEqual(calls[4].init.body, JSON.stringify({ accountId: "a" }));
  assert.deepEqual(calls[5].init.body, JSON.stringify({ id: "a", paused: true }));
  assert.equal(calls[9].init.method, "DELETE");
});


