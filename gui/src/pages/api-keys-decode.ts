/**
 * Benes dashboard source. Boundary decoding for the API workspace's own board.
 *
 * The listener owns the shape of `/api/keys`; this module decides what the
 * dashboard is willing to believe about it. Nothing the board prints is read
 * without being checked first, so a payload the dashboard cannot read fails
 * closed and the page shows its load failure instead of a half-filled board.
 *
 * The accepted owners stay authoritative — rows come from `api-access/keys`,
 * the header rules from `api-access/auth-matrix`, and the URLs from
 * `api-access/endpoints` — so this module only decides what to do when a field
 * is absent, and never re-decides what a field means.
 */
import { DEFAULT_ENDPOINTS, deriveApiEndpoints, type ApiEndpointInfo } from "../api-access/endpoints.ts";
import { parseApiAuthMatrix, type ApiAuthMatrixRow } from "../api-access/auth-matrix.ts";
import { isApiKeyUsage, parseApiKeyEntry, type ApiKeyEntry } from "../api-access/keys.ts";
import { readSessionListCacheEntry } from "../session-list-cache.ts";

/** Everything the Keys panel and the session cache need from a listener answer. */
export type CachedKeysShape = {
  keys: ApiKeyEntry[];
  endpoints: ApiEndpointInfo;
  claudeCodeEnabled: boolean;
  attributionSince?: string;
  historyTruncated?: boolean;
  authMatrix: ApiAuthMatrixRow[];
};

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

/** A field the dashboard prints, or `null` when the answer did not carry one. */
function readString(source: Record<string, unknown>, field: string): string | null {
  const value = source[field];
  return typeof value === "string" && value !== "" ? value : null;
}

/**
 * The five URLs as a board carries them, or `null` when the value is not one.
 *
 * A board states every route it prints, so a value missing any of them is not a
 * board this workspace can render.
 */
function readEndpoints(value: unknown): ApiEndpointInfo | null {
  if (!isRecord(value)) return null;
  const baseUrl = value.baseUrl;
  const responses = value.responses;
  const chatCompletions = value.chatCompletions;
  const messages = value.messages;
  const models = value.models;
  if (typeof baseUrl !== "string" || typeof responses !== "string") return null;
  if (typeof chatCompletions !== "string" || typeof messages !== "string") return null;
  if (typeof models !== "string") return null;
  return { baseUrl, responses, chatCompletions, messages, models };
}

/**
 * A value that already describes a board: every URL, flag, row, and matrix the
 * panel prints is present and readable. Used to revalidate a session-cache
 * entry, which stores the board rather than the wire shape.
 */
function isKeysBoard(value: unknown): value is CachedKeysShape {
  if (!isRecord(value) || !Array.isArray(value.keys)) return false;
  if (readEndpoints(value.endpoints) === null) return false;
  if (parseApiAuthMatrix(value.authMatrix) === null) return false;
  if (typeof value.claudeCodeEnabled !== "boolean") return false;
  if (value.attributionSince !== undefined && typeof value.attributionSince !== "string") return false;
  if (value.historyTruncated !== undefined && typeof value.historyTruncated !== "boolean") return false;
  return value.keys.every(row => isRecord(row)
    && isApiKeyUsage(row.usage)
    && typeof row.id === "string"
    && typeof row.name === "string"
    && typeof row.prefix === "string"
    && typeof row.createdAt === "string");
}

/**
 * A board read back from the session cache, or `null` when the stored value no
 * longer describes one.
 *
 * The cache holds the board the last read produced, so a value written by
 * another build — rows this build's owner rejects, a URL it cannot print, a
 * matrix it will not accept — is discarded in favour of a fresh read rather
 * than painted as something the listener never said.
 */
export function validCachedKeys(cached: unknown): CachedKeysShape | null {
  return isKeysBoard(cached) ? cached : null;
}

/** A board a previous visit left in this session, and how old it is. */
export interface KeysBoardSeed {
  readonly board: CachedKeysShape;
  readonly cachedAt: number | null;
}

/**
 * The board the session cache still describes, or `null` when it holds none.
 *
 * Reading the cache belongs to the module that owns the board's shape: only the
 * reader that knows what a board is can say whether a stored value still is
 * one. The `cacheKey` names the exact board and is versioned by the caller, so
 * a board written by another build is never read as this one.
 */
export function readKeysBoardSeed(cacheKey: string): KeysBoardSeed | null {
  const entry = readSessionListCacheEntry<unknown>(cacheKey);
  const board = validCachedKeys(entry?.data);
  return board === null ? null : { board, cachedAt: entry?.cachedAt ?? null };
}

/**
 * The endpoints an answer reports.
 *
 * The listener names every route it serves, so a reported value wins and the
 * derivation only fills a route the answer left out. That keeps the board
 * printable even when a route arrives unnamed, without inventing a URL the
 * accepted `api-access/endpoints` owner would not derive from the same answer.
 */
function endpointsFromPayload(payload: Record<string, unknown>): ApiEndpointInfo {
  const endpoint = readString(payload, "endpoint");
  const derived = deriveApiEndpoints(endpoint ?? "");
  return {
    baseUrl: readString(payload, "baseUrl") ?? derived.baseUrl,
    responses: readString(payload, "responsesEndpoint") ?? endpoint ?? DEFAULT_ENDPOINTS.responses,
    chatCompletions: readString(payload, "chatCompletionsEndpoint") ?? derived.chatCompletions,
    messages: readString(payload, "messagesEndpoint") ?? derived.messages,
    models: readString(payload, "modelsEndpoint") ?? derived.models,
  };
}

/**
 * The whole `/api/keys` answer, or `null` when it cannot be read.
 *
 * The board is rebuilt from the answer rather than handed over as parsed JSON,
 * so nothing the dashboard does not print can travel with it. One unreadable
 * row, or a missing header matrix, fails the whole read: a board that is
 * quietly missing something is worse than one that says it could not load.
 * A missing Claude flag means the Messages surface is on, which is the
 * listener's own default.
 */
export function decodeKeysPayload(payload: unknown): CachedKeysShape | null {
  if (!isRecord(payload)) return null;
  const authMatrix = parseApiAuthMatrix(payload.authMatrix);
  if (authMatrix === null) return null;
  const rows = payload.keys === undefined ? [] : payload.keys;
  if (!Array.isArray(rows)) return null;
  const keys: ApiKeyEntry[] = [];
  for (const row of rows) {
    const entry = parseApiKeyEntry(row);
    if (entry === null) return null;
    keys.push(entry);
  }
  const attributionSince = readString(payload, "attributionSince");
  return {
    keys,
    endpoints: endpointsFromPayload(payload),
    claudeCodeEnabled: payload.claudeCodeEnabled !== false,
    ...(attributionSince === null ? {} : { attributionSince }),
    ...(payload.historyTruncated === true ? { historyTruncated: true } : {}),
    authMatrix,
  };
}
