/** Benes dashboard client for the Go proxy (`internal/server`). */
import { formatBytes } from "../../format-bytes";
import type { Locale, TFn } from "../../i18n/shared";
import {
  ALL_STORAGE_KEY,
  resolvedPhysicalBytes,
  timestampDisplay,
} from "../../pages/storage-report-view";
import type { StorageBucket, StorageLargestEntry, StorageReport } from "./types";

function bytesOrDash(bytes: number | undefined, locale: Locale): string {
  return bytes === undefined ? "—" : formatBytes(bytes, locale);
}

/** One selectable row of the storage rail: a bucket, or the synthetic All row above them. */
function RailRow({
  name,
  size,
  selected,
  onSelect,
}: {
  name: string;
  size: string;
  selected: boolean;
  onSelect: () => void;
}) {
  return (
    <button
      type="button"
      className={selected
        ? "storage-workspace-rail-row storage-workspace-rail-row--selected"
        : "storage-workspace-rail-row"}
      onClick={onSelect}
      aria-current={selected ? "true" : undefined}
    >
      <span className="storage-workspace-rail-primary">
        <span className="storage-workspace-rail-name">{name}</span>
        <span className="storage-workspace-rail-size">{size}</span>
      </span>
    </button>
  );
}

export function StorageRail({
  t, locale, sortedBuckets, selectedKey, bucketLabel, onSelect, allBytes,
}: {
  t: TFn;
  locale: Locale;
  sortedBuckets: StorageBucket[];
  selectedKey: string;
  bucketLabel: (bucket: StorageBucket, t: TFn) => string;
  onSelect: (key: string) => void;
  allBytes: number;
}) {
  const rows = [
    { key: ALL_STORAGE_KEY, name: t("storage.allStorage"), size: formatBytes(allBytes, locale) },
    ...sortedBuckets.map(bucket => ({
      key: bucket.key,
      name: bucketLabel(bucket, t),
      size: formatBytes(bucket.bytes, locale),
    })),
  ];
  return (
    <aside className="storage-workspace-rail" aria-label={t("storage.section.buckets")}>
      <div className="storage-workspace-rail-header">
        <span className="storage-workspace-rail-title">{t("storage.section.buckets")}</span>
      </div>
      <div className="storage-workspace-rail-list">
        {rows.map(row => (
          <RailRow
            key={row.key}
            name={row.name}
            size={row.size}
            selected={row.key === selectedKey}
            onSelect={() => onSelect(row.key)}
          />
        ))}
      </div>
    </aside>
  );
}

/** One label/value line of a bucket's fact list. */
function FactRow({ label, value }: { label: string; value: string }) {
  return (
    <div className="stw-kv-row">
      <dt>{label}</dt>
      <dd className="stw-kv-mono">{value}</dd>
    </div>
  );
}

export function StorageBucketFacts({
  t, locale, title, bytes, physicalBytes, fileCount, oldest, newest, rows, reclaimable, includeAge = false,
}: {
  t: TFn;
  locale: Locale;
  title: string;
  bytes: number;
  physicalBytes?: number;
  fileCount: number;
  oldest?: number;
  newest?: number;
  rows?: number;
  reclaimable?: number;
  includeAge?: boolean;
}) {
  const facts = [
    { label: t("storage.col.size"), value: formatBytes(bytes, locale) },
    { label: t("storage.col.physical"), value: bytesOrDash(resolvedPhysicalBytes(bytes, physicalBytes), locale) },
    { label: t("storage.col.files"), value: fileCount.toLocaleString(locale) },
    ...(includeAge
      ? [
        { label: t("storage.col.oldest"), value: timestampDisplay(oldest, locale) },
        { label: t("storage.col.newest"), value: timestampDisplay(newest, locale) },
      ]
      : []),
    ...(rows === undefined ? [] : [{ label: t("storage.col.rows"), value: rows.toLocaleString(locale) }]),
    ...(reclaimable === undefined
      ? []
      : [{ label: t("storage.col.reclaimable"), value: formatBytes(reclaimable, locale) }]),
  ];
  return (
    <>
      <h2 className="stw-detail-title">{title}</h2>
      <dl className="stw-kv">
        {facts.map(fact => <FactRow key={fact.label} label={fact.label} value={fact.value} />)}
      </dl>
    </>
  );
}

export function StorageLargestList({
  t, locale, largestAcross, bucketByKey, bucketLabel, emptyLabel,
}: {
  t: TFn;
  locale: Locale;
  largestAcross: Array<StorageLargestEntry & { bucketKey?: string }>;
  bucketByKey?: Map<string, StorageBucket>;
  bucketLabel?: (bucket: StorageBucket, t: TFn) => string;
  emptyLabel: string;
}) {
  if (largestAcross.length === 0) {
    return <p className="stw-empty">{emptyLabel}</p>;
  }
  const withBucket = Boolean(bucketByKey);
  const rowClass = withBucket ? "stw-file-row stw-file-row--with-bucket" : "stw-file-row";
  const headClass = withBucket ? "stw-file-head stw-file-head--with-bucket" : "stw-file-head";
  return (
    <div className="stw-section">
      <h3 className="stw-section-title">{t("storage.section.largest")}</h3>
      <div className={headClass}>
        <span>{t("storage.col.path")}</span>
        {withBucket && <span>{t("storage.col.bucket")}</span>}
        <span>{t("storage.col.size")}</span>
      </div>
      {largestAcross.map(entry => {
        const owner = entry.bucketKey ? bucketByKey?.get(entry.bucketKey) : undefined;
        return (
          <div key={`${entry.bucketKey ?? ""}:${entry.path}`} className={rowClass}>
            <span className="stw-file-path" title={entry.path}>{entry.path}</span>
            {withBucket && (
              <span className="stw-file-bucket">{owner && bucketLabel ? bucketLabel(owner, t) : "—"}</span>
            )}
            <span className="stw-file-size">{formatBytes(entry.bytes, locale)}</span>
          </div>
        );
      })}
    </div>
  );
}

export function StorageOverviewSummary({ t, locale, report }: { t: TFn; locale: Locale; report: StorageReport }) {
  const physical = resolvedPhysicalBytes(report.total.bytes, report.total.physicalBytes);
  const metrics = [
    { label: t("storage.card.total"), value: formatBytes(report.total.bytes, locale) },
    { label: t("storage.card.physical"), value: physical === undefined ? "—" : formatBytes(physical, locale) },
    { label: t("storage.card.files"), value: report.total.fileCount.toLocaleString(locale) },
  ];
  return (
    <div className="stw-summary" role="group" aria-label={t("storage.tab.overview")}>
      {metrics.map(metric => (
        <div key={metric.label} className="stw-summary-metric">
          <div className="stw-summary-label">{metric.label}</div>
          <div className="stw-summary-value">{metric.value}</div>
        </div>
      ))}
    </div>
  );
}