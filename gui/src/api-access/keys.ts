/**
 * Benes dashboard source. The API-key record contract: what the listener says
 * about a stored key, and the shape the dashboard is allowed to render.
 *
 * Two things are deliberately separated here. The wire form carries a bare
 * `ambiguous` flag when two stored keys ended up sharing an id, and the view
 * form keeps that case in its own constructor so no renderer can read a count
 * off an ambiguous key. And a key row never carries the secret: `prefix` is the
 * listener's own truncated form, and the dashboard neither receives nor derives
 * the rest.
 */

/** Which of the two things the listener could say about a key's traffic. */
export type ApiKeyUsage =
  | { kind: "ambiguous" }
  | { kind: "attributed"; requests7d: number; totalRequests: number; lastUsedAt: string | null };

/** One stored key as the list and detail surfaces read it. */
export interface ApiKeyEntry {
  id: string;
  name: string;
  /** Server-computed and already truncated. Display-only. */
  prefix: string;
  createdAt: string;
  /** Always present; zero is a real answer. Whether anything is attributable
   *  at all is the response-level `attributionSince`. */
  usage: ApiKeyUsage;
}

function isFiniteCount(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

/**
 * `undefined` means the field was present but unusable (a failed parse must
 * reject the whole record); `null` means the key has simply never been used.
 */
function readLastUsed(value: unknown): string | null | undefined {
  if (value === undefined) return null;
  if (typeof value !== "string" || Number.isNaN(new Date(value).getTime())) return undefined;
  return value;
}

/**
 * Turn one `/api/keys` usage object into a renderable record, or `null` when the
 * payload cannot be trusted. Reading a malformed object as zeroes would report
 * "used zero times" about data nobody could parse, which is the false
 * confidence the response-level `attributionSince` exists to prevent.
 */
export function parseApiKeyUsage(input: unknown): ApiKeyUsage | null {
  if (input === null || typeof input !== "object") return null;
  const wire = input as Record<string, unknown>;
  if (wire.ambiguous === true) return { kind: "ambiguous" };
  if (wire.ambiguous !== undefined && wire.ambiguous !== false) return null;
  if (!isFiniteCount(wire.requests7d) || !isFiniteCount(wire.totalRequests)) return null;
  const lastUsedAt = readLastUsed(wire.lastUsedAt);
  if (lastUsedAt === undefined) return null;
  return {
    kind: "attributed",
    requests7d: wire.requests7d,
    totalRequests: wire.totalRequests,
    lastUsedAt,
  };
}

/**
 * True when a value is already the parsed view model.
 *
 * Used to revalidate a session-cache entry, which stores the view model rather
 * than the wire shape. A cache written by an older build carries the old shape,
 * so it fails this check and is discarded in favour of a fresh read rather than
 * being rendered as something it is not.
 */
export function isApiKeyUsage(value: unknown): value is ApiKeyUsage {
  if (value === null || typeof value !== "object") return false;
  const record = value as Record<string, unknown>;
  if (record.kind === "ambiguous") return true;
  if (record.kind !== "attributed") return false;
  return isFiniteCount(record.requests7d)
    && isFiniteCount(record.totalRequests)
    && (record.lastUsedAt === null || typeof record.lastUsedAt === "string");
}

/**
 * Read one `/api/keys` row, or `null` when any field the surfaces print is
 * missing. A row without a name or a prefix would render as blanks that look
 * like an empty key rather than a broken payload.
 */
export function parseApiKeyEntry(input: unknown): ApiKeyEntry | null {
  if (input === null || typeof input !== "object") return null;
  const wire = input as Record<string, unknown>;
  const usage = parseApiKeyUsage(wire.usage);
  if (usage === null) return null;
  if (
    typeof wire.id !== "string"
    || typeof wire.name !== "string"
    || typeof wire.prefix !== "string"
    || typeof wire.createdAt !== "string"
  ) {
    return null;
  }
  return {
    id: wire.id,
    name: wire.name,
    prefix: wire.prefix,
    createdAt: wire.createdAt,
    usage,
  };
}
