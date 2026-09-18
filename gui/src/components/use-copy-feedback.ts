/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useState } from "react";
import {
  COPY_FEEDBACK_VISIBLE_MS,
  copyTextToClipboard,
  createAttemptSequencer,
  feedbackOutcome,
  settledCopy,
  type CopyFeedback,
  type CopyOutcome,
} from "../copy-feedback";

export type { CopyOutcome };

/** What the hook keeps: the newest attempt's result, tagged with the attempt that produced it. */
interface SettledFeedback<Scope> extends CopyFeedback<Scope> {
  attempt: number;
}

/** The two things a copy affordance reads: the outcome for a target, and the write itself. */
interface CopyFeedbackApi<Scope> {
  outcomeFor: (scope: Scope) => CopyOutcome | null;
  copy: (text: string, scope: Scope) => void;
}

/**
 * One copy protocol for every copy affordance: attempt, report honestly, expire.
 *
 * The hook stores only what the UI reads, so the label expires on the stored state's
 * own clock: the effect below owns the single timer, which makes a newer write, an
 * unmount, and the visible window closing the same path. Scope travels with the
 * outcome, so a changed target reads as idle through `feedbackOutcome` at render time
 * instead of through an effect that clears the label.
 */
export function useCopyFeedback<Scope = void>(): CopyFeedbackApi<Scope> {
  // One sequence per mounted affordance; built once rather than per render.
  const [attempts] = useState(createAttemptSequencer);
  const [settled, setSettled] = useState<SettledFeedback<Scope> | null>(null);

  useEffect(() => {
    if (settled === null) return undefined;
    const { attempt } = settled;
    const expiry = setTimeout(() => {
      // A newer write replaced this one: its own timer already owns the label.
      setSettled((current) => (current?.attempt === attempt ? null : current));
    }, COPY_FEEDBACK_VISIBLE_MS);
    return () => {
      clearTimeout(expiry);
    };
  }, [settled]);

  const copy = useCallback(
    (text: string, scope: Scope) => {
      const attempt = attempts.begin();
      void copyTextToClipboard(text).then((copied) => {
        // A delayed permission prompt must not overwrite a later click's result.
        if (!attempts.isNewest(attempt)) return;
        setSettled({ ...settledCopy(scope, copied), attempt });
      });
    },
    [attempts],
  );

  const outcomeFor = useCallback((scope: Scope) => feedbackOutcome(settled, scope), [settled]);

  return { outcomeFor, copy };
}

