/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { Locale } from "./i18n/shared";

/** Binary unit symbols are locale-invariant, like model ids. */
const BINARY_UNITS = ["B", "KiB", "MiB", "GiB", "TiB"] as const;

/** TiB is the largest unit this formatter prints. */
const LARGEST_UNIT_INDEX = BINARY_UNITS.length - 1;

/**
 * The unit that carries the value: the largest one that still leaves at least one whole
 * unit, that is the largest `i` with `bytes / 1024 ** (i + 1) >= 1`. A value that never
 * reaches a whole unit (including non-finite input) stays on KiB, as before.
 */
function binaryUnitIndex(bytes: number): number {
  let index = 0;
  for (let candidate = 0; candidate < LARGEST_UNIT_INDEX; candidate += 1) {
    if (bytes / 1024 ** (candidate + 1) >= 1) index = candidate;
  }
  return index;
}

/** Human-readable byte size (1.5 MiB, 320 KiB). */
export function formatBytes(bytes: number, locale: Locale): string {
  if (bytes < 1024) return `${bytes} B`;
  const index = binaryUnitIndex(bytes);
  const scaled = bytes / 1024 ** (index + 1);
  return `${scaled.toLocaleString(locale, { maximumFractionDigits: 1 })} ${BINARY_UNITS[index + 1]}`;
}
