import type { Locale, TFn } from "../../i18n/shared";
import { ALL_STORAGE_KEY } from "../../pages/storage-report-view";
import { CodexLogGuardPanel, CodexLogGuardUnavailablePanel } from "./log-guard-panel";
import { StorageBucketFacts, StorageLargestList } from "./storage-workspace-sections";
import type { CodexLogGuardAction, CodexLogGuardReport, StorageBucket, StorageLargestEntry, StorageReport } from "./types";

export function StorageDetailPane({
  t, locale, report, selected, selectedKey, bucketLabel, largestAcross, bucketByKey,
  logGuard, logGuardBusy, logGuardError, logGuardCompaction, inspectFailed, onLogGuardAction,
}: {
  t: TFn;
  locale: Locale;
  report: StorageReport;
  selected: StorageBucket | null;
  selectedKey: string;
  bucketLabel: (bucket: StorageBucket, t: TFn) => string;
  largestAcross: Array<StorageLargestEntry & { bucketKey: string }>;
  bucketByKey: Map<string, StorageBucket>;
  logGuard: CodexLogGuardReport | null;
  logGuardBusy: boolean;
  logGuardError: string | null;
  logGuardCompaction: string | null;
  inspectFailed: string | null;
  onLogGuardAction: (action: CodexLogGuardAction) => void;
}) {
  if (selectedKey === ALL_STORAGE_KEY || !selected) {
    return (
      <>
        <StorageBucketFacts
          t={t}
          locale={locale}
          title={t("storage.allStorage")}
          bytes={report.total.bytes}
          physicalBytes={report.total.physicalBytes}
          fileCount={report.total.fileCount}
        />
        <StorageLargestList
          t={t}
          locale={locale}
          largestAcross={largestAcross}
          bucketByKey={bucketByKey}
          bucketLabel={bucketLabel}
          emptyLabel={report.total.fileCount === 0 ? t("storage.empty.files") : t("storage.empty.largest")}
        />
      </>
    );
  }

  const logsDb = selected.key === "logs_db";
  return (
    <>
      <StorageBucketFacts
        t={t}
        locale={locale}
        title={bucketLabel(selected, t)}
        bytes={selected.bytes}
        physicalBytes={selected.physicalBytes}
        fileCount={selected.fileCount}
        oldest={selected.oldest}
        newest={selected.newest}
        includeAge
        rows={logsDb ? logGuard?.metrics?.totalRows : undefined}
        reclaimable={logsDb ? logGuard?.metrics?.reclaimableBytes : undefined}
      />
      {logsDb && (
        <>
          <div className="stw-section-rule" />
          {logGuard ? (
            <CodexLogGuardPanel
              report={logGuard}
              locale={locale}
              t={t}
              busy={logGuardBusy}
              error={logGuardError}
              compaction={logGuardCompaction}
              onAction={onLogGuardAction}
              embedded
            />
          ) : inspectFailed ? (
            <CodexLogGuardUnavailablePanel locale={locale} t={t} embedded />
          ) : null}
        </>
      )}
      <StorageLargestList
        t={t}
        locale={locale}
        largestAcross={selected.largest ?? []}
        emptyLabel={t("storage.empty.largest")}
      />
    </>
  );
}
