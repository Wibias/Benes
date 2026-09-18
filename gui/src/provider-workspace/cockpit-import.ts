/**
 * Wire decode for the cockpit-tools account import.
 *
 * `POST /api/oauth/accounts/import` answers with one row per imported entry.
 * The listener is the only party allowed to claim an import succeeded, so this
 * module re-derives every count from the rows and refuses a response that
 * disagrees with itself, carries keys it does not own, or nests a row at the
 * wrong index. It never reads a credential: rows are counts and status codes.
 */

export const COCKPIT_IMPORT_MAX_BYTES = 256 * 1024;

export type CockpitImportResult = {
  importedCount: number;
  updatedCount: number;
  failedCount: number;
  unsupportedCount: number;
};

export type CockpitImportOutcome =
  | { status: "failed" }
  | { status: "complete"; result: CockpitImportResult };

export type CockpitImportFile = {
  name: string;
  size: number;
  text(): Promise<string>;
};

export type CockpitImportSession = {
  busy: boolean;
  begin(): void;
  finish(): void;
  setInvalid(): void;
  setFailed(): void;
  setComplete(result: CockpitImportResult): void;
  postDocument(document: unknown): Promise<{ ok: boolean; payload: unknown }>;
  refreshAccounts(): Promise<void>;
};

type CockpitRowStatus = "imported" | "updated" | "failed" | "unsupported";

/** Response envelope keys. Anything else means the listener changed shape. */
const RESPONSE_KEYS = [
  "totalCount",
  "importedCount",
  "updatedCount",
  "failedCount",
  "unsupportedCount",
  "results",
] as const;

const ROW_KEYS = ["index", "status", "code"] as const;
const COUNT_KEYS = ["totalCount", "importedCount", "updatedCount", "failedCount", "unsupportedCount"] as const;

/** Row status → the codes the listener may pair with it. */
const ROW_CODES = new Map<string, readonly string[]>([
  ["imported", ["imported"]],
  ["updated", ["updated"]],
  ["failed", ["invalid_record", "credential_rejected", "identity_mismatch", "missing_project", "persist_failed"]],
  ["unsupported", ["unsupported_provider", "unsupported_format"]],
]);

/** Row status → the tally it feeds. */
const STATUS_TALLY: Record<CockpitRowStatus, keyof CockpitImportResult> = {
  imported: "importedCount",
  updated: "updatedCount",
  failed: "failedCount",
  unsupported: "unsupportedCount",
};

/**
 * JSON object, not an array and not a caller-built instance.
 * `Object.create({ ... })` and class instances keep a prototype this decoder
 * does not trust.
 */
function isRecord(value: unknown): value is Record<string, unknown> {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return false;
  const prototype: unknown = Object.getPrototypeOf(value);
  return prototype === Object.prototype || prototype === null;
}

function hasOnlyKeys(value: Record<string, unknown>, allowed: readonly string[]): boolean {
  return Object.keys(value).every(key => allowed.includes(key));
}

function isTallyValue(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}

function readDeclaredCounts(value: Record<string, unknown>): Record<(typeof COUNT_KEYS)[number], number> | null {
  const counts = {} as Record<(typeof COUNT_KEYS)[number], number>;
  for (const key of COUNT_KEYS) {
    const raw = value[key];
    if (!isTallyValue(raw)) return null;
    counts[key] = raw;
  }
  return counts;
}

/** One result row, or null when it is not a row the listener can emit. */
function readRowStatus(row: unknown, index: number): CockpitRowStatus | null {
  if (!isRecord(row) || !hasOnlyKeys(row, ROW_KEYS)) return null;
  if (row.index !== index) return null;
  const status = String(row.status);
  const codes = ROW_CODES.get(status);
  if (!codes || !codes.includes(String(row.code))) return null;
  return status as CockpitRowStatus;
}

/** Tally the rows themselves; any malformed row voids the whole response. */
function tallyRows(results: unknown[]): CockpitImportResult | null {
  const tally: CockpitImportResult = { importedCount: 0, updatedCount: 0, failedCount: 0, unsupportedCount: 0 };
  for (const [index, row] of results.entries()) {
    const status = readRowStatus(row, index);
    if (!status) return null;
    tally[STATUS_TALLY[status]] += 1;
  }
  return tally;
}

export function cockpitImportFileAccepted(file: Pick<CockpitImportFile, "name" | "size">): boolean {
  return file.name.toLowerCase().endsWith(".json") && file.size <= COCKPIT_IMPORT_MAX_BYTES;
}

export function safeCockpitImportResult(value: unknown): CockpitImportResult | null {
  if (!isRecord(value) || !hasOnlyKeys(value, RESPONSE_KEYS)) return null;
  const declared = readDeclaredCounts(value);
  if (!declared) return null;
  const { results } = value;
  if (!Array.isArray(results)) return null;
  const tally = tallyRows(results);
  if (!tally) return null;
  if (results.length !== declared.totalCount) return null;
  const agrees = tally.importedCount === declared.importedCount
    && tally.updatedCount === declared.updatedCount
    && tally.failedCount === declared.failedCount
    && tally.unsupportedCount === declared.unsupportedCount;
  return agrees ? tally : null;
}

export function cockpitImportOutcomeFromResponse(ok: boolean, payload: unknown): CockpitImportOutcome {
  if (!ok) return { status: "failed" };
  const result = safeCockpitImportResult(payload);
  return result ? { status: "complete", result } : { status: "failed" };
}

type CockpitImportStep =
  | { kind: "invalid" }
  | { kind: "failed" }
  | { kind: "complete"; result: CockpitImportResult };

/**
 * Read the document and post it. An unreadable or non-JSON file is the user's
 * mistake (invalid); a refused post is the listener's answer (failed).
 */
async function readAndPostCockpitDocument(
  file: Pick<CockpitImportFile, "name" | "size" | "text">,
  session: CockpitImportSession,
): Promise<CockpitImportStep> {
  if (!cockpitImportFileAccepted(file)) return { kind: "invalid" };
  let document: unknown;
  try {
    document = JSON.parse(await file.text()) as unknown;
  } catch {
    return { kind: "invalid" };
  }
  const response = await session.postDocument(document);
  const outcome = cockpitImportOutcomeFromResponse(response.ok, response.payload);
  return outcome.status === "complete" ? { kind: "complete", result: outcome.result } : { kind: "failed" };
}

export async function runCockpitImport(
  file: CockpitImportFile | undefined,
  session: CockpitImportSession,
): Promise<void> {
  if (!file || session.busy) return;
  session.begin();
  try {
    const step = await readAndPostCockpitDocument(file, session);
    if (step.kind === "invalid") {
      session.setInvalid();
      return;
    }
    if (step.kind === "failed") {
      session.setFailed();
      return;
    }
    session.setComplete(step.result);
    try {
      await session.refreshAccounts();
    } catch {
      /* Keep the completed import; accountLoadState owns the refresh failure. */
    }
  } catch {
    session.setFailed();
  } finally {
    session.finish();
  }
}
