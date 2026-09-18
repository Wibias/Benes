/** Benes dashboard client for the Go proxy (`internal/server`). */
/**
 * Wire shapes the Storage workspace observes.
 *
 * `/api/storage` reports byte counts and timestamps per bucket and `/api/storage/codex-logs`
 * reports the Codex log database. Every field is read-only: the board renders one snapshot the
 * listener sent and never edits it in place, and an optional field means the listener may omit
 * it — never that it is zero.
 */

/** One file inside a bucket, as the scan's largest-files list reports it. */
export interface StorageLargestEntry {
  readonly path: string;
  readonly bytes: number;
}

/** Counts the scan reports, for one bucket or for the whole report. */
interface ByteCounts {
  readonly bytes: number;
  readonly fileCount: number;
  /** On-disk bytes; reported only when the listener could measure them. */
  readonly physicalBytes?: number;
}

/** When the oldest and newest file in a bucket were last written. */
interface ByteTimes {
  readonly oldest?: number;
  readonly newest?: number;
}

export interface StorageBucket extends ByteCounts, ByteTimes {
  readonly key: string;
  readonly label: string;
  readonly largest?: StorageLargestEntry[];
}

/** The write-guard mode the database should be kept in. */
type LogGuardMode = "off" | "compat" | "quiet";

/** The mode the database is actually in; a collision means another writer holds it. */
type LogGuardObservedMode = LogGuardMode | "collision";

/** How the guard reads: off, in force, drifted from the desired mode, or unavailable. */
type LogGuardState = "off" | "active" | "drifted" | "unsupported" | "unknown";

/** Why the guard cannot inspect or protect the database. */
type LogGuardReason = "database_missing" | "database_unreadable" | "unknown_schema";

type LogGuardCapability =
  | { readonly state: "supported" }
  | { readonly state: "unsupported"; readonly reason: LogGuardReason };

/** What the listener found when it inspected the database schema. */
export type LogGuardSchema =
  | { readonly state: "compatible" }
  | { readonly state: "missing"; readonly reason: "database_missing" }
  | { readonly state: "unreadable"; readonly reason: "database_unreadable" }
  | { readonly state: "unsupported"; readonly reason: "unknown_schema" };

export interface CodexLogGuardProtection {
  readonly desiredMode: LogGuardMode;
  readonly observedMode: LogGuardObservedMode;
  readonly state: LogGuardState;
}

/** Byte counts of the log database and of its write-ahead sidecars. */
interface LogGuardFiles {
  readonly databaseBytes: number;
  readonly walBytes: number;
  readonly shmBytes: number;
}

interface LogGuardCapabilities {
  readonly inspection: LogGuardCapability;
  readonly protection: LogGuardCapability;
  readonly reclaim: LogGuardCapability;
}

/** Row accounting, page geometry, and reclaim numbers, reported while inspection is supported. */
interface LogGuardMetrics {
  readonly totalRows: number;
  readonly rowsByLevel: { readonly [level: string]: number };
  readonly traceRows: number;
  readonly traceShare: number;
  readonly topTargets: { readonly target: string; readonly rows: number }[];
  readonly pageSize: number;
  readonly pageCount: number;
  readonly freelistPages: number;
  readonly reclaimableBytes: number;
  readonly estimatedLogBytes: number | null;
}

export interface CodexLogGuardReport {
  readonly generatedAt: number;
  readonly externalSqliteHome: boolean;
  readonly snapshot: "checkpointed";
  readonly files: LogGuardFiles;
  readonly schema: LogGuardSchema;
  readonly capabilities: LogGuardCapabilities;
  readonly protection?: CodexLogGuardProtection;
  readonly metrics: null | LogGuardMetrics;
}

export interface StorageReport {
  readonly codexHome: string;
  readonly generatedAt: number;
  readonly total: ByteCounts;
  readonly buckets: StorageBucket[];
  readonly truncated?: boolean;
  readonly error?: string;
}

/** What the workspace can ask the listener to do about the log database. */
export type CodexLogGuardAction =
  | { readonly action: "protect"; readonly mode: Exclude<LogGuardMode, "off"> }
  | { readonly action: "unprotect" }
  | { readonly action: "repair" }
  | { readonly action: "compact" };