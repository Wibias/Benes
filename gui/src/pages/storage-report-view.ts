import type { StorageBucket, StorageLargestEntry, StorageReport } from "../components/storage-workspace/types";

export const ALL_STORAGE_KEY = "all";

export type ScanCompleteness = "complete" | "partial";

export function isFiniteNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

/** Logical bytes when physicalBytes is omitted (backend omitempty when equal). Never invent 0. */
export function resolvedPhysicalBytes(logical: number | undefined, physical: number | undefined): number | undefined {
  if (isFiniteNumber(physical)) return physical;
  if (isFiniteNumber(logical)) return logical;
  return undefined;
}

export function scanCompleteness(truncated: boolean | undefined): ScanCompleteness {
  return truncated === true ? "partial" : "complete";
}

export function bucketsBySize(buckets: StorageBucket[]): StorageBucket[] {
  return buckets.toSorted((a, b) => b.bytes - a.bytes || a.key.localeCompare(b.key));
}

export function largestAcrossBuckets(
  buckets: StorageBucket[],
  cap = 10,
): Array<StorageLargestEntry & { bucketKey: string }> {
  const rows: Array<StorageLargestEntry & { bucketKey: string }> = [];
  for (const bucket of buckets) {
    for (const entry of bucket.largest ?? []) rows.push({ ...entry, bucketKey: bucket.key });
  }
  return rows.sort((a, b) => b.bytes - a.bytes || a.path.localeCompare(b.path)).slice(0, cap);
}

export function bucketByKey(buckets: StorageBucket[], key: string): StorageBucket | undefined {
  return buckets.find(bucket => bucket.key === key);
}

export function archivedBucket(report: StorageReport | null | undefined): StorageBucket | undefined {
  return report?.buckets.find(bucket => bucket.key === "archived_sessions");
}

export function storageSummary(report: StorageReport): {
  totalBytes: number;
  physicalBytes: number | undefined;
  fileCount: number;
  truncated: boolean;
} {
  return {
    totalBytes: report.total.bytes,
    physicalBytes: resolvedPhysicalBytes(report.total.bytes, report.total.physicalBytes),
    fileCount: report.total.fileCount,
    truncated: report.truncated === true,
  };
}

export function cleanupRefusedByTruncation(truncated: boolean | undefined): boolean {
  return truncated === true;
}

export type StorageBoardChrome = "loading" | "scan_error" | "ready" | "idle";

export function storageBoardChrome(input: {
  showSkeleton: boolean;
  hasData: boolean;
  reportFailed: boolean;
}): StorageBoardChrome {
  if (input.showSkeleton && !input.hasData) return "loading";
  if (input.reportFailed) return "scan_error";
  if (input.hasData) return "ready";
  return "idle";
}

export function usableTimestamp(ms: number | undefined | null): number | undefined {
  if (!isFiniteNumber(ms)) return undefined;
  return ms;
}

export function timestampDisplay(ms: number | undefined | null, locale: string): string {
  const usable = usableTimestamp(ms);
  return usable === undefined ? "—" : new Date(usable).toLocaleDateString(locale);
}

export function timestampDateTimeDisplay(ms: number | undefined | null, locale: string): string {
  const usable = usableTimestamp(ms);
  return usable === undefined ? "—" : new Date(usable).toLocaleString(locale);
}
