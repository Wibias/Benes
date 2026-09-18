/** Benes dashboard client for the Go proxy (`internal/server`). */

/**
 * What a clipboard write is allowed to report. There is no pending member: the
 * affordance keeps its idle label until the write settles.
 */
export type CopyOutcome = "copied" | "unavailable";

/** How long a settled outcome stays on screen before the affordance reads idle again. */
export const COPY_FEEDBACK_VISIBLE_MS = 2500;

/**
 * The newest write's result, tagged with the target it belongs to. Scope travels
 * with the outcome so a changed target reads as idle at render time instead of
 * through an effect that clears the label.
 */
export interface CopyFeedback<Scope> {
  scope: Scope;
  outcome: CopyOutcome;
}

/** A settled clipboard write, ready to publish. */
export function settledCopy<Scope>(scope: Scope, copied: boolean): CopyFeedback<Scope> {
  return { scope, outcome: copied ? "copied" : "unavailable" };
}

/** The outcome showing for `scope`, or `null` when that target has nothing to report. */
export function feedbackOutcome<Scope>(feedback: CopyFeedback<Scope> | null, scope: Scope): CopyOutcome | null {
  if (feedback === null || !Object.is(feedback.scope, scope)) return null;
  return feedback.outcome;
}

/**
 * Monotonic attempt counter for clipboard writes.
 *
 * Writes settle out of order: a permission prompt can delay the first attempt past
 * a second one. Without a generation the older completion overwrites the newer
 * click's result — and its timer then expires the wrong label. Only the newest
 * attempt may publish an outcome, and only the newest attempt's timer may clear it.
 */
export function createAttemptSequencer() {
  let newest = 0;
  return {
    begin(): number {
      newest += 1;
      return newest;
    },
    isNewest(attempt: number): boolean {
      return attempt === newest;
    },
  };
}

export type AttemptSequencer = ReturnType<typeof createAttemptSequencer>;
/**
 * Write `value` to the clipboard, reporting honestly whether it landed. Never throws.
 *
 * The legacy `execCommand` path is not decoration: `navigator.clipboard` is undefined
 * outside a secure context, which is exactly what a LAN-bound dashboard
 * (`hostname: 0.0.0.0` over plain HTTP) serves. Without the fallback, copying would be
 * permanently unavailable on that deployment.
 */
export async function copyTextToClipboard(value: string): Promise<boolean> {
  const clipboard = navigator.clipboard;
  if (clipboard?.writeText) {
    try {
      await clipboard.writeText(value);
      return true;
    } catch {
      // Permission denied or a non-secure context: fall through to the legacy path.
    }
  }
  return legacyClipboardCopy(value);
}

function legacyClipboardCopy(value: string): boolean {
  const legacy = typeof document !== "undefined" && typeof document.execCommand === "function";
  if (!legacy) return false;
  const field = document.createElement("textarea");
  field.readOnly = true;
  field.value = value;
  field.setAttribute("aria-hidden", "true");
  Object.assign(field.style, { position: "fixed", top: "0", opacity: "0" });
  document.body.appendChild(field);
  let copied = false;
  try {
    field.select();
    copied = document.execCommand("copy");
  } catch {
    // execCommand threw; `copied` stays false.
  }
  field.remove();
  return copied;
}

