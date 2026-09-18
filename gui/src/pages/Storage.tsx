/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useI18n, type Locale, type TFn } from "../i18n/shared";
import type { StorageReport } from "../components/storage-workspace/types";
import { DataSurfaceStatus } from "../components/data-surface";
import { ToastNotice } from "../ui";
import { scanCompleteness } from "./storage-report-view";
import { selectStorageTab } from "./storage-tab";
import { StorageTabStrip } from "./storage-tab-strip";
import {
  StorageCleanupPanel,
  StorageOverviewPanel,
  StorageQuarantinePanel,
} from "./storage-board-panels";
import { useStoragePage, useStorageTab } from "./use-storage-page";

type StorageProps = { apiBase: string };

/** The dot the meta line puts between the facts it reports. */
function MetaSeparator() {
  return <span className="storage-board-meta__sep" aria-hidden="true">·</span>;
}

/** The board head: where the scan looked is the reader's context, the rescan is their action. */
function StorageBoardHead({
  t,
  loading,
  onRefresh,
}: {
  t: TFn;
  loading: boolean;
  onRefresh: () => void;
}) {
  return (
    <header className="page-head">
      <h2 id="storage-page-title">{t("storage.title")}</h2>
      <div className="page-head-actions">
        <button type="button" className="btn storage-rescan" disabled={loading} onClick={onRefresh}>
          {t("storage.refresh")}
        </button>
      </div>
    </header>
  );
}

/**
 * What the board reports about the last scan.
 *
 * The scan directory, when it ran, and whether the walk finished are three separate facts about
 * one scan, so they read as one line; the completeness half is the only one that carries a tone.
 */
function StorageBoardMeta({ t, locale, data }: { t: TFn; locale: Locale; data: StorageReport }) {
  const partial = scanCompleteness(data.truncated) === "partial";
  const scannedAt = new Date(data.generatedAt).toLocaleString(locale);
  const completenessCopy = partial ? t("storage.scan.partial") : t("storage.scan.complete");
  return (
    <p className="storage-board-meta">
      <code className="mono storage-board-meta__home" title={data.codexHome}>{data.codexHome || "—"}</code>
      <MetaSeparator />
      <span>{t("storage.snapshot.lastScan")} {scannedAt}</span>
      <MetaSeparator />
      <span className={partial ? "storage-scan--partial" : undefined}>{completenessCopy}</span>
    </p>
  );
}

/**
 * The failure the board is showing, when a scan failed.
 *
 * The retry rides inside the alert rather than in the head: a failure that left nothing to render
 * has to say so where the reader is looking, and the head's rescan would not explain itself.
 */
function StorageScanAlert({
  t,
  error,
  offerRetry,
  onRetry,
}: {
  t: TFn;
  error: unknown;
  offerRetry: boolean;
  onRetry: () => void;
}) {
  return (
    <p className="storage-board-error" role="alert">
      {error instanceof Error ? error.message : t("storage.error")}
      {offerRetry && (
        <>
          {" "}
          <button type="button" className="btn btn-ghost btn-sm" onClick={onRetry}>{t("common.retry")}</button>
        </>
      )}
    </p>
  );
}

/** Scan feedback and scan state, above the panels so they survive a tab switch. */
function StorageBoardAlerts({
  t,
  scanFeedback,
  onClearScanFeedback,
  showError,
  error,
  failedCold,
  hasData,
  refreshing,
  showSkeleton,
  onRetry,
}: {
  t: TFn;
  scanFeedback: string | null;
  onClearScanFeedback: () => void;
  showError: boolean;
  error: unknown;
  failedCold: boolean;
  hasData: boolean;
  refreshing: boolean;
  showSkeleton: boolean;
  onRetry: () => void;
}) {
  const feedbackTone = scanFeedback === t("storage.error") ? "err" : "ok";
  const liveRefresh = hasData && refreshing && !showSkeleton;
  return (
    <>
      {scanFeedback && (
        <ToastNotice
          tone={feedbackTone}
          dismissLabel={t("common.close")}
          onDismiss={onClearScanFeedback}
        >
          {scanFeedback}
        </ToastNotice>
      )}
      {showError && (
        <StorageScanAlert
          t={t}
          error={error}
          offerRetry={failedCold && !hasData}
          onRetry={onRetry}
        />
      )}
      {liveRefresh && <DataSurfaceStatus live={!showError}>{t("storage.loading")}</DataSurfaceStatus>}
    </>
  );
}

export default function Storage({ apiBase }: StorageProps) {
  const { t, locale } = useI18n();
  const tab = useStorageTab();
  const page = useStoragePage(apiBase, t);
  const data = page.data;

  return (
    <div className="storage-board">
      <StorageBoardHead t={t} loading={page.loading} onRefresh={() => void page.refreshAll()} />
      <div className="storage-board-tabs">
        <StorageTabStrip tab={tab} onSelect={selectStorageTab} />
        {data && data.error === undefined && <StorageBoardMeta t={t} locale={locale} data={data} />}
      </div>
      <StorageBoardAlerts
        t={t}
        scanFeedback={page.scanFeedback}
        onClearScanFeedback={page.clearScanFeedback}
        showError={page.reportState.showError}
        error={page.reportState.error}
        failedCold={page.reportState.kind === "failed-cold"}
        hasData={Boolean(data) && data?.error === undefined}
        refreshing={page.reportState.refreshing}
        showSkeleton={page.reportState.showSkeleton}
        onRetry={page.refreshReport}
      />
      <StorageOverviewPanel
        active={tab === "overview"}
        t={t}
        locale={locale}
        showSkeleton={page.reportState.showSkeleton}
        reportFailed={page.reportFailed}
        showBody={page.showBody}
        data={data}
        onRetry={page.refreshReport}
      />
      <StorageCleanupPanel
        active={tab === "cleanup"}
        apiBase={apiBase}
        locale={locale}
        t={t}
        showSkeleton={page.reportState.showSkeleton}
        reportFailed={page.reportFailed}
        showBody={page.showBody}
        data={data}
        archivedCount={page.archivedCount}
        archivedBytes={page.archivedBytes}
        onDone={() => void page.refreshAll()}
        onRetry={page.refreshReport}
      />
      <StorageQuarantinePanel
        active={tab === "quarantine"}
        apiBase={apiBase}
        locale={locale}
        t={t}
        reloadToken={page.trashReloadToken}
        onDone={() => void page.refreshAll()}
      />
    </div>
  );
}