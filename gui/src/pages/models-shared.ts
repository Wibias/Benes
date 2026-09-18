/**
 * Benes dashboard client for the Go proxy (`internal/server`).
 * Shared model-catalog vocabulary for the Models page and its routing/profile callers.
 *
 * This module owns the *data* side of the catalog: the row shape the board renders, the
 * numeric ladders the pickers offer, and the persisted collapse preference. It holds no
 * React state and does not fetch, so every caller can share one definition without
 * pulling the Models board into its own bundle.
 */

import type { TFn, TKey } from "../i18n/shared";
import type { ProviderDiscoverySummary } from "../models-groups";

/** The discovery-failure variant of the provider summary, which carries the reason. */
export type DiscoveryFailure = Extract<ProviderDiscoverySummary, { status: "failed" }>;

/**
 * Failure reason → catalog key. Kept as a lookup rather than a switch so an unknown
 * reason from a newer Go listener still produces a sentence instead of nothing: the
 * board must never show a failed provider row with no explanation.
 */
const DISCOVERY_FAILURE_KEYS: ReadonlyMap<string, TKey> = new Map<string, TKey>([
  ["http", "models.discoveryFailedHttp"],
  ["blocked", "models.discoveryFailedBlocked"],
  ["invalid_response", "models.discoveryFailedInvalidResponse"],
  ["network", "models.discoveryFailedNetwork"],
  ["provider", "models.discoveryFailedProvider"],
]);

export function discoveryFailureLabel(translate: TFn, failure: DiscoveryFailure): string {
  const key = DISCOVERY_FAILURE_KEYS.get(failure.reason) ?? "models.discoveryFailedGeneric";
  // Only the HTTP variant of the summary carries a status, and only its message
  // interpolates one; the other reasons must not observe an empty `{status}`.
  if (failure.httpStatus === undefined) return translate(key);
  return translate(key, { status: failure.httpStatus });
}

/**
 * One catalog row as the Models board groups it under its provider. Fields are grouped by
 * concern, not by consumer: the addressing triple, then what the provider advertises, then
 * the cap and the routing flags the board writes back.
 */
export type ModelRow = {
  provider: string;
  id: string;
  namespaced: string;
  /** Catalog name, when the provider advertises one. */
  displayName?: string;
  /** Window the provider advertises for this row, before any stored cap. */
  contextWindow?: number;
  standardContextWindow?: number;
  inputModalities?: string[];
  /** Ceiling the board dialled in for this row; a cap only ever lowers a window. */
  contextCap?: number;
  contextCapped?: boolean;
  /** Where the row came from: the provider's own catalog, or a model the user added. */
  native?: boolean;
  custom?: boolean;
  customId?: string;
  /** Routing currently excludes this row. */
  disabled: boolean;
  /** Stored custom-row override (not the inherited ladder); only present on custom rows. */
  reasoningEfforts?: string[];
};

/** Answer of `GET /api/provider-context-caps`, in the shape the Go listener publishes. */
export interface ProviderContextCapsResponse {
  /** Per-provider caps, when the listener has them. */
  caps?: Record<string, number>;
  /** The live global cap. */
  value?: number;
  /** The conservative cap the listener falls back to. */
  cap?: number;
}

/**
 * Reasoning-effort labels offered in the custom-model dialog. The full set of real
 * `reasoning_effort` values (none, minimal, low, medium, high, xhigh, max). Deliberately
 * excludes `ultra`: that is a Codex catalog label for the multi-agent collab surface, not a
 * real `reasoning_effort` value — codex-rs converts it to `max` before any provider
 * request, and the catalog writer appends it to every non-empty ladder anyway.
 */
export const REASONING_EFFORT_LEVELS = ["none", "minimal", "low", "medium", "high", "xhigh", "max"] as const;

/** The sentinel every picker offers next to its own ladder, for a value it cannot name. */
export const CUSTOM_OPTION = "custom";

/** Context-cap ladder for API-key providers: 100k … 950k in 50k steps. */
const CAP_FLOOR = 100_000;
const CAP_STEP = 50_000;
const CAP_RUNGS = 18;

export const CAP_OPTIONS = Array.from({ length: CAP_RUNGS }, (_, index) => CAP_FLOOR + index * CAP_STEP);
export const CAP_OPTION_SET = new Set(CAP_OPTIONS);

/**
 * Cap presets for the Codex-login native group.
 *
 * GPT-5.6 live catalogs advertise 1,050,000 tokens (available). Older GPT-5.x
 * windows (272k) and the previous 922k input cap remain as lowering choices.
 * A cap only ever lowers a window, so listing a value above the advertised one
 * would be an inert choice. Anything else goes through "Custom".
 */
const NATIVE_GPT56_WINDOW = 1_050_000;

export const NATIVE_GPT56_DEFAULT_WINDOW = NATIVE_GPT56_WINDOW;
export const NATIVE_CAP_OPTIONS = [272_000, 400_000, 922_000, NATIVE_GPT56_WINDOW];
export const NATIVE_CAP_OPTION_SET = new Set(NATIVE_CAP_OPTIONS);

/** `k` and `M` are the only units this display ever names; both are exact thousands. */
const COMPACT_STEP = 1_000;
const COMPACT_MILLION = 1_000 * COMPACT_STEP;

/**
 * Compact token display (350k, 1.05M) — the unit suffix is technical notation, not prose,
 * so it is not an i18n string (same rule the "k" suffix has always followed).
 */
export function fmtK(count: number): string {
  // Non-finite, zero, and negative counts have no compact form; a count that is not an
  // exact thousand reads better with the locale's own digit grouping (12,345).
  const positive = Number.isFinite(count) && count > 0;
  if (!positive || count % COMPACT_STEP !== 0) return positive ? count.toLocaleString() : String(count);
  if (count >= COMPACT_MILLION) {
    // Past a million "1050k" stops reading as a size. Trailing zeros are dropped so
    // 1,000,000 renders as "1M" rather than "1.00M".
    const millions = Number((count / COMPACT_MILLION).toFixed(2));
    // eslint-disable-next-line local-i18n/no-hardcoded-ui-strings -- unit suffix, not prose
    return `${millions}M`;
  }
  return `${count / COMPACT_STEP}k`;
}

export function collectDisabledNamespaced(rows: readonly ModelRow[]): Set<string> {
  return new Set(rows.filter(row => row.disabled).map(row => row.namespaced));
}

/**
 * Folded provider groups, as the board last left them.
 *
 * The key names the one thing it holds, so the parsing lives with the board that folds
 * the groups: a preference is only meaningful against the surface that wrote it, and a
 * generic collapse helper would make one board's ids readable as another's state.
 */
const COLLAPSED_PROVIDERS_KEY = "benes-models-collapsed:v2";

/** The two `Storage` methods this preference uses; tests pass a stand-in for `localStorage`. */
type CollapseStorage = Pick<Storage, "getItem" | "setItem">;

function collapseStorage(injected?: CollapseStorage): CollapseStorage | null {
  if (injected) return injected;
  // The board also renders where `localStorage` does not exist at all. That is an ordinary
  // state, not an error: the preference is simply unknown and the caller defaults.
  return typeof localStorage === "undefined" ? null : localStorage;
}

/**
 * `null` = no stored preference, so the caller applies its own default. An empty Set is a
 * different answer — the user opened every group — and has to survive a reload, so it is
 * never reported as "no preference".
 */
export function readCollapsedProviders(storage?: CollapseStorage): Set<string> | null {
  const source = collapseStorage(storage);
  if (source === null) return null;
  let saved: string | null;
  try {
    saved = source.getItem(COLLAPSED_PROVIDERS_KEY);
  } catch {
    // A storage that refuses the read leaves the board with no preference, which is the
    // same answer as an empty storage.
    return null;
  }
  if (saved === null) return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(saved);
  } catch {
    return null;
  }
  // Only a list of provider ids is a readable preference. Any other shape — a stale
  // object form, a bare scalar — is not a partial answer, so the board returns to its
  // default rather than folding a subset the user never chose.
  if (!Array.isArray(parsed)) return null;
  return new Set(parsed.filter((entry): entry is string => typeof entry === "string"));
}

export function writeCollapsedProviders(collapsed: ReadonlySet<string>, storage?: CollapseStorage): void {
  const target = collapseStorage(storage);
  if (target === null) return;
  try {
    target.setItem(COLLAPSED_PROVIDERS_KEY, JSON.stringify([...collapsed]));
  } catch {
    // Quota and private-mode refusals leave the board working. The preference is a
    // convenience for the next visit, never state the catalogue depends on.
  }
}

/** Concurrency presets for the multi-agent thread picker. */
const THREAD_CHOICES = [4, 8, 16, 32, 64, 128, 256, 500, 1000];

export const THREAD_OPTION_SET = new Set(THREAD_CHOICES);

/** Rows rendered per provider before a "show more" control. */
export const PAGE = 60;
