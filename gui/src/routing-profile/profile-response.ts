/**
 * Routing-profile write responses.
 *
 * The management API answers with `{ success: true }` or an error envelope. A
 * malformed body is neither a success nor a message: it is "unknown", and the
 * caller falls back to its own copy.
 */

function asRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function nonBlankString(value: unknown): string | undefined {
  if (typeof value !== "string" || value.trim() === "") return undefined;
  return value;
}

/** Error copy from a routing-profile response: a plain string or a nested message. */
export function routingProfileResponseError(data: unknown): string | undefined {
  const body = asRecord(data);
  if (!body) return undefined;
  const direct = nonBlankString(body.error);
  if (direct !== undefined) return direct;
  const envelope = asRecord(body.error);
  return envelope ? nonBlankString(envelope.message) : undefined;
}

/** Only an explicit `success: true` counts as a successful write. */
export function routingProfileResponseSucceeded(data: unknown): boolean {
  return asRecord(data)?.success === true;
}
