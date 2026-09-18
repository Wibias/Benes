/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { TFn } from "./i18n/shared";

/**
 * Account selection order. Higher numbers are picked earlier; 0 is the unset default.
 *
 * The control never says "priority" to the user: its presets read as a sequence
 * (First, Earlier, Normal, Later, Last) so the setting describes WHEN an account is
 * used rather than how important it is. Only identifiers keep the internal name.
 */

export const DEFAULT_ACCOUNT_PRIORITY = 0;
export const MIN_ACCOUNT_PRIORITY = -100;
export const MAX_ACCOUNT_PRIORITY = 100;

/** i18n keys for the preset names. Kept here so the select stays copy-free. */
export type AccountPriorityPresetKey =
  | "accountPool.priorityFirst"
  | "accountPool.priorityEarlier"
  | "accountPool.priorityNormal"
  | "accountPool.priorityLater"
  | "accountPool.priorityLast";

/**
 * The presets the select shows, highest first, paired with the label key each one
 * reads. One table drives both the values and their copy, so a preset cannot drift
 * out of the sequence its label claims.
 */
const PRIORITY_PRESETS: ReadonlyArray<{ preset: number; labelKey: AccountPriorityPresetKey }> = [
  { preset: 2, labelKey: "accountPool.priorityFirst" },
  { preset: 1, labelKey: "accountPool.priorityEarlier" },
  { preset: 0, labelKey: "accountPool.priorityNormal" },
  { preset: -1, labelKey: "accountPool.priorityLater" },
  { preset: -2, labelKey: "accountPool.priorityLast" },
];

export const ACCOUNT_PRIORITY_PRESETS: readonly number[] = PRIORITY_PRESETS.map(row => row.preset);

function presetKeyFor(candidate: number): AccountPriorityPresetKey | null {
  return PRIORITY_PRESETS.find(row => row.preset === candidate)?.labelKey ?? null;
}

export function normalizeAccountPriority(raw: unknown): number {
  if (typeof raw !== "number" || !Number.isInteger(raw)) return DEFAULT_ACCOUNT_PRIORITY;
  const belowFloor = raw < MIN_ACCOUNT_PRIORITY;
  const aboveCeiling = raw > MAX_ACCOUNT_PRIORITY;
  return belowFloor || aboveCeiling ? DEFAULT_ACCOUNT_PRIORITY : raw;
}

/**
 * Signed rendering with an ASCII hyphen: "+2", "0", "-1". The number is always part of
 * the label, so an operator can map the words back to what the API and CLI report.
 */
export function formatAccountPriority(raw: unknown): string {
  const priority = normalizeAccountPriority(raw);
  const plus = priority > 0 ? "+" : "";
  return `${plus}${priority}`;
}

export function isAccountPriorityPreset(candidate: number): boolean {
  return presetKeyFor(candidate) !== null;
}

/** Label key for a preset, or null for a value only the API or CLI can produce. */
export function accountPriorityPresetKey(candidate: number): AccountPriorityPresetKey | null {
  return presetKeyFor(candidate);
}

/** "First (+2)" for presets, "Custom (+7)" for anything else. */
export function accountPriorityLabel(translate: TFn, raw: number): string {
  const priority = normalizeAccountPriority(raw);
  const named = translate(accountPriorityPresetKey(priority) ?? "accountPool.priorityCustom");
  return translate("accountPool.priorityOption", { name: named, value: formatAccountPriority(priority) });
}
