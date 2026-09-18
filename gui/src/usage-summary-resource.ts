/**
 * Shared cache identity for the 30-day usage snapshot.
 *
 * Every subscriber of this key shares one `useKeyedClientResource` store.
 * Do not invent a per-surface key unless the fetch window itself changes.
 */
const USAGE_SUMMARY_WINDOW = "30d";
const USAGE_SUMMARY_PREFIX = `usage-summary-${USAGE_SUMMARY_WINDOW}`;

export type UsageSummarySurface = "all" | "codex";

export function usageSummary30dResourceKey(
  apiBase: string,
  surface: UsageSummarySurface = "all",
): string {
  const scope: UsageSummarySurface = surface === "codex" ? "codex" : "all";
  return `${USAGE_SUMMARY_PREFIX}:${apiBase}:${scope}`;
}
