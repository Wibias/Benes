import type { Locale, TFn } from "../i18n/shared";
import StorageWorkspace, { type StorageReport } from "../components/storage-workspace/StorageWorkspace";
import { DataSurfaceSkeleton } from "../components/data-surface";
import { StorageCleanupView } from "./storage-cleanup-card";
import { QuarantineTrashPanel } from "./storage-quarantine-panel";
import { storageBoardChrome } from "./storage-report-view";
import { storagePanelDomId, storageTabDomId } from "./storage-tab";

function StorageReportIssue({
  t, chrome, onRetry, skeletonRows,
}: {
  t: TFn;
  chrome: ReturnType<typeof storageBoardChrome>;
  onRetry: () => void;
  skeletonRows: number;
}) {
  if (chrome === "loading") return <DataSurfaceSkeleton label={t("storage.loading")} rows={skeletonRows} />;
  if (chrome === "scan_error") {
    return (
      <p className="storage-board-error" role="alert">
        {t("storage.error")}
        {" "}
        <button type="button" className="btn btn-ghost btn-sm" onClick={onRetry}>{t("common.retry")}</button>
      </p>
    );
  }
  return null;
}

export function StorageOverviewPanel({
  active, t, locale, showSkeleton, reportFailed, showBody, data, onRetry,
}: {
  active: boolean;
  t: TFn;
  locale: Locale;
  showSkeleton: boolean;
  reportFailed: boolean;
  showBody: boolean;
  data: StorageReport | undefined | null;
  onRetry: () => void;
}) {
  const chrome = storageBoardChrome({
    showSkeleton,
    hasData: Boolean(data),
    reportFailed,
  });
  return (
    <div
      role="tabpanel"
      id={storagePanelDomId("overview")}
      aria-labelledby={storageTabDomId("overview")}
      hidden={!active}
      className="storage-board-panel"
    >
      {active && <StorageReportIssue t={t} chrome={chrome} onRetry={onRetry} skeletonRows={5} />}
      {active && showBody && data && <StorageWorkspace report={data} locale={locale} />}
    </div>
  );
}

export function StorageCleanupPanel({
  active, apiBase, locale, t, showSkeleton, reportFailed, showBody, data, archivedCount, archivedBytes, onDone, onRetry,
}: {
  active: boolean;
  apiBase: string;
  locale: Locale;
  t: TFn;
  showSkeleton: boolean;
  reportFailed: boolean;
  showBody: boolean;
  data: StorageReport | undefined | null;
  archivedCount: number;
  archivedBytes: number;
  onDone: () => void;
  onRetry: () => void;
}) {
  const chrome = storageBoardChrome({
    showSkeleton,
    hasData: Boolean(data),
    reportFailed,
  });
  return (
    <div
      role="tabpanel"
      id={storagePanelDomId("cleanup")}
      aria-labelledby={storageTabDomId("cleanup")}
      hidden={!active}
      className="storage-board-panel"
    >
      {active && <StorageReportIssue t={t} chrome={chrome} onRetry={onRetry} skeletonRows={5} />}
      {active && showBody && data && (
        <StorageCleanupView
          apiBase={apiBase}
          locale={locale}
          t={t}
          archivedCount={archivedCount}
          archivedBytes={archivedBytes}
          truncated={data.truncated === true}
          storageGeneration={data.generatedAt}
          onDone={onDone}
        />
      )}
    </div>
  );
}

export function StorageQuarantinePanel({
  active, apiBase, locale, t, reloadToken, onDone,
}: {
  active: boolean;
  apiBase: string;
  locale: Locale;
  t: TFn;
  reloadToken: number;
  onDone: () => void;
}) {
  return (
    <div
      role="tabpanel"
      id={storagePanelDomId("quarantine")}
      aria-labelledby={storageTabDomId("quarantine")}
      hidden={!active}
      className="storage-board-panel"
    >
      {active && (
        <QuarantineTrashPanel
          apiBase={apiBase}
          locale={locale}
          t={t}
          onDone={onDone}
          reloadToken={reloadToken}
        />
      )}
    </div>
  );
}
