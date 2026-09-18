/** Benes dashboard client for the Go proxy (`internal/server`). */

/*
 * A NumberStepper field keeps its value as a text draft, so a half-typed or empty input
 * still has to survive a click on +/-. Exactly two things are policy here: which number
 * an unreadable draft starts from, and how the stepped result is printed back.
 */

/** An unreadable draft starts from the low bound, so the first step is predictable. */
function draftBase(raw: string, fallback: number): number {
  const text = raw.trim();
  if (text.length === 0) return fallback;
  const parsed = Number(text);
  return Number.isFinite(parsed) ? parsed : fallback;
}

/** Fractional steps (GiB-style) keep one decimal; whole steps print integers. */
function draftDecimals(step: number): number {
  return step < 1 ? 1 : 0;
}

function roundTo(value: number, decimals: number): number {
  const scale = 10 ** decimals;
  return Math.round(value * scale) / scale;
}

/** Step a numeric draft by `delta` and clamp the result into [min, max]. */
export function clampNumberDraft(
  raw: string,
  delta: number,
  min: number,
  max: number,
  step = 1,
): string {
  const moved = draftBase(raw, min) + delta;
  const bounded = Math.min(Math.max(moved, min), max);
  return String(roundTo(bounded, draftDecimals(step)));
}
