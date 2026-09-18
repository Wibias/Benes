/** Dashboard freshness from a last-checked epoch. */

type FreshnessKey = "time.justNow" | "time.notChecked" | "time.minutesAgo" | "time.hoursAgo" | "time.daysAgo";
type Translate = (key: FreshnessKey, vars?: Record<string, string | number>) => string;

const EN: Translate = (key, vars) => {
  if (key === "time.justNow") return "Just now";
  if (key === "time.notChecked") return "Not checked";
  if (key === "time.minutesAgo") return `${vars?.n ?? 0} min ago`;
  if (key === "time.hoursAgo") return `${vars?.n ?? 0}h ago`;
  if (key === "time.daysAgo") return `${vars?.n ?? 0}d ago`;
  return key;
};

export function formatElapsedSince(
  updatedAt: number | undefined,
  translate: Translate = EN,
  now?: number,
): string {
  const clock = now ?? Date.now();
  if (typeof updatedAt !== "number" || !Number.isFinite(updatedAt)) return translate("time.notChecked");
  const elapsedMin = Math.max(0, Math.trunc((clock - updatedAt) / 60_000));
  if (elapsedMin === 0) return translate("time.justNow");
  if (elapsedMin < 60) return translate("time.minutesAgo", { n: elapsedMin });
  const elapsedHr = Math.trunc(elapsedMin / 60);
  if (elapsedHr < 24) return translate("time.hoursAgo", { n: elapsedHr });
  return translate("time.daysAgo", { n: Math.trunc(elapsedHr / 24) });
}
