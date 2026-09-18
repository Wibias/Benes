/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useRef, useState } from "react";
import type { Dispatch } from "react";
import type {
  AddAccountEvent,
  AddAccountState,
  ManualCodeStatus,
} from "./add-codex-account-reducer";
import type { TFn } from "../i18n/shared";
import { readJsonIfOk, readJsonOrThrow } from "../fetch-json";
import { startVisibilityPoll } from "../visibility-poll";
import {
  codexAccountMutationCompletion,
  type CodexAccountMutationCompletion,
} from "../codex-account-mutation";
import {
  codexAuthLoginAccountId,
  codexAuthLoginRequestBody,
  codexAuthLoginStatusUrl,
  codexAuthManualCodeBlocked,
  decodeCodexAuthConflictStep,
  decodeCodexAuthLoginOpened,
  decodeCodexAuthManualCodeFailure,
  decodeCodexAuthPollCatch,
  decodeCodexAuthPollTick,
  decodeCodexAuthStartCatch,
  decodeCodexAuthTimeoutApplies,
  type CodexAuthPollDecision,
} from "../lib/codex-account-oauth-decode";

/** How often the login is asked whether the browser has finished. */
const STATUS_POLL_MS = 2000;

/** A single status read is abandoned after this; the next tick tries again. */
const STATUS_TICK_TIMEOUT_MS = 10_000;

/** How long the operator is given to finish in the browser before the attempt is dropped. */
const LOGIN_WINDOW_MS = 300_000;

/** What an authorization POST answers. */
type LoginResponse = { url?: string; flowId?: string; error?: string; status?: string };

/** The status document a poll tick reads. */
type LoginStatus = { status: string; error?: string; catalogRefreshPending?: unknown };

/**
 * The mutable half of one login attempt.
 *
 * None of this belongs in React state: the poll tick, the deadline and the cancel path all
 * have to read and write it from outside a render, and a render must never observe a
 * half-applied attempt. It used to be a handful of separate refs; it is one record now so
 * "what does an attempt own" has a single answer.
 */
interface LoginAttempt {
  /** False once the hook unmounts; every async continuation checks it before dispatching. */
  alive: boolean;
  /** The flow the listener is currently authorizing, or null when there is none. */
  flow: string | null;
  /** Mirrored manual-code phase, for the poll tick's synchronous read. */
  manualCodePhase: ManualCodeStatus;
  /** Consecutive failed status reads, to tell a blip from a listener that has gone away. */
  errorStreak: number;
  /** One status read at a time. */
  pollInFlight: boolean;
  /** Stops the visibility poll, when one is running. */
  stopPoll: (() => void) | null;
  /** The attempt's own deadline, when one is armed. */
  deadline: ReturnType<typeof setTimeout> | null;
  /** Aborts the status reads of the flow being polled. */
  pollAbort: AbortController | null;
  /** Aborts the login POST that is waiting on the browser. */
  loginAbort: AbortController | null;
  /** The modal's completion callback, bound once the modal has mounted. */
  onAdded: (completion: CodexAccountMutationCompletion) => void;
  /** The modal's close callback, so a finished flow can dismiss the dialog. */
  onClose: () => void;
}

function createLoginAttempt(): LoginAttempt {
  return {
    alive: true,
    flow: null,
    manualCodePhase: "idle",
    errorStreak: 0,
    pollInFlight: false,
    stopPoll: null,
    deadline: null,
    pollAbort: null,
    loginAbort: null,
    onAdded: () => {},
    onClose: () => {},
  };
}

/** Everything an attempt needs from the hook that owns it. */
interface AttemptContext {
  apiBase: string;
  reauthAccountId: string | undefined;
  attempt: LoginAttempt;
  dispatch: Dispatch<AddAccountEvent>;
  t: TFn;
}

/** Stop the poll, disarm the deadline, and abandon any status read still open. */
function haltPolling(attempt: LoginAttempt): void {
  attempt.stopPoll?.();
  attempt.stopPoll = null;
  if (attempt.deadline !== null) {
    clearTimeout(attempt.deadline);
    attempt.deadline = null;
  }
  attempt.pollAbort?.abort();
  attempt.pollAbort = null;
  attempt.pollInFlight = false;
}

/** Put the manual-code input back to idle and forget the retry streak. */
function resetManualCode(context: AttemptContext): void {
  context.dispatch({ kind: "manual-code-reset" });
  context.attempt.errorStreak = 0;
}

/** Tell the listener to drop a flow, ignoring the answer - there is nothing to do with it. */
async function abandonFlow(apiBase: string, body: unknown): Promise<void> {
  await fetch(`${apiBase}/api/codex-auth/login/cancel`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  }).catch(() => {});
}

/**
 * Abandon the attempt: no flow, no poll, no browser window.
 *
 * The flow id is read and cleared before the cancel goes out, so a second call cannot send
 * the same cancellation twice; the state transition lands first so the dialog stops showing
 * a URL that is already dead.
 */
async function cancelLogin(context: AttemptContext): Promise<void> {
  const { attempt, apiBase } = context;
  resetManualCode(context);
  const flow = attempt.flow;
  attempt.flow = null;
  context.dispatch({ kind: "authorization-closed" });
  haltPolling(attempt);
  attempt.loginAbort?.abort();
  attempt.loginAbort = null;
  if (flow === null) return;
  await abandonFlow(apiBase, { flowId: flow });
}

/**
 * Open a login, retrying once around a conflict.
 *
 * A 409 means a flow already exists on the listener, often one left behind by a dialog that
 * went away; the first cancel clears that. If the retry still conflicts, another caller
 * legitimately holds the flow and this attempt reports it instead of fighting for it. A
 * conflict that began before unmount is not worth retrying, so that case returns nothing.
 */
async function requestLogin(context: AttemptContext, requestedId: string | undefined): Promise<LoginResponse | null> {
  const { apiBase, attempt, dispatch, t, reauthAccountId } = context;
  const controller = new AbortController();
  attempt.loginAbort?.abort();
  attempt.loginAbort = controller;

  const post = () => fetch(`${apiBase}/api/codex-auth/login`, {
    signal: controller.signal,
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(codexAuthLoginRequestBody(reauthAccountId, requestedId)),
  });

  let response = await post();
  if (!attempt.alive) return null;

  if (response.status === 409) {
    await abandonFlow(apiBase, {});
    const afterCancel = decodeCodexAuthConflictStep({
      firstStatus: 409,
      alive: attempt.alive,
      aborted: controller.signal.aborted,
    });
    if (afterCancel.kind === "dead") return null;
    response = await post();
    const retry = decodeCodexAuthConflictStep({
      firstStatus: 409,
      alive: true,
      aborted: false,
      retryStatus: response.status,
    });
    if (retry.kind === "already-in-progress") {
      dispatch({ kind: "authorization-failed", error: t("codexAuth.oauthAlreadyInProgress") });
      return null;
    }
  }

  return await readJsonOrThrow<LoginResponse>(response, t("modal.networkError")) ?? null;
}

/**
 * Apply one poll decision.
 *
 * The two "still going" decisions only move the retry streak and the notice. Every other
 * decision ends the attempt, and ending it is one path: stop polling, forget the flow, then
 * either hand the result to the modal or say why it failed.
 */
function applyPollDecision(context: AttemptContext, decision: CodexAuthPollDecision): void {
  const { attempt, dispatch, t } = context;
  if (decision.kind === "abort") return;

  if (decision.kind === "missing") {
    attempt.errorStreak = decision.nextStreak;
    if (decision.retrying) {
      dispatch({ kind: "notice-raised", notice: { message: t("codexAuth.oauthStatusRetrying"), tone: "warn" } });
    }
    return;
  }

  if (decision.kind === "in-progress") {
    attempt.errorStreak = decision.nextStreak;
    dispatch(decision.waitingForCode
      ? { kind: "notice-raised", notice: { message: t("codexAuth.oauthCodeSubmitted"), tone: "ok" } }
      : { kind: "notice-cleared" });
    return;
  }

  haltPolling(attempt);
  resetManualCode(context);
  attempt.flow = null;
  dispatch({ kind: "authorization-closed" });

  if (decision.kind === "done") {
    if (!attempt.alive) return;
    attempt.onAdded(codexAccountMutationCompletion(decision.payload));
    attempt.onClose();
    return;
  }

  if (!attempt.alive) return;
  if (!context.reauthAccountId) dispatch({ kind: "returned-to-choosing" });
  dispatch({ kind: "authorization-failed", error: decision.error ?? t("codexAuth.loginFailed") });
}

/** One status read, guarded so overlapping ticks cannot stack up. */
async function pollStatusOnce(context: AttemptContext, statusUrl: string, poll: AbortController): Promise<void> {
  const { attempt } = context;
  if (attempt.pollInFlight || poll.signal.aborted) return;
  attempt.pollInFlight = true;
  try {
    const response = await fetch(statusUrl, {
      signal: AbortSignal.any([poll.signal, AbortSignal.timeout(STATUS_TICK_TIMEOUT_MS)]),
    });
    const status = await readJsonIfOk<LoginStatus>(response);
    applyPollDecision(context, decodeCodexAuthPollTick({
      alive: attempt.alive,
      aborted: poll.signal.aborted,
      status,
      errorStreak: attempt.errorStreak,
      manualCodeWaiting: attempt.manualCodePhase === "awaiting",
    }));
  } catch (error) {
    applyPollDecision(context, decodeCodexAuthPollCatch({
      alive: attempt.alive,
      aborted: poll.signal.aborted,
      isAbortError: error instanceof Error && error.name === "AbortError",
      errorStreak: attempt.errorStreak,
    }));
  } finally {
    attempt.pollInFlight = false;
  }
}

/**
 * Watch a flow until it finishes, fails, or runs past the operator's window.
 *
 * The deadline is armed once per attempt rather than per tick: it is the whole attempt's
 * budget, which is what the shorter tick signal above is not.
 */
function watchFlow(context: AttemptContext, opened: { url: string; flowId: string | null }, accountId: string): void {
  const { attempt, apiBase, dispatch } = context;
  attempt.flow = opened.flowId;
  dispatch({ kind: "authorization-opened", flowId: opened.flowId, authUrl: opened.url });
  haltPolling(attempt);

  const statusUrl = codexAuthLoginStatusUrl(apiBase, opened.flowId ?? "", accountId, context.reauthAccountId);
  const poll = new AbortController();
  attempt.pollAbort = poll;
  attempt.stopPoll = startVisibilityPoll(() => { void pollStatusOnce(context, statusUrl, poll); }, STATUS_POLL_MS);
  attempt.deadline = setTimeout(() => { onLoginWindowClosed(context); }, LOGIN_WINDOW_MS);
}

/** The operator ran out of time: drop the flow and say so. */
function onLoginWindowClosed(context: AttemptContext): void {
  const { attempt, dispatch, t } = context;
  if (!decodeCodexAuthTimeoutApplies(attempt.stopPoll !== null)) return;
  resetManualCode(context);
  void cancelLogin(context);
  if (!attempt.alive) return;
  if (!context.reauthAccountId) dispatch({ kind: "returned-to-choosing" });
  dispatch({ kind: "authorization-failed", error: t("modal.loginTimeout") });
}

/**
 * The Codex add-account / re-authentication authorization lifecycle.
 *
 * The dialog owns its phase machine; this hook owns the attempt. One attempt record holds
 * everything outside React's render model - the live flow, the poll, the deadline, the
 * abort signals - and the returned callbacks only translate what the operator did into
 * calls on it. Authentication material never leaves the fetch layer: the authorization URL
 * is the only thing the dialog is handed, and the flow id stays out of the markup.
 */
export function useAddCodexAccountOAuth({
  apiBase,
  reauthAccountId,
  state,
  dispatch,
  t,
}: {
  apiBase: string;
  reauthAccountId?: string;
  state: AddAccountState;
  dispatch: Dispatch<AddAccountEvent>;
  t: TFn;
}) {
  // The attempt lives behind a ref: every field is read and written from async continuations
  // that run outside React's render model, and no render may observe a half-applied attempt.
  const [attemptSeed] = useState(createLoginAttempt);
  const attemptRef = useRef(attemptSeed);
  const startedReauthRef = useRef<string | null>(null);

  const manualCode = state.phase === "authorizing" ? state.manualCode : "";
  const manualCodeStatus: ManualCodeStatus = state.phase === "authorizing" ? state.manualCodeStatus : "idle";
  const manualCodeBusy = manualCodeStatus === "submitting";
  const manualCodeWaiting = manualCodeStatus === "awaiting";

  useEffect(() => {
    // The poll tick runs outside React, so it reads the phase from the attempt record.
    attemptRef.current.manualCodePhase = manualCodeStatus;
  }, [manualCodeStatus]);

  const context = useCallback(
    (attempt: LoginAttempt): AttemptContext => ({ apiBase, reauthAccountId, attempt, dispatch, t }),
    [apiBase, dispatch, reauthAccountId, t],
  );

  useEffect(() => {
    const attempt = attemptRef.current;
    attempt.alive = true;
    return () => {
      const closing = context(attempt);
      resetManualCode(closing);
      attempt.alive = false;
      startedReauthRef.current = null;
      attempt.loginAbort?.abort();
      attempt.loginAbort = null;
      const flow = attempt.flow;
      attempt.flow = null;
      dispatch({ kind: "authorization-closed" });
      haltPolling(attempt);
      if (flow !== null) void abandonFlow(apiBase, { flowId: flow });
    };
  }, [apiBase, context, dispatch]);

  const bindCallbacks = useCallback((
    onAdded: (completion: CodexAccountMutationCompletion) => void,
    onClose: () => void,
  ) => {
    const attempt = attemptRef.current;
    attempt.onAdded = onAdded;
    attempt.onClose = onClose;
  }, []);

  const startOAuth = useCallback(async (requestedId?: string) => {
    const attempt = attemptRef.current;
    const opening = context(attempt);
    resetManualCode(opening);
    attempt.flow = null;
    dispatch({ kind: "authorization-requested" });
    try {
      const accountId = codexAuthLoginAccountId(reauthAccountId, requestedId);
      const body = await requestLogin(opening, requestedId);
      if (body === null) return;
      const opened = decodeCodexAuthLoginOpened(body);
      if (opened.kind === "opened") {
        watchFlow(opening, opened, accountId);
        return;
      }
      if (opened.kind === "error") dispatch({ kind: "authorization-failed", error: opened.error });
    } catch (error) {
      // A withdrawal of this hook's own request is not a failure worth reporting.
      const aborted = error instanceof Error && error.name === "AbortError";
      if (!decodeCodexAuthStartCatch(attempt.alive, aborted)) return;
      dispatch({ kind: "authorization-failed", error: error instanceof Error ? error.message : String(error) });
    }
  }, [context, dispatch, reauthAccountId]);

  useEffect(() => {
    if (!reauthAccountId) {
      startedReauthRef.current = null;
      return;
    }
    if (startedReauthRef.current === reauthAccountId) return;
    startedReauthRef.current = reauthAccountId;
    void startOAuth();
  }, [reauthAccountId, startOAuth]);

  const closeModal = useCallback(() => {
    const attempt = attemptRef.current;
    if (state.phase === "authorizing") void cancelLogin(context(attempt));
    attempt.onClose();
  }, [context, state.phase]);

  const submitManualCode = useCallback(async () => {
    const attempt = attemptRef.current;
    const flow = attempt.flow;
    const input = manualCode.trim();
    if (codexAuthManualCodeBlocked(flow, input, manualCodeBusy, manualCodeWaiting)) return;
    dispatch({ kind: "manual-code-submitted" });
    try {
      const response = await fetch(`${apiBase}/api/codex-auth/login/code`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ flowId: flow, input }),
      });
      if (!attempt.alive) return;
      if (!response.ok) {
        const body = await response.json().catch(() => ({})) as { error?: string };
        dispatch({
          kind: "manual-code-submit-failed",
          error: t("prov.pasteFail", { error: decodeCodexAuthManualCodeFailure(body, response.statusText) }),
        });
        return;
      }
      dispatch({ kind: "manual-code-awaiting" });
      dispatch({ kind: "notice-raised", notice: { message: t("codexAuth.oauthCodeSubmitted"), tone: "ok" } });
      attempt.errorStreak = 0;
    } catch {
      if (attempt.alive) dispatch({ kind: "manual-code-submit-failed", error: t("modal.networkError") });
    }
  }, [apiBase, dispatch, manualCode, manualCodeBusy, manualCodeWaiting, t]);

  return {
    manualCodeBusy,
    manualCodeWaiting,
    bindCallbacks,
    closeModal,
    startOAuth,
    submitManualCode,
  };
}
