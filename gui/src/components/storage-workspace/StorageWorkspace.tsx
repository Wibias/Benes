/**
 * Benes dashboard client for the Go proxy (`internal/server`).
 */
/* eslint-disable react-refresh/only-export-components -- bucket label helper co-locates with the rail rows */
import { useMemo, useState } from "react";
import { useT } from "../../i18n/shared";
import type { Locale, TFn, TKey } from "../../i18n/shared";
import type { CodexLogGuardAction, StorageBucket, StorageReport } from "./types";
import { StorageOverviewSummary, StorageRail } from "./storage-workspace-sections";
import { StorageDetailPane } from "./storage-detail-pane";
import { useLogGuard } from "./use-log-guard";
import { ALL_STORAGE_KEY, bucketsBySize, largestAcrossBuckets } from "../../pages/storage-report-view";

export type {
  CodexLogGuardAction,
  CodexLogGuardProtection,
  CodexLogGuardReport,
  StorageBucket,
  StorageLargestEntry,
  StorageReport,
} from "./types";

/**
 * Catalogue copy for the bucket keys this build knows, keyed by the key the listener reports.
 *
 * The scan may find a bucket this build has no copy for, so the table is a lookup rather than a
 * closed shape: a key that is missing keeps the label the listener sent.
 */
const BUCKET_COPY_KEYS = new Map<string, TKey>([
  ["sessions", "storage.bucket.sessions"],
  ["archived_sessions", "storage.bucket.archived_sessions"],
  ["logs_db", "storage.bucket.logs_db"],
  ["state_db", "storage.bucket.state_db"],
  ["attachments", "storage.bucket.attachments"],
  ["deletion_manifests", "storage.bucket.deletion_manifests"],
  ["other", "storage.bucket.other"],
]);

export function bucketLabel(bucket: StorageBucket, t: TFn): string {
  const copyKey = BUCKET_COPY_KEYS.get(bucket.key);
  return copyKey === undefined ? bucket.label : t(copyKey);
}

export interface StorageWorkspaceProps {
  readonly report: StorageReport;
  readonly locale: Locale;
  readonly logGuardBusy?: boolean;
  readonly onLogGuardAction?: (action: CodexLogGuardAction) => void;
}

/** The three readings of one bucket list the rail and the detail pane need between them. */
type BucketIndex = {
  readonly sorted: StorageBucket[];
  readonly largest: ReturnType<typeof largestAcrossBuckets>;
  readonly byKey: Map<string, StorageBucket>;
};

export default function StorageWorkspace({
  report,
  locale,
  logGuardBusy = false,
  onLogGuardAction,
}: StorageWorkspaceProps) {
  const t = useT();
  const [selectedKey, setSelectedKey] = useState(ALL_STORAGE_KEY);
  const logGuard = useLogGuard(report.generatedAt, locale, logGuardBusy, onLogGuardAction);

  const index = useMemo<BucketIndex>(() => ({
    sorted: bucketsBySize(report.buckets),
    largest: largestAcrossBuckets(report.buckets),
    byKey: new Map(report.buckets.map(bucket => [bucket.key, bucket])),
  }), [report.buckets]);
  const selected = index.sorted.find(bucket => bucket.key === selectedKey) ?? null;
  const mainLabel = selected ? bucketLabel(selected, t) : t("storage.allStorage");

  return (
    <div className="storage-workspace-root">
      <StorageOverviewSummary t={t} locale={locale} report={report} />
      <div className="storage-workspace-split">
        <StorageRail
          t={t}
          locale={locale}
          sortedBuckets={index.sorted}
          selectedKey={selectedKey}
          bucketLabel={bucketLabel}
          onSelect={setSelectedKey}
          allBytes={report.total.bytes}
        />
        <section className="storage-workspace-main" aria-label={mainLabel}>
          <StorageDetailPane
            t={t}
            locale={locale}
            report={report}
            selected={selected}
            selectedKey={selectedKey}
            bucketLabel={bucketLabel}
            largestAcross={index.largest}
            bucketByKey={index.byKey}
            logGuard={logGuard.report}
            logGuardBusy={logGuard.busy}
            logGuardError={logGuard.error}
            logGuardCompaction={logGuard.compaction}
            inspectFailed={logGuard.inspectFailed}
            onLogGuardAction={logGuard.runAction}
          />
        </section>
      </div>
    </div>
  );
}