/** Benes dashboard client for the Go proxy (`internal/server`). */

/** Threshold the dashboard paints before `/active` confirms the stored value. */
export const DEFAULT_AUTO_SWITCH_THRESHOLD = 80;

const AUTO_SWITCH_PATH = "/api/codex-auth/auto-switch";
const AUTO_SWITCH_PUT_TIMEOUT_MS = 10_000;
const ENABLED_MIN = 1;
const ENABLED_MAX = 100;

export type AutoSwitchFetch = (url: string, options: RequestInit) => Promise<Response>;

export type AutoSwitchThresholdReadDisposition = "apply" | "defer" | "ignore";

export interface AutoSwitchTogglePlan {
  threshold: number;
  lastEnabled: number;
}

function integerOrNull(raw: unknown): number | null {
  return typeof raw === "number" && Number.isInteger(raw) ? raw : null;
}

/** A whole number in the enabled range (1–100), or null. */
function enabledValue(raw: unknown): number | null {
  const whole = integerOrNull(raw);
  if (whole === null) return null;
  return whole >= ENABLED_MIN && whole <= ENABLED_MAX ? whole : null;
}

/** 0–100 integer, or the default when the value is anything else. */
export function normalizeAutoSwitchThreshold(raw: unknown): number {
  const whole = integerOrNull(raw);
  if (whole === null || whole < 0 || whole > 100) return DEFAULT_AUTO_SWITCH_THRESHOLD;
  return whole;
}

/** A drafted enabled threshold: digits only, 1–100. Anything else is null. */
export function parseEnabledAutoSwitchThreshold(typedText: string): number | null {
  const digits = typedText.trim();
  if (!/^\d+$/.test(digits)) return null;
  return enabledValue(Number(digits));
}

/** What the toggle restores: 0 when currently enabled, else the last enabled value. */
export function nextAutoSwitchThreshold(active: number, enabledBefore: number): number {
  if (active > 0) return 0;
  return enabledValue(enabledBefore) ?? DEFAULT_AUTO_SWITCH_THRESHOLD;
}

/**
 * Whether a `/active` read that started at `readRevision` may still be applied.
 * A superseded read is dropped; a read that races an edit or save is held back.
 */
export function autoSwitchThresholdReadDisposition(
  isEditing: boolean,
  isSaving: boolean,
  readRevision: number,
  liveRevision: number,
): AutoSwitchThresholdReadDisposition {
  if (readRevision !== liveRevision) return "ignore";
  if (isEditing || isSaving) return "defer";
  return "apply";
}

/** Accept a bare threshold number or a full `/active` payload. */
export function extractAutoSwitchThresholdPayload(raw: unknown): unknown {
  if (typeof raw !== "object" || raw === null || Array.isArray(raw)) return raw;
  const row = raw as Record<string, unknown>;
  return "autoSwitchThreshold" in row ? row.autoSwitchThreshold : raw;
}

/**
 * Disabling is one server write. A valid dirty draft becomes the page-lifetime
 * restore value, so re-enabling can persist it without a partial two-write
 * failure state.
 */
export function planAutoSwitchToggleWrite(
  active: number,
  typedText: string,
  enabledBefore: number,
): AutoSwitchTogglePlan {
  if (active > 0) {
    const drafted = parseEnabledAutoSwitchThreshold(typedText);
    const restore = drafted ?? nextAutoSwitchThreshold(0, enabledBefore);
    return { threshold: 0, lastEnabled: restore };
  }
  const enableTo = nextAutoSwitchThreshold(active, enabledBefore);
  return { threshold: enableTo, lastEnabled: enableTo };
}

export async function putAutoSwitchThreshold(
  baseUrl: string,
  next: number,
  request: AutoSwitchFetch = (url, options) => fetch(url, options),
  timeout: number = AUTO_SWITCH_PUT_TIMEOUT_MS,
): Promise<boolean> {
  if (!Number.isInteger(next) || next < 0 || next > 100) return false;
  const init: RequestInit = {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    signal: AbortSignal.timeout(timeout),
    body: JSON.stringify({ threshold: next }),
  };
  try {
    const response = await request(`${baseUrl}${AUTO_SWITCH_PATH}`, init);
    return response.ok;
  } catch {
    return false;
  }
}
