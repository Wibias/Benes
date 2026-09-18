import type { TFn } from "../i18n/shared";
import type { TrashEntry, TrashList, TrashRecovery } from "./storage-cleanup-policy";

export type TrashRowStatus = "ready" | "partial" | "recovery_needed";

export type TrashRow = {
  id: string;
  kind: "entry" | "recovery";
  createdAt?: number;
  mode?: TrashEntry["mode"];
  fileCount?: number;
  bytes?: number;
  status: TrashRowStatus;
  recoveryError?: string;
  recoveryStatus?: string;
  recoveryId?: string;
  entry?: TrashEntry;
};

export function entryCreatedAt(entry: TrashEntry): number | undefined {
  if (isFiniteNumber(entry.quarantinedAt) && entry.quarantinedAt > 0) return entry.quarantinedAt;
  const epochMs = Number(String(entry.epoch ?? "").split("-")[0]);
  if (Number.isFinite(epochMs) && epochMs > 0) return epochMs;
  return undefined;
}

export function entryStatus(entry: TrashEntry): TrashRowStatus {
  return entry.partial === true ? "partial" : "ready";
}

export function recoveryRowId(row: TrashRecovery, index: number): string {
  const id = row.id?.trim();
  if (id) return id;
  return ["rec", row.status, row.error, String(index)].join("-");
}

export function trashRows(list: TrashList | null | undefined): TrashRow[] {
  const rows: TrashRow[] = [];
  for (const entry of list?.entries ?? []) {
    rows.push({
      id: entry.id,
      kind: "entry",
      createdAt: entryCreatedAt(entry),
      mode: entry.mode,
      fileCount: entry.fileCount,
      bytes: entry.bytes,
      status: entryStatus(entry),
      entry,
    });
  }
  (list?.recoveryNeeded ?? []).forEach((row, index) => {
    const recoveryId = row.id?.trim();
    rows.push({
      id: recoveryRowId(row, index),
      kind: "recovery",
      status: "recovery_needed",
      recoveryId: recoveryId || undefined,
      recoveryError: row.error,
      recoveryStatus: row.status,
    });
  });
  return rows;
}

export function trashHasContent(list: TrashList | null | undefined): boolean {
  return trashRows(list).length > 0;
}

export type QuarantineChromeKind = "loading" | "error" | "empty" | "split";

export function quarantineChromeKind(input: {
  showSkeleton: boolean;
  showError: boolean;
  rowCount: number;
}): QuarantineChromeKind {
  if (input.showSkeleton) return "loading";
  if (input.showError) return "error";
  if (input.rowCount === 0) return "empty";
  return "split";
}

export function trashListFailureMessage(
  payload: { error?: unknown; message?: unknown },
  fallback: string,
): string {
  if (typeof payload.message === "string" && payload.message.trim()) return payload.message.trim();
  if (typeof payload.error === "string" && payload.error.trim()) return payload.error.trim();
  return fallback;
}

const RECOVERY_REASON_KEYS = {
  invalid_trash: "storage.trash.recovery.reason.invalid",
  path_escape: "storage.trash.recovery.reason.invalid",
  missing_trash: "storage.trash.recovery.reason.missing",
  fs_failed: "storage.trash.recovery.reason.unreadable",
  unreadable: "storage.trash.recovery.reason.unreadable",
  incomplete: "storage.trash.recovery.reason.incomplete",
} as const;

export function recoveryReasonKey(error?: string, status?: string): keyof typeof RECOVERY_REASON_KEYS | "generic" {
  const code = (error ?? "").trim();
  if (code in RECOVERY_REASON_KEYS) return code as keyof typeof RECOVERY_REASON_KEYS;
  const state = (status ?? "").trim();
  if (state in RECOVERY_REASON_KEYS) return state as keyof typeof RECOVERY_REASON_KEYS;
  return "generic";
}

export function recoveryReasonLabel(t: TFn, error?: string, status?: string): string {
  const key = recoveryReasonKey(error, status);
  if (key === "generic") return t("storage.trash.recovery.reason.unreadable");
  return t(RECOVERY_REASON_KEYS[key]);
}

function isFiniteNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}
