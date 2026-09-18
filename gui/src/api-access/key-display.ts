/**
 * Benes dashboard source. How a stored key is allowed to appear on screen.
 *
 * Everything here is display-only. The wire format, the authentication value,
 * and the server's own prefix computation are untouched; this module only
 * decides what a dashboard cell may show of what the listener already sent.
 */

/**
 * Longest key name the create and rename routes accept.
 * Mirrors `apiKeyNameMaxRunes` in `internal/server/keys_api.go`; the inputs cap
 * at the same number so a too-long name is untypeable rather than rejected.
 */
export const API_KEY_NAME_MAX_LENGTH = 64;

const DATA_PLANE_MARKER = "benes_data_";
const PUBLIC_MARKER = "benes_";

/**
 * Drop the data-plane `_data_` segment from a server-computed prefix.
 *
 * The listener authenticates with the full secret and publishes a truncated
 * prefix of it. That prefix is already short; removing the internal segment
 * keeps the cell readable without widening what is shown. A value that does not
 * carry the marker is returned verbatim — nothing is invented, appended, or
 * un-redacted.
 */
export function redactApiKeyPrefix(prefix: string): string {
  if (!prefix.startsWith(DATA_PLANE_MARKER)) return prefix;
  return PUBLIC_MARKER + prefix.slice(DATA_PLANE_MARKER.length);
}

/** Marks a timestamp the dashboard cannot state honestly. */
export const UNKNOWN_DATE_LABEL = "—";

/**
 * Render a server timestamp in the reader's locale.
 *
 * A hand-edited config can carry a non-string `createdAt`, which the listener
 * salvages to an empty string instead of discarding a working key. Rendering
 * that as `Invalid Date` would state something false about the key, so an
 * absent or unparseable value becomes an em dash — and never a guessed date.
 */
export function formatKeyTimestamp(iso: string, localeTag?: string): string {
  if (iso === "") return UNKNOWN_DATE_LABEL;
  const parsed = new Date(iso);
  if (Number.isNaN(parsed.getTime())) return UNKNOWN_DATE_LABEL;
  return parsed.toLocaleDateString(localeTag);
}
