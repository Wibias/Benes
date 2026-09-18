import { useCallback, useEffect, useRef, useState } from "react";
import type { StorageReport } from "../components/storage-workspace/types";
import { useDataSurface } from "../data-surface";
import { readJsonIfOk } from "../fetch-json";
import { readSessionListCache, writeSessionListCache } from "../session-list-cache";
import type { TFn } from "../i18n/shared";
import { archivedBucket } from "./storage-report-view";
import { readStorageTab, type StorageTab } from "./storage-tab";

/** Local-storage key the last accepted scan is kept under. */
function reportCacheKey(apiBase: string): string {
  return `benes.storage.report.v1:${apiBase}`;
}

/** In-memory resource key the scan poll publishes to. */
function reportResourceKey(apiBase: string): string {
  return `storage-report:${apiBase}`;
}

/** Address-bar events a Storage tab arrives on: the in-app link and the browser's own history. */
const TAB_EVENTS = ["hashchange", "popstate"] as const;

/** The Storage tab the address bar addresses, kept in step with links and with back/forward. */
export function useStorageTab(): StorageTab {
  const [tab, setTab] = useState<StorageTab>(readStorageTab);
  useEffect(() => {
    const syncFromHash = () => setTab(readStorageTab());
    for (const event of TAB_EVENTS) window.addEventListener(event, syncFromHash);
    return () => {
      for (const event of TAB_EVENTS) window.removeEventListener(event, syncFromHash);
    };
  }, []);
  return tab;
}

/** Read one scan from the listener; a refused or unreadable response reads as "no scan". */
async function readStorageReport(apiBase: string, signal: AbortSignal): Promise<StorageReport | null> {
  const response = await fetch(`${apiBase}/api/storage`, { signal });
  const report = await readJsonIfOk<StorageReport>(response);
  return report ?? null;
}

/** An empty scan found no files; a failed scan is not empty, it is a failure. */
function scanIsEmpty(report: StorageReport): boolean {
  return report.total.fileCount === 0 && report.error === undefined;
}

/**
 * The storage scan the board renders.
 *
 * Only a rescan the reader asked for reports back — a background poll keeps quiet — and a failed
 * scan surfaces the board's own copy rather than whatever the transport said, because the reader
 * can act on "storage scan failed" and not on a transport status.
 */
export function useStoragePage(apiBase: string, t: TFn) {
  const cacheKey = reportCacheKey(apiBase);
  const cachedReport = readSessionListCache<StorageReport>(cacheKey);
  const [trashReloadToken, setTrashReloadToken] = useState(0);
  const [scanFeedback, setScanFeedback] = useState<string | null>(null);
  const manualRefresh = useRef(false);

  const loadReport = useCallback(async (signal: AbortSignal): Promise<StorageReport> => {
    const manual = manualRefresh.current;
    if (manual) manualRefresh.current = false;
    const failureCopy = t("storage.error");
    try {
      const report = await readStorageReport(apiBase, signal);
      if (report === null) throw new Error(failureCopy);
      writeSessionListCache(cacheKey, report);
      if (manual) setScanFeedback(t("storage.rescanned"));
      return report;
    } catch (error) {
      if (manual) setScanFeedback(failureCopy);
      if (signal.aborted) throw error;
      throw new Error(failureCopy, { cause: error });
    }
  }, [apiBase, cacheKey, t]);

  const reportResource = useDataSurface<StorageReport>(
    reportResourceKey(apiBase),
    [apiBase],
    loadReport,
    { isEmpty: scanIsEmpty },
  );
  const { state: reportState, refresh: refreshReport } = reportResource;
  const data = reportState.data ?? cachedReport;
  const refreshAll = useCallback(() => {
    setScanFeedback(null);
    manualRefresh.current = true;
    refreshReport();
    setTrashReloadToken(token => token + 1);
  }, [refreshReport]);

  const archived = archivedBucket(data);
  const reportFailed = data?.error !== undefined;
  const loading = reportState.refreshing || (reportState.showSkeleton && !data);
  return {
    data,
    reportState,
    loading,
    scanFeedback,
    clearScanFeedback: () => setScanFeedback(null),
    trashReloadToken,
    archivedCount: archived?.fileCount ?? 0,
    archivedBytes: archived?.bytes ?? 0,
    reportFailed,
    showBody: Boolean(data) && !reportFailed,
    refreshReport,
    refreshAll,
  };
}