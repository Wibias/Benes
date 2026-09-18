/**
 * Benes dashboard source. The one plaintext key the listener hands over, and
 * how long it stays visible.
 *
 * A create response is the only moment the dashboard ever holds a full key, so
 * the rule is a pair of pure decisions instead of ad-hoc component state: the
 * plaintext is shown while its dialog is open, dismissing consumes it, and a
 * consumed value is never revealed again even though the create result may
 * still be in memory behind it.
 */

/**
 * The plaintext the dialog may show, or `null` when nothing is revealable.
 *
 * `alreadyConsumed` is compared by the caller because only it knows which live
 * value was dismissed; this function never invents a session for one.
 */
export function oneTimeSecretForDialog(
  liveSecret: string | null,
  dialogOpen: boolean,
  alreadyConsumed: boolean,
): string | null {
  if (!dialogOpen || liveSecret === null || alreadyConsumed) return null;
  return liveSecret;
}

/** What dismissing consumes: the value just shown, else the one already consumed. */
export function consumedOneTimeSecret(
  shownSecret: string | null,
  alreadyConsumed: string | null,
): string | null {
  return shownSecret ?? alreadyConsumed;
}
