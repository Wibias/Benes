/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useRef } from "react";
import type { TFn } from "../i18n/shared";
import { readJsonIfOk } from "../fetch-json";
import {
  addProviderOAuthAttemptStale,
  createOAuthPopup,
  decodeAddProviderManualCodeFailure,
  decodeAddProviderOAuthPollTick,
  decodeAddProviderOAuthStart,
  isOAuthLoginBusyError,
  type OAuthPopup,
} from "../lib/add-provider-oauth-decode";

export const OAUTH_LOGIN_POLL_INTERVAL_MS = 2_000;

/** Poll ticks a browser handoff may run before the modal calls it a timeout. */
const OAUTH_LOGIN_POLL_TICKS = 100;

/**
 * Every notice the OAuth start/poll loop writes back into the Add Provider
 * session. One contract for both directions of the flow: the login lane and the
 * manual paste lane write through the same names.
 */
export type AddProviderOAuthSetters = {
  setOauthBusy: (busy: boolean) => void;
  setOauthMsg: (notice: string) => void;
  setOauthMsgTone: (tone: "ok" | "warn") => void;
  setOauthUrl: (url: string, providerId: string) => void;
  setManualCode: (code: string) => void;
  setManualCodeBusy: (busy: boolean) => void;
  setManualCodeOk: (ok: boolean) => void;
  setManualCodeMsg: (notice: string) => void;
};

/** The manual paste lane touches only the paste fields. */
export type AddProviderManualCodeSetters = Pick<
  AddProviderOAuthSetters,
  "setManualCode" | "setManualCodeBusy" | "setManualCodeOk" | "setManualCodeMsg"
>;

type AddProviderOAuthAttempt = {
  apiBase: string;
  providerId: string;
  attempt: number;
  t: TFn;
  aliveRef: React.MutableRefObject<boolean>;
  attemptRef: React.MutableRefObject<number>;
  popupRef: React.MutableRefObject<OAuthPopup | null>;
  pendingProviderRef: React.MutableRefObject<string | null>;
  setters: AddProviderOAuthSetters;
  onAdded: (name: string) => void;
  cancel: () => Promise<void>;
};

/** A superseded or unmounted attempt must not write anything else back. */
function attemptIsCurrent(attempt: AddProviderOAuthAttempt): boolean {
  return !addProviderOAuthAttemptStale(attempt.attempt, attempt.attemptRef.current);
}

async function postOAuthLoginCancel(apiBase: string, provider: string): Promise<void> {
  await fetch(`${apiBase}/api/oauth/login/cancel`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ provider }),
  }).catch(() => undefined);
}

/** Start with a clean sheet: no stale notice, no stale URL, no stale paste error. */
function resetOAuthAttemptFields(setters: AddProviderOAuthSetters, providerId: string) {
  setters.setOauthBusy(true);
  setters.setOauthMsg("");
  setters.setOauthMsgTone("ok");
  setters.setOauthUrl("", providerId);
  setters.setManualCode("");
  setters.setManualCodeMsg("");
  setters.setManualCodeOk(true);
}

type OAuthStartPayload = { error?: string; url?: string; instructions?: string };

/**
 * Ask the listener to begin the flow. A leftover "already in progress" answer is
 * not final: cancel it and ask once more, because the previous browser window
 * may have been closed without a callback.
 */
async function startOAuthAttempt(
  apiBase: string,
  providerId: string,
  retryBusy: boolean,
): Promise<{ ok: boolean; data: OAuthStartPayload }> {
  const post = () => fetch(`${apiBase}/api/oauth/login`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ provider: providerId }),
  });
  let response = await post();
  let data = await response.json().catch(() => ({})) as OAuthStartPayload;
  if (!response.ok && retryBusy && isOAuthLoginBusyError(data.error)) {
    await postOAuthLoginCancel(apiBase, providerId);
    response = await post();
    data = await response.json().catch(() => ({})) as OAuthStartPayload;
  }
  return { ok: response.ok, data };
}

/** Reflect the start answer: opening the popup, a refusal, or a device-code wait. */
function publishOAuthStart(attempt: AddProviderOAuthAttempt, start: { ok: boolean; data: OAuthStartPayload }) {
  const { setters, t } = attempt;
  const decoded = decodeAddProviderOAuthStart(start.ok, start.data);
  if (decoded.kind === "unknown-provider") {
    setters.setOauthMsgTone("warn");
    setters.setOauthMsg(t("modal.oauthComingSoonShort"));
    return false;
  }
  if (decoded.kind === "start-failed") {
    setters.setOauthMsgTone("warn");
    setters.setOauthMsg(decoded.error || t("modal.loginFailStart"));
    return false;
  }
  if (decoded.kind === "opened") {
    setters.setOauthUrl(decoded.url, attempt.providerId);
    setters.setOauthMsg(t("modal.waitingLogin"));
    return true;
  }
  setters.setOauthMsg(decoded.instructions || t("modal.loggingIn"));
  return true;
}

type OAuthPollOutcome = "logged-in" | "not-yet" | "refused";

/** One status tick. The decode owner decides what a payload means. */
async function pollOAuthStatusOnce(attempt: AddProviderOAuthAttempt): Promise<OAuthPollOutcome> {
  const response = await fetch(`${attempt.apiBase}/api/oauth/status?provider=${attempt.providerId}`).catch(() => null);
  const payload = response ? await readJsonIfOk<{ loggedIn?: boolean; error?: string }>(response) : null;
  if (!attemptIsCurrent(attempt) || !attempt.aliveRef.current) return "not-yet";
  const tick = decodeAddProviderOAuthPollTick(payload);
  if (tick.kind === "error") {
    attempt.setters.setOauthMsgTone("warn");
    attempt.setters.setOauthMsg(attempt.t("modal.loginError", { error: tick.error }));
    return "refused";
  }
  return tick.kind === "logged-in" ? "logged-in" : "not-yet";
}

/**
 * Wait for the browser to come back. Lives here rather than in the caller so the
 * tick cadence, the attempt guard, and the timeout copy stay together.
 */
async function awaitOAuthCallback(attempt: AddProviderOAuthAttempt): Promise<OAuthPollOutcome> {
  for (let tick = 0; tick < OAUTH_LOGIN_POLL_TICKS; tick++) {
    await new Promise(resolve => setTimeout(resolve, OAUTH_LOGIN_POLL_INTERVAL_MS));
    if (!attempt.aliveRef.current || !attemptIsCurrent(attempt)) return "not-yet";
    const outcome = await pollOAuthStatusOnce(attempt);
    if (outcome !== "not-yet") return outcome;
  }
  if (!attempt.aliveRef.current || !attemptIsCurrent(attempt)) return "not-yet";
  attempt.setters.setOauthMsgTone("warn");
  attempt.setters.setOauthMsg(attempt.t("modal.loginTimeout"));
  return "refused";
}

/** Run one browser handoff end to end for the current attempt. */
async function runOAuthAttempt(attempt: AddProviderOAuthAttempt): Promise<void> {
  const popup = createOAuthPopup();
  attempt.popupRef.current = popup;
  try {
    const start = await startOAuthAttempt(attempt.apiBase, attempt.providerId, true);
    if (!attemptIsCurrent(attempt)) {
      if (start.ok) await postOAuthLoginCancel(attempt.apiBase, attempt.providerId);
      popup.close();
      attempt.popupRef.current = null;
      return;
    }
    if (start.ok) attempt.pendingProviderRef.current = attempt.providerId;
    if (!attempt.aliveRef.current) {
      await attempt.cancel();
      return;
    }
    if (!publishOAuthStart(attempt, start)) {
      popup.close();
      attempt.popupRef.current = null;
      return;
    }
    if (start.data.url) popup.navigate(start.data.url);
    const outcome = await awaitOAuthCallback(attempt);
    if (!attemptIsCurrent(attempt)) return;
    if (outcome === "logged-in") {
      attempt.pendingProviderRef.current = null;
      attempt.onAdded(attempt.providerId);
    }
  } catch {
    if (!attemptIsCurrent(attempt)) return;
    popup.close();
    attempt.popupRef.current = null;
    if (attempt.aliveRef.current) {
      attempt.setters.setOauthMsgTone("warn");
      attempt.setters.setOauthMsg(attempt.t("modal.networkError"));
    }
  } finally {
    if (attempt.aliveRef.current && attemptIsCurrent(attempt)) {
      attempt.pendingProviderRef.current = null;
      attempt.setters.setOauthBusy(false);
    }
  }
}

export function useAddProviderOAuth({
  apiBase,
  t,
  aliveRef,
  onAdded,
}: {
  apiBase: string;
  t: TFn;
  aliveRef: React.MutableRefObject<boolean>;
  onAdded: (name: string) => void;
}) {
  const pendingProviderRef = useRef<string | null>(null);
  const popupRef = useRef<OAuthPopup | null>(null);
  const attemptRef = useRef(0);

  const cancelPendingLogin = useCallback(async () => {
    attemptRef.current += 1;
    const provider = pendingProviderRef.current;
    pendingProviderRef.current = null;
    popupRef.current?.close();
    popupRef.current = null;
    if (!provider) return;
    await postOAuthLoginCancel(apiBase, provider);
  }, [apiBase]);

  useEffect(() => () => {
    void cancelPendingLogin();
  }, [cancelPendingLogin]);

  const loginOAuth = useCallback(async (providerId: string, setters: AddProviderOAuthSetters) => {
    resetOAuthAttemptFields(setters, providerId);
    popupRef.current?.close();
    const attempt = attemptRef.current + 1;
    attemptRef.current = attempt;
    await runOAuthAttempt({
      apiBase,
      providerId,
      attempt,
      t,
      aliveRef,
      attemptRef,
      popupRef,
      pendingProviderRef,
      setters,
      onAdded,
      cancel: cancelPendingLogin,
    });
  }, [aliveRef, apiBase, cancelPendingLogin, onAdded, t]);

  const submitManualCode = useCallback(async (
    providerId: string,
    manualCode: string,
    manualCodeBusy: boolean,
    setters: AddProviderManualCodeSetters,
  ) => {
    const input = manualCode.trim();
    if (!input || manualCodeBusy) return;
    setters.setManualCodeBusy(true);
    setters.setManualCodeMsg("");
    try {
      const response = await fetch(`${apiBase}/api/oauth/login/code`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ provider: providerId, input }),
      });
      if (!aliveRef.current) return;
      if (!response.ok) {
        const payload = await response.json().catch(() => ({})) as { error?: string };
        setters.setManualCodeOk(false);
        setters.setManualCodeMsg(t("prov.pasteFail", { error: decodeAddProviderManualCodeFailure(payload, response.statusText) }));
        return;
      }
      setters.setManualCode("");
      setters.setManualCodeOk(true);
      setters.setManualCodeMsg(t("prov.pasteOk"));
    } catch {
      if (aliveRef.current) {
        setters.setManualCodeOk(false);
        setters.setManualCodeMsg(t("modal.networkError"));
      }
    } finally {
      if (aliveRef.current) setters.setManualCodeBusy(false);
    }
  }, [aliveRef, apiBase, t]);

  return { loginOAuth, submitManualCode, cancelPendingLogin };
}
