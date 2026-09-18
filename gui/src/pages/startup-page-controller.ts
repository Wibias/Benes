/**
 * The Control page's controller: one reducer for the page's slots, the read that fills them,
 * and the actions the shell and the board ask for.
 *
 * Every slot has one transition table instead of its own `useState` setter, so a slow peer
 * cannot half-update the page: a failed tray action restores the row it had, and a resolved
 * runtime read clears the pending placeholder in the same commit. The page composes this
 * model and renders; nothing here decides layout.
 */
import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react";
import { useDataSurface } from "../data-surface";
import { fetchCodexAppServerState, type AppServerStateOutcome } from "../listener-commands";
import { useCodexRestart } from "../use-codex-restart";
import { useI18n, type TFn } from "../i18n/shared.ts";
import {
  retireFailedInstallResult,
  type StartupInstallResult,
  type TrayAction,
} from "./startup-page-state.ts";
import {
  loadStartupSnapshot,
  postInstallAction,
  postTrayAction,
  readStartupPageCache,
  startupBoardToast,
  startupSurfaceFor,
  writeStartupPageCache,
} from "./startup-page-runtime.ts";
import type { StartupHealthData, StartupInstallAction, TrayStatusData } from "./startup-shared.ts";

/** The runtime-notice slot: the copy it will show, and whether it is still being read. */
type NoticeSlot = {
  pending: boolean;
  warning: string | null;
  fix: string | null;
};

type PageSlots = {
  tray: TrayStatusData | null;
  trayBusy: boolean;
  trayError: boolean;
  installBusy: StartupInstallAction | null;
  installResult: StartupInstallResult | null;
  restoreBusy: boolean;
  copied: string | null;
  notice: NoticeSlot;
};

type NoticeCopy = { warning: string | null; fix: string | null };

type PageEvent =
  | { kind: "read-started"; firstPaint: boolean }
  | { kind: "read-succeeded"; tray: TrayStatusData | null; trayError: boolean; notice: NoticeCopy | null }
  | { kind: "read-failed"; firstPaint: boolean }
  | { kind: "install-started"; action: StartupInstallAction }
  | { kind: "install-settled"; result: StartupInstallResult | null }
  | { kind: "install-retired"; health: StartupHealthData }
  | { kind: "restore-started" }
  | { kind: "restore-settled" }
  | { kind: "tray-started" }
  | { kind: "tray-succeeded"; tray: TrayStatusData }
  | { kind: "tray-failed" }
  | { kind: "copied"; command: string | null }
  | { kind: "dismissed" };

function idleSlots(cached: ReturnType<typeof readStartupPageCache>): PageSlots {
  return {
    tray: cached?.tray ?? null,
    trayBusy: false,
    trayError: false,
    installBusy: null,
    installResult: null,
    restoreBusy: false,
    copied: null,
    notice: { pending: !cached?.data, warning: cached?.warning ?? null, fix: cached?.fix ?? null },
  };
}

/** One transition per slot, so a partially applied read can never leave a stale twin behind. */
function reduceSlots(slots: PageSlots, event: PageEvent): PageSlots {
  switch (event.kind) {
    case "read-started":
      return event.firstPaint ? { ...slots, notice: { ...slots.notice, pending: true } } : slots;
    case "read-succeeded":
      return {
        ...slots,
        tray: event.tray,
        trayError: event.trayError,
        notice: {
          pending: false,
          warning: event.notice ? event.notice.warning : slots.notice.warning,
          fix: event.notice ? event.notice.fix : slots.notice.fix,
        },
      };
    case "read-failed":
      return event.firstPaint
        ? { ...slots, tray: null, trayError: true, notice: { pending: false, warning: null, fix: null } }
        : { ...slots, notice: { ...slots.notice, pending: false } };
    case "install-started":
      return { ...slots, installBusy: event.action, installResult: null };
    case "install-settled":
      return { ...slots, installBusy: null, installResult: event.result };
    case "install-retired":
      return { ...slots, installResult: retireFailedInstallResult(slots.installResult, event.health) };
    case "restore-started":
      return { ...slots, restoreBusy: true };
    case "restore-settled":
      return { ...slots, restoreBusy: false };
    case "tray-started":
      return { ...slots, trayBusy: true, trayError: false };
    case "tray-succeeded":
      return { ...slots, tray: event.tray, trayBusy: false, trayError: false };
    case "tray-failed":
      return { ...slots, tray: null, trayBusy: false, trayError: true };
    case "copied":
      return { ...slots, copied: event.command };
    case "dismissed":
      return { ...slots, installResult: null, copied: null };
  }
}

/** How long a copied command stays acknowledged in the toast. */
const COPY_ACK_MS = 1_600;
/** A stale diagnostic is re-asked on this cadence rather than waiting for the next mount. */
const STALE_RETRY_MS = 2_000;

export function useControlPage(apiBase: string, restartEpoch: number) {
  const { t } = useI18n();
  const cached = useMemo(() => readStartupPageCache(apiBase), [apiBase]);
  const [slots, dispatch] = useReducer(reduceSlots, cached, idleSlots);
  const [nonce, bumpNonce] = useReducer((value: number) => value + 1, 0);
  const [appServerState, setAppServerState] = useState<AppServerStateOutcome["state"]>(null);
  const paintedRef = useRef(Boolean(cached?.data));
  const mountedRef = useRef(true);

  const reloadAppServer = useCallback((signal?: AbortSignal) => {
    void fetchCodexAppServerState(apiBase, { signal }).then(outcome => {
      if (signal?.aborted || !mountedRef.current) return;
      setAppServerState(outcome.state);
    });
  }, [apiBase]);

  const codex = useCodexRestart(apiBase, { onSettled: () => reloadAppServer() });

  useEffect(() => {
    mountedRef.current = true;
    return () => { mountedRef.current = false; };
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    reloadAppServer(controller.signal);
    return () => controller.abort();
  }, [reloadAppServer, restartEpoch]);

  const readHealth = useCallback(async (signal: AbortSignal): Promise<StartupHealthData> => {
    const firstPaint = !paintedRef.current;
    dispatch({ kind: "read-started", firstPaint });
    try {
      const snapshot = await loadStartupSnapshot({ apiBase, signal, t });
      dispatch({ kind: "install-retired", health: snapshot.health });
      paintedRef.current = true;
      dispatch({
        kind: "read-succeeded",
        tray: snapshot.tray,
        trayError: snapshot.health.platform === "win32" ? snapshot.trayError : false,
        notice: snapshot.notice,
      });
      writeStartupPageCache(apiBase, {
        data: snapshot.health,
        tray: snapshot.tray,
        notice: snapshot.notice ?? undefined,
      });
      return snapshot.health;
    } catch (failure) {
      if (signal.aborted) throw failure;
      dispatch({ kind: "read-failed", firstPaint });
      throw failure;
    }
  }, [apiBase, t]);

  const resource = useDataSurface<StartupHealthData>(
    `startup-page:${apiBase}`,
    [apiBase],
    readHealth,
    { isEmpty: () => false, initialData: cached?.data ?? undefined },
  );
  const loadState = resource.state;
  const data = loadState.data ?? cached?.data ?? null;
  const failed = Boolean(data?.diagnosticStale) || loadState.showError;
  const refresh = resource.refresh;

  useEffect(() => {
    if (!data?.diagnosticStale) return;
    const timer = window.setTimeout(refresh, STALE_RETRY_MS);
    return () => window.clearTimeout(timer);
  }, [data, refresh]);

  const copyCommand = useCallback(async (command: string) => {
    try {
      await navigator.clipboard.writeText(command);
      dispatch({ kind: "copied", command });
      window.setTimeout(() => dispatch({ kind: "copied", command: null }), COPY_ACK_MS);
    } catch {
      dispatch({ kind: "copied", command: null });
    }
  }, []);

  const runTrayAction = useCallback(async (action: TrayAction) => {
    dispatch({ kind: "tray-started" });
    try {
      dispatch({ kind: "tray-succeeded", tray: await postTrayAction(apiBase, action) });
    } catch {
      dispatch({ kind: "tray-failed" });
    }
  }, [apiBase]);

  const runInstallAction = useCallback(async (action: StartupInstallAction, opts?: { repair?: boolean }) => {
    const repair = opts?.repair === true;
    dispatch({ kind: "install-started", action });
    try {
      await postInstallAction(apiBase, action, repair);
      dispatch({ kind: "install-settled", result: { kind: "success", action, repair } });
      refresh();
    } catch (failure) {
      dispatch({
        kind: "install-settled",
        result: {
          kind: "error",
          action,
          repair,
          detail: failure instanceof Error ? failure.message : String(failure),
          forLocalRouting: data?.localRoutingDependency === true,
        },
      });
    }
  }, [apiBase, data?.localRoutingDependency, refresh]);

  const restoreNative = useCallback(async () => {
    const command = data?.commands.restoreNative;
    if (!command || !window.confirm(t("control.restoreNativeHint"))) return;
    dispatch({ kind: "restore-started" });
    try {
      await copyCommand(command);
    } finally {
      dispatch({ kind: "restore-settled" });
    }
  }, [copyCommand, data?.commands.restoreNative, t]);

  const refreshBoard = useCallback(() => {
    refresh();
    bumpNonce();
  }, [refresh]);

  return {
    t,
    apiBase,
    nonce,
    data,
    loadState,
    failed,
    refreshing: loadState.refreshing,
    surface: startupSurfaceFor({ loadState, data, failed }),
    slots,
    appServerState,
    codex,
    toast: startupBoardToast(t, slots.installResult, slots.copied),
    refresh,
    refreshBoard,
    copyCommand,
    runTrayAction,
    runInstallAction,
    restoreNative,
    dismissToast: () => dispatch({ kind: "dismissed" }),
  };
}

/** What the page's presentation components render from. */
export type ControlPageModel = ReturnType<typeof useControlPage>;

/** The localized translator the page's own copy needs. */
export type ControlPageT = TFn;
