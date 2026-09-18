/** Benes dashboard client for the Go proxy (`internal/server`). */

/**
 * The rotation values a Codex or Anthropic account pool stores on the listener, and the
 * single write that changes them.
 *
 * The three settings are one setting in practice: the card reads them together and writes
 * them as one patch. They are modelled here as a value table plus the guards over it, so
 * "an unknown value is the default" is written once instead of restated per field.
 */

const STRATEGY_VALUES = ["quota", "round-robin", "fill-first", "reset-window"] as const;
const RESET_ORDER_VALUES = ["soonest", "latest"] as const;

export type AccountPoolStrategy = (typeof STRATEGY_VALUES)[number];
export type AccountPoolResetOrder = (typeof RESET_ORDER_VALUES)[number];

/** The operator-facing order of both domains, straight off the value tables. */
export const ACCOUNT_POOL_STRATEGIES: readonly AccountPoolStrategy[] = STRATEGY_VALUES;
export const ACCOUNT_POOL_RESET_ORDERS: readonly AccountPoolResetOrder[] = RESET_ORDER_VALUES;

export const DEFAULT_ACCOUNT_POOL_STRATEGY: AccountPoolStrategy = "quota";
export const DEFAULT_ACCOUNT_POOL_RESET_ORDER: AccountPoolResetOrder = "soonest";

/** A thread affinity of 1 is "stay put"; the upper bound is the listener's own ceiling. */
export const MIN_ACCOUNT_POOL_STICKY_LIMIT = 1;
export const MAX_ACCOUNT_POOL_STICKY_LIMIT = 100;
export const DEFAULT_ACCOUNT_POOL_STICKY_LIMIT = 1;

/** Where the listener keeps the three rotation values. */
const POOL_STRATEGY_PATH = "/api/codex-auth/pool-strategy";
const JSON_HEADERS = { "Content-Type": "application/json" } as const;

function among<T extends string>(domain: readonly T[]): (value: unknown) => value is T {
  const members = new Set<string>(domain);
  return (value: unknown): value is T => typeof value === "string" && members.has(value);
}

const isAccountPoolStrategy = among(STRATEGY_VALUES);
const isAccountPoolResetOrder = among(RESET_ORDER_VALUES);

/** Sticky limits are whole numbers inside the listener's range. */
function withinStickyRange(value: number): boolean {
  return value >= MIN_ACCOUNT_POOL_STICKY_LIMIT && value <= MAX_ACCOUNT_POOL_STICKY_LIMIT;
}

export function normalizeAccountPoolStrategy(value: unknown): AccountPoolStrategy {
  if (isAccountPoolStrategy(value)) return value;
  return DEFAULT_ACCOUNT_POOL_STRATEGY;
}

export function normalizeAccountPoolResetOrder(value: unknown): AccountPoolResetOrder {
  if (isAccountPoolResetOrder(value)) return value;
  return DEFAULT_ACCOUNT_POOL_RESET_ORDER;
}

export function normalizeAccountPoolStickyLimit(value: unknown): number {
  if (typeof value !== "number" || !Number.isInteger(value)) return DEFAULT_ACCOUNT_POOL_STICKY_LIMIT;
  return withinStickyRange(value) ? value : DEFAULT_ACCOUNT_POOL_STICKY_LIMIT;
}

/**
 * Strict draft parse for the sticky-limit input: plain digits, in range, or nothing.
 *
 * Separate from the normalizer because the two answer different questions. This one decides
 * whether what the operator typed may be sent at all, so a leading sign or a decimal point
 * is a rejection rather than a silent repair.
 */
export function parseAccountPoolStickyLimitDraft(value: string): number | null {
  const trimmed = value.trim();
  if (!/^\d+$/.test(trimmed)) return null;
  const parsed = Number(trimmed);
  return withinStickyRange(parsed) ? parsed : null;
}

export type PoolStrategyFetch = (input: string, init: RequestInit) => Promise<Response>;

/** A patch carries only the fields the operator actually moved. */
export interface PoolStrategyPatch {
  strategy?: AccountPoolStrategy;
  stickyLimit?: number;
  resetOrder?: AccountPoolResetOrder;
}

/** What the listener reports it stored, after normalization. */
export interface StoredPoolStrategy {
  strategy: AccountPoolStrategy;
  stickyLimit: number;
  resetOrder: AccountPoolResetOrder;
}

type WriteResult<Stored> = ({ ok: true } & Stored) | { ok: false };

export type PoolStrategyWrite = WriteResult<StoredPoolStrategy>;

/** The patch as the request body: absent fields stay absent rather than becoming null. */
function patchFields(patch: PoolStrategyPatch): Record<string, unknown> {
  const fields: Record<string, unknown> = {};
  if (patch.strategy !== undefined) fields.strategy = patch.strategy;
  if (patch.stickyLimit !== undefined) fields.stickyLimit = patch.stickyLimit;
  if (patch.resetOrder !== undefined) fields.resetOrder = patch.resetOrder;
  return fields;
}

function strategyRequest(apiBase: string, body: unknown): { url: string; init: RequestInit } {
  return {
    url: `${apiBase}${POOL_STRATEGY_PATH}`,
    init: { method: "PUT", headers: JSON_HEADERS, body: JSON.stringify(body) },
  };
}

/**
 * The triple the listener reports.
 *
 * A response is trusted only for the fields it actually carries: a listener that answers
 * with two of the three leaves the third at what the caller sent, which is what the server
 * was asked to keep.
 */
function storedStrategy(payload: unknown, requested: PoolStrategyPatch): StoredPoolStrategy {
  const reported = (payload ?? {}) as Record<string, unknown>;
  return {
    strategy: normalizeAccountPoolStrategy(reported.accountPoolStrategy ?? requested.strategy),
    stickyLimit: normalizeAccountPoolStickyLimit(reported.accountPoolStickyLimit ?? requested.stickyLimit),
    resetOrder: normalizeAccountPoolResetOrder(reported.accountPoolResetOrder ?? requested.resetOrder),
  };
}

/**
 * PUT the rotation patch and report the stored triple.
 *
 * An empty patch is refused before any request goes out, and a transport failure reads the
 * same as a non-2xx answer: the write did not land.
 */
export async function putCodexPoolStrategy(
  apiBase: string,
  patch: PoolStrategyPatch,
  send: PoolStrategyFetch = (input, init) => fetch(input, init),
): Promise<PoolStrategyWrite> {
  const body = patchFields(patch);
  if (Object.keys(body).length === 0) return { ok: false };
  try {
    const { url, init } = strategyRequest(apiBase, body);
    const response = await send(url, init);
    if (!response.ok) return { ok: false };
    return { ok: true, ...storedStrategy(await response.json(), patch) };
  } catch {
    return { ok: false };
  }
}
