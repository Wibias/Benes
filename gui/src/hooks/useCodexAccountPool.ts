/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { createBoundedFetch } from "../bounded-fetch";
import { startVisibilityPoll } from "../visibility-poll";
import {
  BUSY_RESULT,
  LOAD_TIMEOUT_MS,
  MAIN_ACCOUNT_ID,
  QUOTA_FILL_DELAYS_MS,
  REQUEST_RESULT,
  acceptObserverReads,
  beginObserverReads,
  createPoolApi,
  createPoolAuthority,
  normalizeAccountList,
  normalizeAccountRowList,
  planPauseAccepted,
  planPauseExhaustedAccepted,
  planSwitchAccepted,
  poolActiveNeedsReauth,
  readActiveId,
  readGoodPool,
  readRemovalCompletion,
  readStoredPriority,
  readThresholdFromActive,
  reconcileActiveRead,
  rejectObserverReads,
  rememberGoodPool,
  runPoolMutation,
  usePoolUsage,
  withPriority,
  withUsage,
  type CodexAccountEntry,
  type CodexAccountLoadObserver,
  type PauseToken,
  type PoolActivity,
  type PoolSnapshot,
} from "../codex-account-pool-domain.ts";
export type { CodexAccountLoadObserver } from "../codex-account-pool-domain.ts";

export type {
  CodexAccountActionResult,
  CodexAccountEntry,
  CodexAccountHealth,
  CodexAccountLoadState,
  PauseToken,
} from "../codex-account-pool-domain.ts";
/**
 * Codex account pool DATA layer.
 *
 * Ownership rule: exactly one caller instantiates this hook (`Providers.tsx`), and every
 * surface that shows Codex accounts reads the SAME controller. Mounting the pool twice
 * used to fork the state, so an action on one surface was invisible to the other.
 *
 * The hook owns list/active/loading plus the mutating actions. Modals, toasts, prompts
 * and popovers stay in the presentation layer. Domain normalization and reconciliation
 * live in `codex-account-pool-domain`; this file sequences I/O and owns the React state.
 */

/**
 * The pool controller every surface consumes. Derived from the implementation so the
 * published shape cannot drift from what the hook actually returns.
 */
export type CodexAccountPoolController = ReturnType<typeof useCodexAccountPool>;

const REFRESH_INTERVAL_MS = 30_000;


/**
 * The pool authority, allocated once per mount.
 *
 * It lives in a ref that callbacks mutate through `authorityRef.current`, so the sequencing
 * state stays outside React's render model and no render reads it.
 */
export function useCodexAccountPool(apiBase: string, enabled = true) {
  const [authoritySeed] = useState(createPoolAuthority);
  const authorityRef = useRef(authoritySeed);
  const api = useMemo(() => createPoolApi(apiBase), [apiBase]);
  const seed = readGoodPool(apiBase);

  const [snapshot, setSnapshot] = useState<PoolSnapshot>(() => ({
    accounts: seed ? normalizeAccountRowList(seed.accounts) : [],
    activeId: seed?.activeId ?? null,
    pinnedId: null,
    phase: seed != null ? "ready" : "loading",
    firstAttemptSettled: seed != null,
  }));
  const [activity, setActivity] = useState<PoolActivity>(() => ({
    inflight: 0,
    switchTarget: null,
    pause: null,
    priority: null,
    pauseLeases: 0,
  }));

  const usage30d = usePoolUsage(api, apiBase, enabled);

  const subscribeLoadObserver = useCallback((observer: CodexAccountLoadObserver) => {
    // One ref read per call keeps the sequencing mutations off the render path.
    const authority = authorityRef.current;
    authority.observers.add(observer);
    // Subscribing stays silent. `acceptActiveRead` means "a read that started at this
    // revision came back", and the strategy/auto-switch owners decide their editing and
    // saving disposition from that. Synthesising one on subscribe can overwrite an
    // in-flight draft or arm a spurious post-save refresh. Late surfaces seed themselves
    // from readLastThreshold()/readLastActive(), which apply only while uninitialized.
    return () => { authority.observers.delete(observer); };
  }, []);

  /** Last threshold an actual read returned, or undefined when none has succeeded yet. */
  const readLastThreshold = useCallback(() => {
    const authority = authorityRef.current;
    return readThresholdFromActive(authority.lastActive?.value);
  }, []);

  /** Full last /active payload, or undefined when none has succeeded yet. */
  const readLastActive = useCallback(() => authorityRef.current.lastActive?.value, []);

  const load = useCallback(async (refreshQuota = false): Promise<boolean> => {
    // One ref read per call keeps the sequencing mutations off the render path.
    const authority = authorityRef.current;
    const generation = ++authority.generation;
    // Bounded per attempt: a hung accounts/active read must settle, not pin the poll.
    const bounded = createBoundedFetch(LOAD_TIMEOUT_MS);
    setActivity(current => ({ ...current, inflight: current.inflight + 1 }));
    // The try opens immediately after the increment so even a synchronous throw in the
    // observer snapshot below cannot leave the counter stuck above zero.
    try {
      // Snapshot subscribers so an unsubscribe mid-flight cannot desync begin/accept pairs.
      const observerRead = beginObserverReads(authority.observers);
      // Soft refresh when boxes are already on screen — avoid a full-page loading flash.
      if (!refreshQuota && !authority.hasLoaded) {
        setSnapshot(current => (current.phase === "loading" ? current : { ...current, phase: "loading" }));
      }

      let paintedRows: CodexAccountEntry[] | null = null;
      let reconciledActive: string | null | undefined;

      const listTask = (async (): Promise<boolean> => {
        try {
          const response = await api.listAccounts(refreshQuota, bounded.signal);
          if (!response.ok) throw new Error("account load failed");
          if (authority.generation !== generation) return true;
          const rows = normalizeAccountList(response.payload);
          paintedRows = rows;
          authority.hasLoaded = true;
          // Progressive: paint account/quota boxes as soon as /accounts returns.
          setSnapshot(current => ({ ...current, accounts: rows, phase: "ready" }));
          return true;
        } catch {
          return false;
        }
      })();

      const activeReadTask = (async (): Promise<boolean> => {
        try {
          const response = await api.readActive(bounded.signal);
          if (!response.ok) throw new Error("active account load failed");
          const activePayload = response.payload;
          if (authority.generation !== generation) return true;
          const serverActiveId = readActiveId(activePayload) ?? null;
          const decision = reconcileActiveRead(serverActiveId, authority.pendingActive);
          if (decision.accept) {
            authority.pendingActive = null;
            reconciledActive = serverActiveId;
            setSnapshot(current => ({ ...current, activeId: serverActiveId }));
          }
          authority.lastActive = { value: activePayload };
          const pinnedId = (activePayload as { pinnedAccountId?: unknown }).pinnedAccountId;
          setSnapshot(current => ({
            ...current,
            pinnedId: typeof pinnedId === "string" ? pinnedId : null,
          }));
          acceptObserverReads(observerRead, activePayload);
          return true;
        } catch {
          if (authority.generation === generation) rejectObserverReads(observerRead);
          return false;
        }
      })();

      const loadOutcomes = await Promise.all([listTask, activeReadTask]);
      // A newer load already superseded this one; leave its state in place.
      if (authority.generation !== generation) return false;
      if (loadOutcomes[0]) {
        authority.hasLoaded = true;
        rememberGoodPool(apiBase, paintedRows, reconciledActive);
        return loadOutcomes[1];
      }
      // Cold failure only: after a successful load (including empty), keep rows and stay
      // ready so a soft poll miss does not flash the skeleton / wipe the pool.
      if (!authority.hasLoaded) {
        setSnapshot(current => (current.phase === "error" ? current : { ...current, phase: "error" }));
      }
      return false;
    } finally {
      bounded.clear();
      setActivity(current => ({ ...current, inflight: Math.max(0, current.inflight - 1) }));
      setSnapshot(current => (current.firstAttemptSettled ? current : { ...current, firstAttemptSettled: true }));
    }
  // oxlint-disable-next-line react/react-compiler -- preserve existing callback dependency semantics during Oxlint migration
  }, [api, apiBase]);

  // Mount load. Guarded per apiBase because StrictMode runs the effect twice, and the
  // deferred load is deliberately uncancellable — the request must always go out.
  useEffect(() => {
    // `enabled` is false for the inert instance a nested consumer still has to create
    // (hooks cannot be called conditionally). Without this guard, embedding the pool under
    // a page that already owns a controller would start a second poll loop.
    if (!enabled) return;
    const authority = authorityRef.current;
    if (authority.mountedFor === apiBase) return;
    authority.mountedFor = apiBase;
    queueMicrotask(() => { void load(); });
  }, [apiBase, enabled, load]);

  // Soft /accounts returns before background WHAM finishes. Re-poll cheaply until
  // credentialed rows have quota (or the user navigates away). Keyed on the boolean so
  // intermediate paints do not restart the chain.
  const needsQuotaTopUp = snapshot.accounts.some(row => row.hasCredential && !row.quota);
  useEffect(() => {
    const suspended = !enabled || !needsQuotaTopUp || activity.pauseLeases > 0;
    if (suspended) return;
    const pending = QUOTA_FILL_DELAYS_MS.map(delay => window.setTimeout(() => { void load(); }, delay));
    return () => {
      for (const timer of pending) window.clearTimeout(timer);
    };
  }, [activity.pauseLeases, enabled, load, needsQuotaTopUp]);

  // Background refresh, suspended while any pause lease is held — and fully paused
  // (no timer, no traffic) while the tab is hidden.
  useEffect(() => {
    const observing = enabled && activity.pauseLeases === 0;
    if (!observing) return;
    return startVisibilityPoll(() => { void load(); }, REFRESH_INTERVAL_MS);
  }, [activity.pauseLeases, enabled, load]);

  const pauseRefresh = useCallback((): PauseToken => {
    // One ref read per call keeps the sequencing mutations off the render path.
    const authority = authorityRef.current;
    const token = {} as PauseToken;
    authority.pauseTokens.add(token);
    setActivity(current => ({ ...current, pauseLeases: authority.pauseTokens.size }));
    return token;
  }, []);

  const resumeRefresh = useCallback((token: PauseToken) => {
    // One ref read per call keeps the sequencing mutations off the render path.
    const authority = authorityRef.current;
    if (!authority.pauseTokens.delete(token)) return;
    setActivity(current => ({ ...current, pauseLeases: authority.pauseTokens.size }));
  }, []);

  const switchAccount = useCallback(async (id: string | null) => {
    // One ref read per call keeps the sequencing mutations off the render path.
    const authority = authorityRef.current;
    const target = id ?? MAIN_ACCOUNT_ID;
    // Cross-gated with the order write, not just with itself: both PUTs move the pin, and
    // in opposite directions, so letting them overlap lets the client settle on the inverse
    // of the server's final pin until a reload happens to correct it.
    const outcome = await runPoolMutation(
      () => {
        if (authority.switchTarget !== null || authority.priority !== null) return false;
        authority.switchTarget = target;
        setActivity(current => ({ ...current, switchTarget: target }));
        return true;
      },
      () => {
        authority.switchTarget = null;
        setActivity(current => ({ ...current, switchTarget: null }));
      },
      async () => {
        const response = await api.pinAccount(id);
        return { ok: response.ok, payload: response.payload };
      },
    );
    if (outcome.status === "busy") return BUSY_RESULT;
    if (outcome.status === "rejected") return REQUEST_RESULT;
    const selected = planSwitchAccepted(outcome.payload, id);
    authority.pendingActive = { id: selected.activeId };
    // A manual selection pins its target until the account drains or routing moves off it.
    // The badge follows the id, not /active's `pinned` boolean, so a same-tier sibling's
    // turn does not make the operator's choice look released.
    setSnapshot(current => ({
      ...current,
      activeId: selected.activeId,
      pinnedId: selected.pinnedId,
    }));
    // Reconcile in the background: the switch is already accepted upstream, so a slow
    // reload must not hold the caller's confirmation dialog open.
    void load();
    return { ok: true, activeId: selected.activeId } as const;
  }, [api, load]);

  const saveAlias = useCallback(async (id: string, alias: string) => {
    const outcome = await runPoolMutation(
      () => true,
      () => {},
      async () => {
        const response = await api.setAlias(id, alias.trim());
        return { ok: response.ok, payload: null };
      },
    );
    if (outcome.status !== "accepted") return REQUEST_RESULT;
    await load();
    return { ok: true } as const;
  }, [api, load]);

  const setAccountPaused = useCallback(async (id: string, paused: boolean) => {
    // One ref read per call keeps the sequencing mutations off the render path.
    const authority = authorityRef.current;
    const outcome = await runPoolMutation(
      () => {
        if (authority.pause !== null) return false;
        authority.pause = { kind: "one", id };
        setActivity(current => ({ ...current, pause: { kind: "one", id } }));
        return true;
      },
      () => {
        authority.pause = null;
        setActivity(current => ({ ...current, pause: null }));
      },
      async () => {
        const response = await api.setPaused(id, paused);
        return { ok: response.ok, payload: response.payload };
      },
    );
    if (outcome.status === "busy") return BUSY_RESULT;
    if (outcome.status === "rejected") return REQUEST_RESULT;
    const applied = planPauseAccepted(outcome.payload, id, paused);
    setSnapshot(current => ({ ...current, accounts: applied.accounts(current.accounts) }));
    const pauseActiveId = applied.activeId;
    if (pauseActiveId !== undefined) {
      authority.pendingActive = { id: pauseActiveId };
      setSnapshot(current => ({ ...current, activeId: pauseActiveId }));
    }
    // Deliberately NOT cross-gated against the switch and order writes, even though pausing
    // the pinned account also releases the pin. This edge is conditional on the pin still
    // naming `id`, which makes it order-robust: whichever response lands last, the client
    // agrees with the server.
    if (paused) {
      setSnapshot(current => (current.pinnedId === id ? { ...current, pinnedId: null } : current));
    }
    void load();
    return { ok: true } as const;
  }, [api, load]);

  const setAccountPriority = useCallback(async (id: string, priority: number | null) => {
    // One ref read per call keeps the sequencing mutations off the render path.
    const authority = authorityRef.current;
    // The other half of the cross-gate in switchAccount: an order write clears the pin a
    // switch sets, so the two cannot be in flight together.
    const outcome = await runPoolMutation(
      () => {
        if (authority.priority !== null || authority.switchTarget !== null) return false;
        authority.priority = { id };
        setActivity(current => ({ ...current, priority: { id } }));
        return true;
      },
      () => {
        authority.priority = null;
        setActivity(current => ({ ...current, priority: null }));
      },
      async () => {
        const response = await api.setPriority(id, priority);
        return { ok: response.ok, payload: response.payload };
      },
    );
    // A rejected write leaves the row alone, so the card snaps back to the last confirmed
    // order instead of showing a value the server never accepted.
    if (outcome.status === "busy") return BUSY_RESULT;
    if (outcome.status === "rejected") return REQUEST_RESULT;
    const stored = readStoredPriority(outcome.payload, priority);
    setSnapshot(current => ({ ...current, accounts: withPriority(current.accounts, id, stored) }));
    // Every accepted order write releases the manual pin, even when the numeric value was
    // unchanged. Do not depend on the follow-up read to hide the badge.
    //
    // A pending marker left over from an earlier switch has to go too: it exists to hold the
    // accepted account until a matching `/active` read arrives, and any read that disagrees
    // is treated as stale — which never clears the marker. This write is newer than the
    // switch, so the account the switch named is no longer the one routing has to agree
    // with. The switch's own in-flight reload cannot win the race either: the generation
    // check discards a response from any generation but the latest.
    authority.pendingActive = null;
    setSnapshot(current => ({ ...current, pinnedId: null }));
    void load();
    return { ok: true } as const;
  }, [api, load]);

  const pauseExhaustedAccounts = useCallback(async () => {
    // One ref read per call keeps the sequencing mutations off the render path.
    const authority = authorityRef.current;
    const outcome = await runPoolMutation(
      () => {
        if (authority.pause !== null) return false;
        authority.pause = { kind: "bulk" };
        setActivity(current => ({ ...current, pause: { kind: "bulk" } }));
        return true;
      },
      () => {
        authority.pause = null;
        setActivity(current => ({ ...current, pause: null }));
      },
      async () => {
        const response = await api.pauseExhausted();
        return { ok: response.ok, payload: response.payload };
      },
    );
    if (outcome.status === "busy") return BUSY_RESULT;
    if (outcome.status === "rejected") return REQUEST_RESULT;
    const bulk = planPauseExhaustedAccepted(outcome.payload);
    setSnapshot(current => ({ ...current, accounts: bulk.accounts(current.accounts) }));
    const bulkActiveId = bulk.activeId;
    if (bulkActiveId !== undefined) {
      authority.pendingActive = { id: bulkActiveId };
      setSnapshot(current => ({ ...current, activeId: bulkActiveId }));
    }
    // Conditional for the same reason as the single-account pause above: clearing outright
    // would race the switch and order writes.
    setSnapshot(current => (
      current.pinnedId !== null && bulk.pausedIds.has(current.pinnedId)
        ? { ...current, pinnedId: null }
        : current
    ));
    void load();
    return { ok: true, pausedCount: bulk.pausedCount } as const;
  }, [api, load]);

  const removeAccount = useCallback(async (id: string) => {
    const outcome = await runPoolMutation(
      () => true,
      () => {},
      async () => {
        const response = await api.remove(id);
        return { ok: response.ok, payload: response.payload };
      },
    );
    if (outcome.status !== "accepted") return REQUEST_RESULT;
    const completion = readRemovalCompletion(outcome.payload);
    await load();
    return { ok: true, ...completion } as const;
  }, [api, load]);

  const syncAfterAccountAdded = useCallback(async () => {
    const reloaded = await load();
    return reloaded ? ({ ok: true } as const) : ({ ok: false, reason: "reload" } as const);
  }, [load]);

  // Include health-only reauth so Providers overview attention matches the row CTAs.
  const activeNeedsReauth = poolActiveNeedsReauth(snapshot.accounts, snapshot.activeId);
  const accountsWithUsage = useMemo(
    () => withUsage(snapshot.accounts, usage30d.data),
    [snapshot.accounts, usage30d.data],
  );

  return {
    accounts: accountsWithUsage,
    activeId: snapshot.activeId,
    loadState: snapshot.phase,
    refreshing: activity.inflight > 0,
    initialLoading: !snapshot.firstAttemptSettled,
    switchingId: activity.switchTarget,
    pauseUpdatingId: activity.pause?.kind === "one" ? activity.pause.id : null,
    priorityUpdatingId: activity.priority?.id ?? null,
    pausingExhausted: activity.pause?.kind === "bulk",
    activeNeedsReauth,
    activePinnedId: snapshot.pinnedId,
    load,
    switchAccount,
    setAccountPaused,
    setAccountPriority,
    pauseExhaustedAccounts,
    saveAlias,
    removeAccount,
    syncAfterAccountAdded,
    pauseRefresh,
    resumeRefresh,
    subscribeLoadObserver,
    readLastThreshold,
    readLastActive,
  };
}

