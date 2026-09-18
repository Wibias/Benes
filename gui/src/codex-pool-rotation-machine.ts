/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useReducer, useRef } from "react";
import {
  DEFAULT_ACCOUNT_POOL_RESET_ORDER,
  DEFAULT_ACCOUNT_POOL_STRATEGY,
  DEFAULT_ACCOUNT_POOL_STICKY_LIMIT,
  normalizeAccountPoolResetOrder,
  normalizeAccountPoolStickyLimit,
  normalizeAccountPoolStrategy,
  parseAccountPoolStickyLimitDraft,
  putCodexPoolStrategy,
  type AccountPoolResetOrder,
  type AccountPoolStrategy,
} from "./account-pool-strategy.ts";
import type {
  CodexAccountLoadObserver,
  CodexAccountLoadObserver as PoolLoadObserver,
} from "./codex-account-pool-domain.ts";

/** The three server-owned rotation settings, as one value. */
export interface PoolRotation {
  strategy: AccountPoolStrategy;
  stickyLimit: number;
  resetOrder: AccountPoolResetOrder;
}

/** Defaults paint immediately so the controls never appear after a layout jump. */
export const DEFAULT_POOL_ROTATION: PoolRotation = {
  strategy: DEFAULT_ACCOUNT_POOL_STRATEGY,
  stickyLimit: DEFAULT_ACCOUNT_POOL_STICKY_LIMIT,
  resetOrder: DEFAULT_ACCOUNT_POOL_RESET_ORDER,
};

/**
 * Read the rotation fields out of an `/active` payload.
 *
 * Returns null when the payload carries none of them, so an unrelated or empty response
 * cannot silently reset the operator's settings to defaults.
 */
export function rotationFromActive(value: unknown): PoolRotation | null {
  if (!value || typeof value !== "object") return null;
  const row = value as Record<string, unknown>;
  const present = "accountPoolStrategy" in row
    || "accountPoolStickyLimit" in row
    || "accountPoolResetOrder" in row;
  if (!present) return null;
  return {
    strategy: normalizeAccountPoolStrategy(row.accountPoolStrategy),
    stickyLimit: normalizeAccountPoolStickyLimit(row.accountPoolStickyLimit),
    resetOrder: normalizeAccountPoolResetOrder(row.accountPoolResetOrder),
  };
}

/** The one error slot the card shows; the copy comes from the caller's i18n. */
export type PoolRotationError = "invalid-sticky" | "write-failed";

/**
 * Why the two layers exist: `confirmed` is what the server last accepted and `visible` is
 * what the controls render. They differ only between a selection and the write's outcome,
 * which is what lets a rejected write roll back without a second source of truth.
 */
export interface PoolRotationState {
  confirmed: PoolRotation;
  visible: PoolRotation;
  /** Raw text in the sticky-limit input; not a number until it is committed. */
  stickyDraft: string;
  /** True once an authoritative read has landed. */
  hydrated: boolean;
  /** True while a write is in flight. */
  saving: boolean;
  /** The feeding read failed before any success. */
  readFailed: boolean;
  /** The current failure to show, cleared by a successful read or a new write. */
  error: PoolRotationError | null;
  /** Bumped when a read starts and when a write starts; reads carry their start value. */
  revision: number;
  /** A read landed mid-write; the caller re-reads once the write settles. */
  heldRead: boolean;
}

export const INITIAL_POOL_ROTATION_STATE: PoolRotationState = {
  confirmed: DEFAULT_POOL_ROTATION,
  visible: DEFAULT_POOL_ROTATION,
  stickyDraft: String(DEFAULT_POOL_ROTATION.stickyLimit),
  hydrated: false,
  saving: false,
  readFailed: false,
  error: null,
  revision: 0,
  heldRead: false,
};

export type PoolRotationEvent =
  | { type: "server-values-arrived"; rotation: PoolRotation | null; startedAtRevision: number }
  | { type: "server-read-failed" }
  | { type: "strategy-chosen"; strategy: AccountPoolStrategy }
  | { type: "reset-order-chosen"; resetOrder: AccountPoolResetOrder }
  | { type: "sticky-draft-typed"; text: string }
  | { type: "sticky-draft-rejected" }
  | { type: "write-started" }
  | { type: "write-accepted"; rotation: PoolRotation }
  | { type: "write-rejected" }
  | { type: "held-read-consumed" };

/** The controls stay inert until an authoritative read confirms them. */
export function controlsLocked(state: PoolRotationState): boolean {
  return state.saving || state.readFailed || !state.hydrated;
}

/** Adopt server values as both confirmed and visible. */
function adopt(state: PoolRotationState, rotation: PoolRotation): PoolRotationState {
  return {
    ...state,
    confirmed: rotation,
    visible: rotation,
    stickyDraft: String(rotation.stickyLimit),
    hydrated: true,
    readFailed: false,
    error: null,
  };
}

/**
 * The rotation state machine.
 *
 * A read that started before the current revision is dropped rather than applied: a saved
 * write bumps the revision, so the GET issued before it cannot undo the accepted values.
 * A read that lands while a write is in flight is held, not dropped, so the caller can
 * re-read exactly once afterwards instead of losing the server's view for a whole poll.
 */
export function poolRotationReducer(
  state: PoolRotationState,
  event: PoolRotationEvent,
): PoolRotationState {
  switch (event.type) {
    case "server-values-arrived": {
      if (event.startedAtRevision !== state.revision) return state;
      if (!event.rotation) return state;
      if (state.saving) return { ...state, heldRead: true };
      return adopt(state, event.rotation);
    }
    case "server-read-failed": {
      // Only the cold path is a failure the operator can act on; a later miss must not
      // replace working controls with a retry button.
      return state.hydrated ? state : { ...state, readFailed: true };
    }
    case "strategy-chosen": {
      return { ...state, visible: { ...state.visible, strategy: event.strategy } };
    }
    case "reset-order-chosen": {
      return { ...state, visible: { ...state.visible, resetOrder: event.resetOrder } };
    }
    case "sticky-draft-typed": {
      return { ...state, stickyDraft: event.text };
    }
    case "sticky-draft-rejected": {
      return { ...state, stickyDraft: String(state.visible.stickyLimit), error: "invalid-sticky" };
    }
    case "write-started": {
      if (state.saving) return state;
      return { ...state, saving: true, error: null, revision: state.revision + 1 };
    }
    case "write-accepted": {
      return { ...adopt(state, event.rotation), saving: false, revision: state.revision + 1 };
    }
    case "write-rejected": {
      return {
        ...state,
        visible: state.confirmed,
        stickyDraft: String(state.confirmed.stickyLimit),
        saving: false,
        error: "write-failed",
        revision: state.revision + 1,
      };
    }
    case "held-read-consumed": {
      return state.heldRead ? { ...state, heldRead: false } : state;
    }
    default:
      return state;
  }
}

/**
 * What a sticky-limit commit should do, decided without touching state.
 *
 * An unparseable draft is not a value: the input goes back to the confirmed limit and the
 * caller shows the invalid-limit copy. An unchanged value only normalizes the text.
 */
export type StickyCommit =
  | { kind: "invalid" }
  | { kind: "rewrite-draft"; text: string }
  | { kind: "write"; rotation: PoolRotation };

export function planStickyCommit(state: PoolRotationState, text: string): StickyCommit {
  const parsed = parseAccountPoolStickyLimitDraft(text ?? state.stickyDraft);
  if (parsed === null) return { kind: "invalid" };
  if (parsed === state.visible.stickyLimit) return { kind: "rewrite-draft", text: String(parsed) };
  return { kind: "write", rotation: { ...state.visible, stickyLimit: parsed } };
}

/** The visible rotation with a patch applied, for selection handlers. */
export function rotationWith(state: PoolRotationState, patch: Partial<PoolRotation>): PoolRotation {
  return { ...state.visible, ...patch };
}

/** Copy keys the caller resolves with `useT()`; the machine never holds translated text. */
export type PoolRotationErrorKey = "accountPool.strategyUpdateFailed" | "accountPool.stickyLimitInvalid";

export function errorKeyFor(state: PoolRotationState): PoolRotationErrorKey | null {
  if (state.error === "write-failed") return "accountPool.strategyUpdateFailed";
  if (state.error === "invalid-sticky") return "accountPool.stickyLimitInvalid";
  return null;
}

/** Everything the controls need, plus the intent callbacks the presentation layer wires up. */
export interface PoolRotationBinding {
  visible: PoolRotation;
  stickyDraft: string;
  saving: boolean;
  connected: boolean;
  locked: boolean;
  readFailed: boolean;
  errorKey: PoolRotationErrorKey | null;
  reload(): void;
  chooseStrategy(strategy: AccountPoolStrategy): void;
  chooseResetOrder(resetOrder: AccountPoolResetOrder): void;
  typeSticky(text: string): void;
  commitSticky(text?: string): void;
}

export interface PoolRotationSource {
  apiBase: string;
  subscribeLoadObserver?: (observer: CodexAccountLoadObserver) => () => void;
  readLastActive?: () => unknown;
  onStrategyResolved?: (strategy: AccountPoolStrategy) => void;
}

/**
 * Bind the rotation machine to one listener: adopt `/active` reads, publish selections and
 * commits, and re-read once when a read arrived during a write.
 *
 * Preferred source is the shared pool observer — it delivers the payload the pool already
 * fetched, so the card costs no extra round-trip. The card's own GET is only the fallback
 * for callers that wire no observer.
 */
export function usePoolRotation(source: PoolRotationSource): PoolRotationBinding {
  const { apiBase, subscribeLoadObserver, readLastActive, onStrategyResolved } = source;
  const [machine, dispatch] = useReducer(poolRotationReducer, INITIAL_POOL_ROTATION_STATE);
  // Async completions must read the newest revision, so the machine is mirrored into a ref.
  // The mirror is written by an effect rather than during render: a render that React
  // discards must not leave the ref describing state that never committed.
  const machineRef = useRef(machine);
  useEffect(() => { machineRef.current = machine; }, [machine]);

  const adopt = useCallback((value: unknown, startedAtRevision: number) => {
    const rotation = rotationFromActive(value);
    if (!rotation) return;
    dispatch({ type: "server-values-arrived", rotation, startedAtRevision });
    onStrategyResolved?.(rotation.strategy);
  }, [onStrategyResolved]);

  const reload = useCallback(() => {
    const startedAtRevision = machineRef.current.revision;
    void (async () => {
      try {
        const response = await fetch(`${apiBase}/api/codex-auth/active`);
        if (!response.ok) throw new Error("pool rotation load failed");
        adopt(await response.json(), startedAtRevision);
      } catch {
        dispatch({ type: "server-read-failed" });
      }
    })();
  }, [adopt, apiBase]);

  useEffect(() => {
    if (!subscribeLoadObserver) return;
    const observer: PoolLoadObserver = {
      beginActiveRead: () => machineRef.current.revision,
      acceptActiveRead: (value, startedRevision) => { adopt(value, startedRevision); },
      rejectActiveRead: () => { dispatch({ type: "server-read-failed" }); },
    };
    const unsubscribe = subscribeLoadObserver(observer);
    // Late mount: seed from the pool's last payload instead of waiting a whole poll.
    adopt(readLastActive?.(), machineRef.current.revision);
    return unsubscribe;
  }, [adopt, readLastActive, subscribeLoadObserver]);

  useEffect(() => {
    if (subscribeLoadObserver || !readLastActive) return;
    adopt(readLastActive(), machineRef.current.revision);
  }, [adopt, readLastActive, subscribeLoadObserver]);

  // Standalone fallback: no shared observer was wired, so this card owns the read.
  useEffect(() => {
    if (subscribeLoadObserver) return;
    // Not a synchronous cascade: the request resolves before the dispatch lands.
    // eslint-disable-next-line react-hooks/set-state-in-effect, react/react-compiler
    reload();
  }, [reload, subscribeLoadObserver]);

  /** A read that arrived during the write is replayed exactly once, after it settles. */
  useEffect(() => {
    if (!machine.heldRead || machine.saving) return;
    dispatch({ type: "held-read-consumed" });
    queueMicrotask(() => { reload(); });
  }, [machine.heldRead, machine.saving, reload]);

  const write = useCallback((rotation: PoolRotation) => {
    if (machineRef.current.saving) return;
    const confirmed = machineRef.current.confirmed;
    dispatch({ type: "write-started" });
    void (async () => {
      const result = await putCodexPoolStrategy(apiBase, rotation);
      if (result.ok) {
        dispatch({
          type: "write-accepted",
          rotation: {
            strategy: result.strategy,
            stickyLimit: result.stickyLimit,
            resetOrder: result.resetOrder,
          },
        });
        onStrategyResolved?.(result.strategy);
        return;
      }
      dispatch({ type: "write-rejected" });
      // The server kept the confirmed values, so the parent's label follows the controls
      // back instead of keeping the value the server refused.
      onStrategyResolved?.(confirmed.strategy);
    })();
  }, [apiBase, onStrategyResolved]);

  return {
    visible: machine.visible,
    stickyDraft: machine.stickyDraft,
    saving: machine.saving,
    connected: machine.hydrated,
    locked: controlsLocked(machine),
    readFailed: machine.readFailed,
    errorKey: errorKeyFor(machine),
    reload,
    chooseStrategy: strategy => {
      if (controlsLocked(machine) || strategy === machine.visible.strategy) return;
      dispatch({ type: "strategy-chosen", strategy });
      // The parent's label follows the selection immediately; a rejected write reports the
      // restored value.
      onStrategyResolved?.(strategy);
      write({ ...machine.visible, strategy });
    },
    chooseResetOrder: resetOrder => {
      if (controlsLocked(machine) || resetOrder === machine.visible.resetOrder) return;
      dispatch({ type: "reset-order-chosen", resetOrder });
      write({ ...machine.visible, resetOrder });
    },
    typeSticky: text => { dispatch({ type: "sticky-draft-typed", text }); },
    commitSticky: text => {
      if (controlsLocked(machine)) return;
      const plan = planStickyCommit(machine, text ?? machine.stickyDraft);
      if (plan.kind === "invalid") {
        dispatch({ type: "sticky-draft-rejected" });
        return;
      }
      if (plan.kind === "rewrite-draft") {
        dispatch({ type: "sticky-draft-typed", text: plan.text });
        return;
      }
      write(plan.rotation);
    },
  };
}




