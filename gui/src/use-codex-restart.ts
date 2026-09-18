import { useCallback, useEffect, useRef, useState } from "react";
import { useI18n } from "./i18n/shared.ts";
import type { TFn } from "./i18n/shared.ts";
import { classifyCodexRestart, requestCodexRestart } from "./codex-restart.ts";
import type { CodexRestartCode, CodexRestartResponse } from "./codex-restart.ts";

export interface CodexRestartController {
  restarting: boolean;
  /**
   * Resolves to the response code, or null when the user declined the confirm or
   * the call failed. Callers that track staleness must treat BOTH `stopped` and
   * `nothing_running` as "no stale app-server remains" — the second is the race
   * where the target exited on its own, and refreshing on only the first would
   * leave a staleness banner up after a successful outcome.
   */
  restart: () => Promise<CodexRestartCode | null>;
}

export interface CodexRestartOptions {
  /**
   * Called after any outcome that means no stale app-server remains. This is how
   * a surface that renders staleness stays correct no matter which button the
   * user pressed — including the sidebar button, which knows nothing about the
   * models page.
   */
  onSettled?: (code: CodexRestartCode) => void;
}

/** Codes that mean nothing stale is left running. */
const SETTLED_CODES: ReadonlySet<CodexRestartCode> = new Set<CodexRestartCode>([
  "stopped",
  "nothing_running",
]);

/** True when the outcome means nothing stale is left running. */
export function isRestartSettled(code: CodexRestartCode): boolean {
  return SETTLED_CODES.has(code);
}

/**
 * Clear the pending attempt only when it is still the newest one, so a stale
 * completion cannot re-enable the button underneath a newer run. `0` means idle.
 */
export function clearRunningAttempt(current: number, attempt: number): number {
  return current === attempt ? 0 : current;
}

/** i18n keys for the restart confirm prompt and its failure copy. */
const RESTART_KEYS = {
  confirm: "dash.codexRestartConfirm",
  failed: "dash.codexRestartFailed",
  unreachable: "dash.codexRestartUnreachable",
  timeout: "dash.codexRestartTimeout",
  malformed: "dash.codexRestartMalformed",
} as const;

/** The four localized failure formatters one restart request needs. */
function restartRequestMessages(translate: TFn) {
  return {
    formatFailure: (status: number) => translate(RESTART_KEYS.failed, { status: String(status) }),
    formatUnreachable: () => translate(RESTART_KEYS.unreachable),
    formatTimeout: () => translate(RESTART_KEYS.timeout),
    formatMalformed: () => translate(RESTART_KEYS.malformed),
  };
}

/** Localized success alert for one restart response. */
export function codexRestartAlertText(translate: TFn, result: CodexRestartResponse): string {
  const announcement = classifyCodexRestart(result);
  switch (announcement.kind) {
    case "stopped":
      return translate("dash.codexRestartDone", { count: String(announcement.stoppedCount) });
    case "nothing-running":
      return translate("dash.codexRestartNothing");
    case "enumeration-unavailable":
      return translate("dash.codexRestartUnknown");
    default:
      return translate("dash.codexRestartPartial", { count: String(announcement.survivingCount) });
  }
}

/**
 * Shared restart action for the sidebar and the models page.
 *
 * The confirm is not ceremony: stopping an app-server can interrupt a Codex turn
 * that is running right now. That is precisely the consent the startup path
 * refuses to assume on the user's behalf, and a dashboard click is where the user
 * gives it.
 *
 * Pending is derived from the running attempt's id rather than a boolean. A stale
 * completion clears only the id it owns, so a second click cannot be re-enabled by
 * the first one finishing late.
 */
export function useCodexRestart(
  apiBase: string,
  callbacks: CodexRestartOptions = {},
): CodexRestartController {
  const { t: translate } = useI18n();
  const [runningAttempt, setRunningAttempt] = useState(0);
  const issuedRef = useRef(0);
  // The request outlives a navigation away from the page that started it, so the
  // completion path must not touch state after unmount. An aborted controller is
  // the unmount signal; nothing else ever aborts it.
  const lifetimeRef = useRef<AbortController | null>(null);
  const settledRef = useRef(callbacks.onSettled);

  useEffect(() => {
    // Written in an effect, not during render: a ref assignment in the render
    // body is exactly what the react-compiler lint forbids.
    settledRef.current = callbacks.onSettled;
  }, [callbacks.onSettled]);

  useEffect(() => {
    const lifetime = new AbortController();
    lifetimeRef.current = lifetime;
    return () => {
      lifetime.abort();
      lifetimeRef.current = null;
    };
  }, []);

  const restart = useCallback(async (): Promise<CodexRestartCode | null> => {
    const confirmed = confirm(translate(RESTART_KEYS.confirm));
    if (!confirmed) return null;
    const id = issuedRef.current + 1;
    issuedRef.current = id;
    setRunningAttempt(id);

    const outcome = await requestCodexRestart(apiBase, restartRequestMessages(translate));
    const live = lifetimeRef.current !== null;
    // Clear pending only if this attempt is still the newest one.
    if (live) setRunningAttempt(current => clearRunningAttempt(current, id));

    if (!outcome.ok || outcome.result === undefined) {
      alert(outcome.message);
      return null;
    }

    const response = outcome.result;
    const announcement = codexRestartAlertText(translate, response);
    alert(announcement);

    // Only while mounted: a settled callback typically starts a refresh fetch,
    // and firing it from a page the user already left is work nobody reads.
    const settled = settledRef.current;
    if (live && settled !== undefined && isRestartSettled(response.code)) settled(response.code);
    return response.code;
  }, [apiBase, translate]);

  return { restarting: runningAttempt !== 0, restart };
}
