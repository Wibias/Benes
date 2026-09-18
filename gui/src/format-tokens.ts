/** Benes dashboard client for the Go proxy (`internal/server`). */
/**
 * Token counts read differently per script family. Western locales step by thousands
 * (K/M/B/T); ko, zh, and zh-TW step by the myriad, because that is the scale those
 * readers count in — ko 만/억/조/경, zh 万/亿/兆/京, zh-TW 萬/億/兆/京. Dashboard, Usage,
 * and Logs all share this single reading.
 */

/** `[smallest count the scale applies to, divisor, suffix]`. */
type TokenScale = readonly [minimum: number, divisor: number, suffix: string];

const MYRIAD_SCALES: Record<string, readonly TokenScale[]> = {
  ko: [[1e16, 1e16, "경"], [1e12, 1e12, "조"], [1e8, 1e8, "억"], [1e4, 1e4, "만"]],
  zh: [[1e16, 1e16, "京"], [1e12, 1e12, "兆"], [1e8, 1e8, "亿"], [1e4, 1e4, "万"]],
  "zh-TW": [[1e16, 1e16, "京"], [1e12, 1e12, "兆"], [1e8, 1e8, "億"], [1e4, 1e4, "萬"]],
};

const THOUSAND_SCALES: readonly TokenScale[] = [
  [1e12, 1e12, "T"],
  [1e9, 1e9, "B"],
  [1e6, 1e6, "M"],
  [1e4, 1e3, "K"],
];

/** 12.0만 prints as 12만. Counts below the smallest scale stay exact. */
function dropTrailingZeros(text: string): string {
  return text.replace(/\.0+$/, "").replace(/(\.\d*?)0+$/, "$1");
}

export function formatTokens(n: number, locale: string): string {
  const scale = (MYRIAD_SCALES[locale] ?? THOUSAND_SCALES).find(([minimum]) => n >= minimum);
  if (!scale) return String(n);
  return `${dropTrailingZeros((n / scale[1]).toFixed(1))}${scale[2]}`;
}
