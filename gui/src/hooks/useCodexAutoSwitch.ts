/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useReducer, useRef } from "react";
import {
  DEFAULT_AUTO_SWITCH_THRESHOLD,
  autoSwitchThresholdReadDisposition,
  extractAutoSwitchThresholdPayload,
  normalizeAutoSwitchThreshold,
  parseEnabledAutoSwitchThreshold,
  planAutoSwitchToggleWrite,
  putAutoSwitchThreshold,
} from "../codex-auto-switch.ts";
import type { AutoSwitchTogglePlan } from "../codex-auto-switch.ts";
import type { AutoSwitchFeedback } from "../lib/codex-auto-switch-policy.ts";

/** How long a save notice stays on screen. */
const NOTICE_VISIBLE_MS = 5000;

export interface CodexAutoSwitchController {
  /** Always a number — seeded with the default so the card never waits on /active. */
  threshold: number;
  draft: string;
  /** False until /active (or hydrate) confirms server state — blocks writes of the seed default. */
  hydrated: boolean;
  saving: boolean;
  loadError: boolean;
  feedback: AutoSwitchFeedback;
  beginServerRead(): number;
  acceptServerRead(value: unknown, startedRevision: number): void;
  hydrateServerValue(value: unknown): void;
  rejectServerRead(): void;
  setDraft(value: string): void;
  setEditing(editing: boolean): void;
  commit(): Promise<boolean>;
  cancel(): void;
  toggle(): Promise<boolean>;
  retry(): void;
}

export interface CodexAutoSwitchMessages {
  updated: string;
  updateFailed: string;
  invalid: string;
}

/**
 * Everything the card renders, plus the fields the async completions must read
 * synchronously. One record and one transition function, rather than several
 * independent setters that can disagree with each other.
 */
export interface AutoSwitchState {
  threshold: number;
  draft: string;
  lastEnabled: number;
  deferred: number | null;
  revision: number;
  hydrated: boolean;
  saving: boolean;
  loadError: boolean;
  editing: boolean;
  cancelledDraft: boolean;
  notice: AutoSwitchFeedback;
}

type AutoSwitchEvent =
  | { kind: "server-value"; value: number }
  | { kind: "server-value-held"; value: number }
  | { kind: "hold-flushed" }
  | { kind: "read-failed" }
  | { kind: "read-settled" }
  | { kind: "save-began" }
  | { kind: "save-accepted"; value: number }
  | { kind: "save-rejected"; previous: number }
  | { kind: "save-ended" }
  | { kind: "draft-changed"; text: string }
  | { kind: "editing-changed"; on: boolean }
  | { kind: "draft-cancelled" }
  | { kind: "draft-rejected" }
  | { kind: "draft-synced"; value: number }
  | { kind: "cancelled-flag-cleared" }
  | { kind: "toggle-applied"; plan: AutoSwitchTogglePlan }
  | { kind: "notice-raised"; message: string; error: boolean }
  | { kind: "notice-cleared" };

export function initialAutoSwitchState(): AutoSwitchState {
  return {
    threshold: DEFAULT_AUTO_SWITCH_THRESHOLD,
    draft: String(DEFAULT_AUTO_SWITCH_THRESHOLD),
    lastEnabled: DEFAULT_AUTO_SWITCH_THRESHOLD,
    deferred: null,
    revision: 0,
    hydrated: false,
    saving: false,
    loadError: false,
    editing: false,
    cancelledDraft: false,
    notice: null,
  };
}

/** Project a confirmed server threshold into the record, draft included. */
function confirmThreshold(state: AutoSwitchState, value: number): AutoSwitchState {
  const lastEnabled = value > 0 ? value : state.lastEnabled;
  return {
    ...state,
    threshold: value,
    lastEnabled,
    hydrated: true,
    draft: String(value > 0 ? value : lastEnabled),
  };
}

/** A read that arrives while editing or saving is held instead of applied. */
function holdOrConfirm(state: AutoSwitchState, value: number): AutoSwitchState {
  if (state.editing || state.saving) return { ...state, deferred: value };
  return { ...confirmThreshold(state, value), deferred: null };
}

/** Prefer a held value; otherwise restore the draft from the confirmed pair. */
function releaseHold(state: AutoSwitchState): AutoSwitchState {
  if (state.deferred !== null) return { ...confirmThreshold(state, state.deferred), deferred: null };
  return { ...state, draft: String(state.threshold > 0 ? state.threshold : state.lastEnabled) };
}

export function autoSwitchReducer(state: AutoSwitchState, event: AutoSwitchEvent): AutoSwitchState {
  switch (event.kind) {
    case "server-value":
      return holdOrConfirm(state, event.value);

    case "server-value-held":
      return { ...state, deferred: event.value };

    case "hold-flushed":
      return state.deferred === null ? state : { ...confirmThreshold(state, state.deferred), deferred: null };

    case "read-failed":
      return state.hydrated ? state : { ...state, loadError: true };

    case "read-settled":
      return state.loadError ? { ...state, loadError: false } : state;

    case "save-began":
      return { ...state, saving: true, editing: false, notice: null, revision: state.revision + 1 };

    case "save-accepted":
      return { ...confirmThreshold(state, event.value), deferred: null, revision: state.revision + 1 };

    case "save-rejected": {
      const bumped: AutoSwitchState = { ...state, revision: state.revision + 1 };
      if (bumped.deferred !== null) return { ...confirmThreshold(bumped, bumped.deferred), deferred: null };
      return confirmThreshold(bumped, event.previous);
    }

    case "save-ended":
      return { ...state, saving: false };

    case "draft-changed":
      return { ...state, editing: true, cancelledDraft: false, notice: null, draft: event.text };

    case "editing-changed":
      return { ...state, editing: event.on };

    case "draft-cancelled":
      return releaseHold({ ...state, editing: false, cancelledDraft: true, notice: null });

    case "draft-rejected":
      return releaseHold({ ...state, editing: false });

    case "draft-synced":
      return { ...state, draft: String(event.value) };

    case "cancelled-flag-cleared":
      return { ...state, cancelledDraft: false };

    case "toggle-applied":
      return {
        ...state,
        lastEnabled: event.plan.lastEnabled,
        draft: event.plan.threshold === 0 ? String(event.plan.lastEnabled) : state.draft,
      };

    case "notice-raised":
      return { ...state, notice: { tone: event.error ? "err" : "ok", message: event.message } };

    case "notice-cleared":
      return state.notice === null ? state : { ...state, notice: null };

    default:
      return state;
  }
}

/**
 * Auto-switch threshold settings for one surface.
 *
 * The policy (normalize, parse, plan, read disposition, PUT) is owned by
 * `codex-auto-switch`; this hook only sequences it. React state renders the card, and
 * a ref mirror of the same record gives the async completions a synchronous view — a
 * read that lands mid-edit must not consult a stale flag.
 */
export function useCodexAutoSwitch(
  apiBase: string,
  messages: CodexAutoSwitchMessages,
): CodexAutoSwitchController {
  const [snapshot, dispatch] = useReducer(autoSwitchReducer, undefined, initialAutoSwitchState);
  const stateRef = useRef(snapshot);
  const noticeTimerRef = useRef<number | null>(null);

  /** Advance the mirror synchronously, then let React render the same transition. */
  const send = useCallback((event: AutoSwitchEvent) => {
    stateRef.current = autoSwitchReducer(stateRef.current, event);
    dispatch(event);
  }, []);

  const clearNoticeTimer = useCallback(() => {
    if (noticeTimerRef.current !== null) {
      window.clearTimeout(noticeTimerRef.current);
      noticeTimerRef.current = null;
    }
  }, []);

  useEffect(() => () => {
    if (noticeTimerRef.current !== null) window.clearTimeout(noticeTimerRef.current);
  }, []);

  const announce = useCallback((message: string, error: boolean) => {
    clearNoticeTimer();
    send({ kind: "notice-raised", message, error });
    noticeTimerRef.current = window.setTimeout(() => {
      send({ kind: "notice-cleared" });
      noticeTimerRef.current = null;
    }, NOTICE_VISIBLE_MS);
  }, [clearNoticeTimer, send]);

  const beginServerRead = useCallback((): number => {
    send({ kind: "read-settled" });
    return stateRef.current.revision;
  }, [send]);

  const acceptServerRead = useCallback((value: unknown, startedRevision: number) => {
    const current = stateRef.current;
    const disposition = autoSwitchThresholdReadDisposition(
      current.editing,
      current.saving,
      startedRevision,
      current.revision,
    );
    if (disposition === "ignore") return;
    const incoming = normalizeAutoSwitchThreshold(extractAutoSwitchThresholdPayload(value));
    send({ kind: "read-settled" });
    send(disposition === "defer"
      ? { kind: "server-value-held", value: incoming }
      : { kind: "server-value", value: incoming });
  }, [send]);

  /**
   * Seed from a value another surface already fetched. Applies ONLY while
   * uninitialized, so it can never disturb a draft, a pending save, or a newer read.
   */
  const hydrateServerValue = useCallback((value: unknown) => {
    const current = stateRef.current;
    if (current.hydrated || current.editing || current.saving) return;
    const incoming = normalizeAutoSwitchThreshold(extractAutoSwitchThresholdPayload(value));
    send({ kind: "read-settled" });
    send({ kind: "server-value", value: incoming });
  }, [send]);

  const rejectServerRead = useCallback(() => {
    send({ kind: "read-failed" });
  }, [send]);

  /** One server write, shared by commit and toggle. */
  const write = useCallback(async (
    next: number,
    previous: number,
    announceSuccess: boolean,
  ): Promise<boolean> => {
    if (stateRef.current.saving) return false;
    clearNoticeTimer();
    send({ kind: "save-began" });
    try {
      const accepted = await putAutoSwitchThreshold(apiBase, next);
      if (accepted) {
        send({ kind: "save-accepted", value: next });
        if (announceSuccess) announce(messages.updated, false);
      } else {
        send({ kind: "save-rejected", previous });
        announce(messages.updateFailed, true);
      }
      return accepted;
    } finally {
      send({ kind: "save-ended" });
    }
  }, [announce, apiBase, clearNoticeTimer, messages.updateFailed, messages.updated, send]);

  const commit = useCallback(async (): Promise<boolean> => {
    const current = stateRef.current;
    // A cancelled draft already reverted; the next commit is a no-op that reports success.
    if (current.cancelledDraft) {
      send({ kind: "cancelled-flag-cleared" });
      return true;
    }
    // Never PUT the paint-time default before /active confirms the real value.
    if (!current.hydrated || current.saving) return false;
    send({ kind: "editing-changed", on: false });
    const parsed = parseEnabledAutoSwitchThreshold(current.draft);
    if (parsed === null) {
      announce(messages.invalid, true);
      send({ kind: "draft-rejected" });
      return false;
    }
    if (parsed === current.threshold) {
      const held = stateRef.current.deferred !== null;
      send({ kind: "hold-flushed" });
      if (!held) send({ kind: "draft-synced", value: parsed });
      return true;
    }
    return write(parsed, current.threshold, true);
  }, [announce, messages.invalid, send, write]);

  const toggle = useCallback(async (): Promise<boolean> => {
    const current = stateRef.current;
    if (!current.hydrated || current.saving) return false;
    send({ kind: "editing-changed", on: false });
    const plan = planAutoSwitchToggleWrite(current.threshold, current.draft, current.lastEnabled);
    const accepted = await write(plan.threshold, current.threshold, true);
    if (!accepted) return false;
    send({ kind: "toggle-applied", plan });
    return true;
  }, [send, write]);

  const setDraft = useCallback((value: string) => {
    if (!stateRef.current.hydrated) return;
    clearNoticeTimer();
    send({ kind: "draft-changed", text: value });
  }, [clearNoticeTimer, send]);

  const setEditing = useCallback((editing: boolean) => {
    send({ kind: "editing-changed", on: editing });
  }, [send]);

  const cancel = useCallback(() => {
    clearNoticeTimer();
    send({ kind: "draft-cancelled" });
  }, [clearNoticeTimer, send]);

  const retry = useCallback(() => {
    clearNoticeTimer();
    send({ kind: "read-settled" });
    send({ kind: "notice-cleared" });
  }, [clearNoticeTimer, send]);

  return {
    threshold: snapshot.threshold,
    draft: snapshot.draft,
    hydrated: snapshot.hydrated,
    saving: snapshot.saving,
    loadError: snapshot.loadError,
    feedback: snapshot.notice,
    beginServerRead,
    acceptServerRead,
    hydrateServerValue,
    rejectServerRead,
    setDraft,
    setEditing,
    commit,
    cancel,
    toggle,
    retry,
  };
}
