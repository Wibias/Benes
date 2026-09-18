/** Benes dashboard client for the Go proxy (`internal/server`). */
import { normalizeAccountPriority } from "./account-priority.ts";
import { accountQuotaFromCodexAccounts, type AccountQuota } from "./codex-quota-utils.ts";
import { accountNeedsReauth } from "./oauth-health-display.ts";
import { extractAutoSwitchThresholdPayload } from "./codex-auto-switch.ts";
import { useKeyedClientResource } from "./client-resource.ts";
import { usageSummary30dResourceKey } from "./usage-summary-resource.ts";
import {
  codexAccountMutationCompletion,
  type CodexAccountMutationCompletion,
} from "./codex-account-mutation.ts";

/**
 * One Codex account as every pool surface renders it.
 *
 * `id`/`email` are operator-visible identity; `logLabel` is the stable non-PII key shared
 * with Logs and per-account usage aggregation. The facets stay separate so a surface can
 * depend on just the part it reads.
 */
interface AccountIdentity {
  id: string;
  email: string;
  alias?: string;
  logLabel?: string;
  plan?: string;
}

interface AccountRoutingFacts {
  /** Required, not optional: the API always distinguishes the app-login row. */
  isMain: boolean;
  /** Persisted routing exclusion. Paused accounts remain visible but cannot be selected. */
  paused: boolean;
  /** Selection order; higher is used earlier. Always present, 0 when unset. */
  priority: number;
  hasCredential: boolean;
}

interface AccountHealthFacts {
  needsReauth?: boolean;
  health?: { status: "healthy" | "cooldown" | "reauth_required" | "warning"; reason?: string; until?: string };
  healthLabel?: string;
  healthSummary?: string;
  healthAction?: string;
}

interface AccountUsageFacts {
  usage30d?: {
    totalTokens: number;
    estimatedCostUsd?: number;
    usageCoverageRatio: number;
  };
}

export interface CodexAccountEntry extends AccountIdentity, AccountRoutingFacts, AccountHealthFacts, AccountUsageFacts {
  quota: AccountQuota | null;
}

export type CodexAccountLoadState = "loading" | "ready" | "error";

export type CodexAccountActionResult<T extends object = Record<never, never>> =
  | ({ ok: true } & T)
  | { ok: false; reason: "busy" | "request" | "reload" };

/** Opaque lease. Two callers pausing for the same reason must not cancel each other. */
export type PauseToken = { readonly __brand: "codex-pool-pause" };

/** The app-login row's id sentinel on every account endpoint. */
export const MAIN_ACCOUNT_ID = "__main__";

/** The status/reason vocabulary the account rows carry, for callers that narrow it. */
export type CodexAccountHealth = NonNullable<CodexAccountEntry["health"]>;

/**
 * Normalize one `/accounts` row for rendering.
 *
 * Selection order is required downstream (badge, select), so a payload without it must
 * not render a NaN order on every card. The app-login row's log label is forced to the
 * shared `main` key so usage aggregation lines up across surfaces.
 */
export function normalizeAccountRow(raw: CodexAccountEntry): CodexAccountEntry {
  const logLabel = raw.isMain ? "main" : raw.logLabel;
  return {
    ...raw,
    quota: accountQuotaFromCodexAccounts(raw.quota),
    ...(logLabel ? { logLabel } : {}),
    priority: normalizeAccountPriority(raw.priority),
  };
}

/** Normalize a whole `/accounts` payload, tolerating a missing or malformed list. */
export function normalizeAccountList(payload: unknown): CodexAccountEntry[] {
  const rows = (payload as { accounts?: unknown } | null | undefined)?.accounts;
  if (!Array.isArray(rows)) return [];
  return rows.map(row => normalizeAccountRow(row as CodexAccountEntry));
}

/**
 * The active id a payload reports: a string, `null`, or `undefined` when the field is
 * absent entirely. Callers must distinguish "no opinion" from "nothing is active".
 */
export function readActiveId(payload: unknown): string | null | undefined {
  if (payload === null || typeof payload !== "object") return undefined;
  if (!Object.hasOwn(payload, "activeCodexAccountId")) return undefined;
  const value = (payload as { activeCodexAccountId?: unknown }).activeCodexAccountId;
  return typeof value === "string" ? value : null;
}

/** Paused ids plus the reported count from a bulk-pause response. */
export function readPausedResult(payload: unknown): { ids: Set<string>; count: number | undefined } {
  const row = (payload !== null && typeof payload === "object" ? payload : {}) as {
    pausedAccountIds?: unknown;
    pausedCount?: unknown;
  };
  const ids = new Set<string>(Array.isArray(row.pausedAccountIds) ? (row.pausedAccountIds as string[]) : []);
  const count = typeof row.pausedCount === "number" ? row.pausedCount : undefined;
  return { ids, count };
}

/** The stored order a priority write reports, or the requested value when absent. */
export function readStoredPriority(payload: unknown, requested: number | null): number {
  const row = (payload !== null && typeof payload === "object" ? payload : {}) as { priority?: unknown };
  return normalizeAccountPriority(row.priority ?? requested);
}

/** Rewrite the rows a matcher selects, leaving every other row identical. */
function patchRows(
  rows: CodexAccountEntry[],
  selects: (row: CodexAccountEntry) => boolean,
  patch: (row: CodexAccountEntry) => CodexAccountEntry,
): CodexAccountEntry[] {
  return rows.map(row => (selects(row) ? patch(row) : row));
}

/** Selector for one account id; the `__main__` sentinel matches the app-login row. */
function selectsAccount(id: string): (row: CodexAccountEntry) => boolean {
  return row => row.id === id || (id === MAIN_ACCOUNT_ID && row.isMain);
}

/** Flip one row's paused flag. */
export function withPausedFlag(
  accounts: CodexAccountEntry[],
  id: string,
  paused: boolean,
): CodexAccountEntry[] {
  return patchRows(accounts, selectsAccount(id), row => ({ ...row, paused }));
}

/** Apply a bulk-pause response to the visible rows. */
export function withPausedIds(
  accounts: CodexAccountEntry[],
  pausedIds: ReadonlySet<string>,
): CodexAccountEntry[] {
  if (pausedIds.size === 0) return accounts;
  const selects = pausedIds.has(MAIN_ACCOUNT_ID)
    ? (row: CodexAccountEntry) => pausedIds.has(row.id) || row.isMain
    : (row: CodexAccountEntry) => pausedIds.has(row.id);
  return patchRows(accounts, selects, row => ({ ...row, paused: true }));
}

/** Write one row's confirmed selection order. */
export function withPriority(
  accounts: CodexAccountEntry[],
  id: string,
  priority: number,
): CodexAccountEntry[] {
  return patchRows(accounts, selectsAccount(id), row => ({ ...row, priority }));
}

/**
 * Whether an incoming `/active` read may move the visible active id.
 *
 * A read that disagrees with an accepted-but-unreconciled id is stale on this pass; the
 * next load reconciles. Returning the decision instead of mutating keeps the ref update
 * and the paint next to each other.
 */
export function reconcileActiveRead(
  serverActiveId: string | null,
  pending: { id: string | null } | null,
): { accept: boolean; nextPending: { id: string | null } | null } {
  if (pending !== null && serverActiveId !== pending.id) return { accept: false, nextPending: pending };
  return { accept: true, nextPending: null };
}

/** The row whose reauth state decides the pool's attention badge. */
export function activeReauthTarget(
  accounts: CodexAccountEntry[],
  activeId: string | null,
): CodexAccountEntry | undefined {
  const poolActive = activeId !== null && activeId !== MAIN_ACCOUNT_ID
    ? accounts.find(account => account.id === activeId)
    : undefined;
  return poolActive ?? accounts.find(account => account.isMain);
}

/**
 * Non-rendered sequencing state: everything the hook must read synchronously from async
 * completions, kept in one record so the authority model is explicit rather than a set of
 * refs that grew one at a time.
 */
export function createPoolAuthority() {
  return {
    /** Monotonic load id; only the newest generation may commit state. */
    generation: 0,
    /** Accepted-but-unreconciled active id from a switch or pause response. */
    pendingActive: null as { id: string | null } | null,
    /** Subscribers that must be told the outcome of every `/active` read. */
    observers: new Set<{ beginActiveRead(): number; acceptActiveRead(value: unknown, startedRevision: number): void; rejectActiveRead(): void }>(),
    /** Outstanding pause leases that suspend background polling. */
    pauseTokens: new Set<PauseToken>(),
    /** In-flight switch target, or null. */
    switchTarget: null as string | null,
    /** In-flight pause mutation, if any. */
    pause: null as { kind: "bulk" } | { kind: "one"; id: string } | null,
    /** In-flight selection-order mutation, if any. */
    priority: null as { id: string } | null,
    /** Last successful `/active` payload, for surfaces that mount late. */
    lastActive: null as { value: unknown } | null,
    /** Whether any load has succeeded; an empty success still counts. */
    hasLoaded: false,
    /** Which apiBase the mount effect already issued its first load for. */
    mountedFor: null as string | null,
  };
}

export type PoolAuthority = ReturnType<typeof createPoolAuthority>;

/** Canonical rendered pool state. One record so a paint cannot mix load generations. */
export interface PoolSnapshot {
  accounts: CodexAccountEntry[];
  activeId: string | null;
  pinnedId: string | null;
  phase: CodexAccountLoadState;
  firstAttemptSettled: boolean;
}

/** What the pool is doing right now; drives spinners and disabled controls. */
export interface PoolActivity {
  inflight: number;
  switchTarget: string | null;
  pause: { kind: "bulk" } | { kind: "one"; id: string } | null;
  priority: { id: string } | null;
  pauseLeases: number;
}

/** Shared refusal results, so every operation reports failure the same way. */
export const BUSY_RESULT = { ok: false, reason: "busy" } as const;
export const REQUEST_RESULT = { ok: false, reason: "request" } as const;

/** `busy` means another operation owned the gate; `rejected` means the request failed. */
export type PoolMutationStatus = "accepted" | "rejected" | "busy";

/**
 * One mutation protocol: claim the gate, run the request, always release.
 *
 * Every pool write goes through here, so "who may run at the same time", "what clears the
 * in-flight marker", and "what a transport failure means" are answered once rather than
 * re-derived per endpoint. Overlapping operations are refused by the gate rather than by
 * a second check at the call site.
 */
export async function runPoolMutation(
  claim: () => boolean,
  release: () => void,
  send: () => Promise<{ ok: boolean; payload: unknown }>,
): Promise<{ status: PoolMutationStatus; payload: unknown }> {
  if (!claim()) return { status: "busy", payload: null };
  try {
    const outcome = await send();
    return { status: outcome.ok ? "accepted" : "rejected", payload: outcome.payload };
  } catch {
    return { status: "rejected", payload: null };
  } finally {
    release();
  }
}


/** Cheap re-reads while credentialed rows are still waiting on background quota. */
export const QUOTA_FILL_DELAYS_MS = [350, 900, 2000];

/** Last timeout before an accounts/active attempt is abandoned rather than pinning the poll. */
export const LOAD_TIMEOUT_MS = 20_000;

/**
 * In-memory last-good snapshot per listener.
 *
 * Deliberately not sessionStorage: the rows carry operator emails and account ids, so they
 * stay in this tab's heap instead of touching browser storage.
 */
const lastGoodByListener = new Map<string, { accounts: CodexAccountEntry[]; activeId: string | null }>();

export function readGoodPool(apiBase: string): { accounts: CodexAccountEntry[]; activeId: string | null } | undefined {
  return lastGoodByListener.get(apiBase);
}

export function rememberGoodPool(
  apiBase: string,
  accounts: CodexAccountEntry[] | null,
  activeId: string | null | undefined,
): void {
  const prior = lastGoodByListener.get(apiBase);
  lastGoodByListener.set(apiBase, {
    accounts: accounts ?? prior?.accounts ?? [],
    activeId: activeId !== undefined ? activeId : (prior?.activeId ?? null),
  });
}

/** Seed state from the last good snapshot, normalizing quota shapes for rendering. */
export function normalizeAccountRowList(rows: CodexAccountEntry[]): CodexAccountEntry[] {
  return rows.map(normalizeAccountRow);
}

/** What a switch response means for the visible pool. */
export function planSwitchAccepted(
  payload: unknown,
  requestedId: string | null,
): { activeId: string | null; pinnedId: string } {
  const selectedId = readActiveId(payload) ?? requestedId ?? null;
  return { activeId: selectedId, pinnedId: selectedId ?? MAIN_ACCOUNT_ID };
}

/** What a single-account pause response means: its flag, and any active-id change. */
export function planPauseAccepted(
  payload: unknown,
  id: string,
  paused: boolean,
): { accounts: (rows: CodexAccountEntry[]) => CodexAccountEntry[]; activeId: string | null | undefined } {
  return {
    accounts: rows => withPausedFlag(rows, id, paused),
    activeId: readActiveId(payload),
  };
}

/** What a bulk-pause response means: which rows pause, and how many to report. */
export function planPauseExhaustedAccepted(payload: unknown): {
  accounts: (rows: CodexAccountEntry[]) => CodexAccountEntry[];
  pausedIds: Set<string>;
  pausedCount: number;
  activeId: string | null | undefined;
} {
  const paused = readPausedResult(payload);
  return {
    accounts: rows => withPausedIds(rows, paused.ids),
    pausedIds: paused.ids,
    pausedCount: paused.count ?? paused.ids.size,
    activeId: readActiveId(payload),
  };
}

/**
 * A surface that wants the pool's `/active` reads.
 *
 * The threshold and rotation settings arrive on the same response as the account list, so
 * those surfaces subscribe here instead of being handed into `load()`: background polls
 * have no caller to pass one, and two delivery paths would notify the same observer twice
 * per explicit load.
 */
export interface CodexAccountLoadObserver {
  beginActiveRead(): number;
  acceptActiveRead(value: unknown, startedRevision: number): void;
  rejectActiveRead(): void;
}

/** The observers a load must tell about its `/active` outcome, snapshotted up front. */
export type PoolObserverRegistry = Set<CodexAccountLoadObserver>;

/**
 * Start one `/active` read for every subscriber.
 *
 * The registry is snapshotted so an unsubscribe mid-flight cannot desync the
 * begin/accept pairing, and the returned map carries each observer's start revision.
 */
export function beginObserverReads(
  registry: PoolObserverRegistry,
): { observers: CodexAccountLoadObserver[]; startedRevisions: Map<CodexAccountLoadObserver, number> } {
  const observers = [...registry];
  const startedRevisions = new Map<CodexAccountLoadObserver, number>();
  for (const observer of observers) startedRevisions.set(observer, observer.beginActiveRead());
  return { observers, startedRevisions };
}

export function acceptObserverReads(
  read: { observers: CodexAccountLoadObserver[]; startedRevisions: Map<CodexAccountLoadObserver, number> },
  payload: unknown,
): void {
  for (const observer of read.observers) {
    observer.acceptActiveRead(payload, read.startedRevisions.get(observer) ?? 0);
  }
}

export function rejectObserverReads(
  read: { observers: CodexAccountLoadObserver[]; startedRevisions: Map<CodexAccountLoadObserver, number> },
): void {
  for (const observer of read.observers) observer.rejectActiveRead();
}

/** Whether the pool's current account needs reauthentication before it is usable again. */
export function poolActiveNeedsReauth(accounts: CodexAccountEntry[], activeId: string | null): boolean {
  const target = activeReauthTarget(accounts, activeId);
  return !target?.paused && accountNeedsReauth(target);
}

/** The auto-switch threshold carried by an `/active` payload, or undefined. */
export function readThresholdFromActive(payload: unknown): unknown {
  return extractAutoSwitchThresholdPayload(payload);
}

/** What a delete response said happened to the removed account. */
export function readRemovalCompletion(payload: unknown): CodexAccountMutationCompletion {
  return codexAccountMutationCompletion(payload);
}

/**
 * The 30-day per-account usage summary, cached per listener by the shared resource layer.
 *
 * It lives here with the rest of the pool's server reads so the hook only composes state.
 */
export function usePoolUsage(
  api: ReturnType<typeof createPoolApi>,
  apiBase: string,
  enabled: boolean,
) {
  return useKeyedClientResource<PoolUsageSummary>(
    usageSummary30dResourceKey(apiBase),
    [apiBase],
    async (signal) => {
      const response = await api.readUsage(signal);
      if (!response.ok) throw new Error("account usage load failed");
      return response.payload as PoolUsageSummary;
    },
    { enabled },
  );
}





/** Per-account usage rows as `/api/usage?range=30d` reports them. */
export interface PoolUsageRow {
  accountLogLabel: string;
  totalTokens: number;
  estimatedCostUsd?: number;
  usageCoverageRatio: number;
}

export interface PoolUsageSummary {
  accounts?: PoolUsageRow[];
}

/** Attach usage to the rows whose log label matches, leaving the rest untouched. */
export function withUsage(
  accounts: CodexAccountEntry[],
  summary: PoolUsageSummary | undefined,
): CodexAccountEntry[] {
  const byLabel = new Map((summary?.accounts ?? []).map(row => [row.accountLogLabel, row] as const));
  if (byLabel.size === 0) return accounts;
  return accounts.map(row => {
    const logLabel = row.isMain ? "main" : row.logLabel;
    const usage = logLabel ? byLabel.get(logLabel) : undefined;
    return usage ? { ...row, usage30d: usage } : row;
  });
}


/** A response body, or `{}` when the endpoint sent nothing parseable. */
function readPayload(response: Response): Promise<unknown> {
  return response.json().catch(() => ({}));
}

/** One request against the pool API, normalized to `{ ok, payload }`. */
export interface PoolResponse {
  ok: boolean;
  payload: unknown;
}

const JSON_HEADERS = { "Content-Type": "application/json" } as const;

function putJson(body?: unknown): RequestInit {
  return body === undefined
    ? { method: "PUT", headers: JSON_HEADERS }
    : { method: "PUT", headers: JSON_HEADERS, body: JSON.stringify(body) };
}

/**
 * Every Codex pool endpoint, so the URL, method, header and body shapes are declared in
 * one place and callers only deal in `{ ok, payload }`. The hook sequences these; it does
 * not spell request shapes.
 */
export function createPoolApi(apiBase: string) {
  const send = async (path: string, init: RequestInit = {}): Promise<PoolResponse> => {
    const response = await fetch(`${apiBase}${path}`, init);
    return { ok: response.ok, payload: await readPayload(response) };
  };
  return {
    listAccounts: (refreshQuota: boolean, signal: AbortSignal) => send(
      refreshQuota ? "/api/codex-auth/accounts?refresh=1" : "/api/codex-auth/accounts",
      { signal },
    ),
    readActive: (signal: AbortSignal) => send("/api/codex-auth/active", { signal }),
    readUsage: (signal: AbortSignal) => send("/api/usage?range=30d", { signal }),
    pinAccount: (accountId: string | null) => send("/api/codex-auth/active", putJson({ accountId })),
    setPaused: (id: string, paused: boolean) => send("/api/codex-auth/accounts/pause", putJson({ id, paused })),
    setPriority: (id: string, priority: number | null) => send("/api/codex-auth/accounts/priority", putJson({ id, priority })),
    setAlias: (id: string, alias: string) => send("/api/codex-auth/accounts/alias", putJson({ id, alias })),
    pauseExhausted: () => send("/api/codex-auth/accounts/pause-exhausted", putJson()),
    remove: (id: string) => send(`/api/codex-auth/accounts?id=${encodeURIComponent(id)}`, { method: "DELETE" }),
  };
}


