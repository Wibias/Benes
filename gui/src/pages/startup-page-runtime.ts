/**
 * Control page runtime: the listener reads and writes the page performs, plus the
 * sessionStorage seed it paints from. `Startup.tsx` keeps the React state; this module
 * keeps the transport and the cache, so both can be exercised without a renderer.
 */
import type { TFn } from "../i18n/shared.ts";
import { readSessionListCache, writeSessionListCache } from "../session-list-cache.ts";
import {
  deriveCodexRuntimeNotice,
  installResultMessageKey,
  type CodexRuntimeSettings,
  type StartupInstallResult,
  type TrayAction,
} from "./startup-page-state.ts";
import {
  isTrayStatusData,
  parseStartupHealthData,
  type StartupHealthData,
  type StartupInstallAction,
  type TrayStatusData,
} from "./startup-shared.ts";

export type StartupPageCache = {
  data: StartupHealthData;
  warning: string | null;
  fix: string | null;
  tray: TrayStatusData | null;
};

/** `notice: undefined` means "the runtime settings could not be read — keep what we had". */
export type StartupPageCachePatch = {
  data: StartupHealthData;
  tray?: TrayStatusData | null;
  notice?: { warning: string | null; fix: string | null } | null;
};

export type StartupSnapshot = {
  health: StartupHealthData;
  tray: TrayStatusData | null;
  trayError: boolean;
  notice: { warning: string | null; fix: string | null } | null;
};

type FetchLike = typeof fetch;

const CACHE_PREFIX = "benes.startup.page.v1:";

export function startupPageCacheKey(apiBase: string): string {
  return `${CACHE_PREFIX}${apiBase}`;
}

export function readStartupPageCache(apiBase: string): StartupPageCache | null {
  return readSessionListCache<StartupPageCache>(startupPageCacheKey(apiBase));
}

function carried<T>(patch: T | undefined, previous: T | null | undefined): T | null {
  return patch === undefined ? previous ?? null : patch;
}

/**
 * One write path for the seed. A patch that omits a field keeps the stored value, while an
 * explicit `null` is authoritative — that is how a resolved runtime notice clears a warning
 * a previous read had cached.
 */
export function writeStartupPageCache(apiBase: string, patch: StartupPageCachePatch): StartupPageCache {
  const previous = readStartupPageCache(apiBase);
  const notice = patch.notice;
  const entry: StartupPageCache = {
    data: patch.data,
    warning: carried(notice === undefined ? undefined : notice?.warning, previous?.warning),
    fix: carried(notice === undefined ? undefined : notice?.fix, previous?.fix),
    tray: carried(patch.tray, previous?.tray),
  };
  writeSessionListCache(startupPageCacheKey(apiBase), entry);
  return entry;
}
type RuntimeRead = { readFailed: boolean; runtime: CodexRuntimeSettings | undefined };

/** A settings read that fails must not fabricate a notice; an absent field clears one. */
async function readCodexRuntime(
  apiBase: string,
  signal: AbortSignal,
  fetchImpl: FetchLike,
): Promise<RuntimeRead> {
  try {
    const response = await fetchImpl(`${apiBase}/api/settings`, { signal });
    if (!response.ok) return { readFailed: true, runtime: undefined };
    const body = await response.json() as { codexRuntime?: CodexRuntimeSettings };
    return { readFailed: false, runtime: body.codexRuntime };
  } catch {
    return { readFailed: true, runtime: undefined };
  }
}

async function readTrayStatus(
  apiBase: string,
  signal: AbortSignal,
  fetchImpl: FetchLike,
  platform: string,
): Promise<{ tray: TrayStatusData | null; error: boolean }> {
  if (platform !== "win32") return { tray: null, error: false };
  try {
    const response = await fetchImpl(`${apiBase}/api/windows-tray`, { signal });
    if (!response.ok) throw new Error("tray status failed");
    const body = await response.json() as unknown;
    if (!isTrayStatusData(body)) throw new Error("invalid tray status");
    return { tray: body, error: false };
  } catch {
    return { tray: null, error: true };
  }
}

/**
 * Health first: it decides whether the host even has a tray and what the routing state is.
 * The runtime settings read starts alongside it because the tray read cannot begin until the
 * platform is known.
 */
export async function loadStartupSnapshot(input: {
  apiBase: string;
  signal: AbortSignal;
  t: TFn;
  fetchImpl?: FetchLike;
}): Promise<StartupSnapshot> {
  const { apiBase, signal, t } = input;
  const fetchImpl = input.fetchImpl ?? fetch;
  const runtimeRead = readCodexRuntime(apiBase, signal, fetchImpl);

  const response = await fetchImpl(`${apiBase}/api/startup-health`, { signal });
  if (!response.ok) throw new Error("fetch failed");
  const health = parseStartupHealthData(await response.json() as unknown);
  if (!health) throw new Error("invalid startup health");

  const trayRead = readTrayStatus(apiBase, signal, fetchImpl, health.platform);
  const [settings, trayResult] = await Promise.all([runtimeRead, trayRead]);
  return {
    health,
    tray: trayResult.tray,
    trayError: trayResult.error,
    notice: settings.readFailed ? null : deriveCodexRuntimeNotice(settings.runtime, t, health.platform),
  };
}

export async function postTrayAction(
  apiBase: string,
  action: TrayAction,
  fetchImpl: FetchLike = fetch,
): Promise<TrayStatusData> {
  const response = await fetchImpl(`${apiBase}/api/windows-tray`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ action }),
  });
  if (!response.ok) throw new Error("tray action failed");
  const body = await response.json() as { status?: unknown };
  if (!isTrayStatusData(body.status)) throw new Error("invalid tray action status");
  return body.status;
}

/**
 * The listener reports refusal in `error`; anything else is a transport failure. Either way
 * the caller shows the message, so both carry the reason the user needs.
 */
export async function postInstallAction(
  apiBase: string,
  action: StartupInstallAction,
  repair: boolean,
  fetchImpl: FetchLike = fetch,
): Promise<void> {
  const response = await fetchImpl(`${apiBase}/api/startup-action`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ action, repair }),
  });
  if (response.ok) return;
  const body = await response.json().catch(() => null) as { error?: unknown } | null;
  throw new Error(typeof body?.error === "string" ? body.error.trim() : "installation failed");
}
export type BoardToast = { tone: "ok" | "err"; text: string };

/**
 * The board reports one thing at a time: a finished install outranks a copied command, and
 * an idle page says nothing. A failed install carries the listener's reason, because the
 * page has no other place to show it.
 */
export function startupBoardToast(
  t: TFn,
  installResult: StartupInstallResult | null,
  copied: string | null,
): BoardToast | null {
  if (installResult) {
    return installResult.kind === "success"
      ? { tone: "ok", text: t(installResultMessageKey(installResult)) }
      : { tone: "err", text: `${t("startup.installFailed")} ${installResult.detail ?? ""}` };
  }
  return copied ? { tone: "ok", text: t("startup.copied") } : null;
}
/** What the load state the data surface reports, and the row it may carry. */
export type StartupLoadState = {
  showSkeleton: boolean;
  kind: string;
  refreshing: boolean;
  showError: boolean;
  error?: unknown;
};

export type StartupSurface =
  | { kind: "loading" }
  | { kind: "cold-failure"; reason: string | null }
  | { kind: "board"; unresolved: boolean; readFailed: boolean };

/**
 * Which surface the health slot shows, decided from the read state and the row it carries.
 *
 * The order is the product behaviour: a cold read has nothing to show but a skeleton, a cold
 * failure has nothing to show but its retry, and a row without its command block is still not a
 * board. On the board, `unresolved` marks rows whose staleness the page must announce and pass
 * to the board, while `readFailed` marks a board whose latest refresh failed and needs the
 * error notice above it; the two can both be true and are reported separately.
 */
export function startupSurfaceFor(input: {
  loadState: StartupLoadState;
  data: StartupHealthData | null;
  failed: boolean;
}): StartupSurface {
  const { loadState, data, failed } = input;
  if (loadState.kind === "failed-cold") {
    const reason = loadState.error instanceof Error ? loadState.error.message : null;
    return { kind: "cold-failure", reason };
  }
  if (!data?.commands) return { kind: "loading" };
  return { kind: "board", unresolved: failed, readFailed: loadState.showError };
}