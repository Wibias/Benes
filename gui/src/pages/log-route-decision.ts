/** Benes dashboard client for the Go proxy (`internal/server`). */
/**
 * Validation for the `routeDecision` envelope that reaches the Logs inspector.
 *
 * These payloads come from the session cache and from the Diagnostics API, so they are
 * untrusted JSON: the inspector dereferences nested `profile`/`selected`/`candidates` fields,
 * which means a malformed envelope has to be rejected at the boundary instead of crashing the
 * detail panel.
 *
 * The field vocabulary is declared once, as the string tuples below, and both the public types
 * and the runtime guards are derived from it. That keeps the wire shape and its validation from
 * drifting apart when a field is added.
 */

/** Leaf ids the envelope may carry, grouped by the object that owns them. */
const PROFILE_FIELDS = ["id", "revision"] as const;
const SELECTED_FIELDS = ["provider", "model", "reason"] as const;
const CANDIDATE_FIELDS = ["provider", "model"] as const;
const EXCLUSION_FIELDS = ["code"] as const;

/** Scalar entry fields, and the primitive each one is measured against. */
const ENTRY_SCALARS = {
  timestamp: "number",
  model: "string",
  provider: "string",
  status: "number",
  durationMs: "number",
} as const;

/** Every listed field is optional, and a string whenever it is present. */
type OptionalStringFields<Names extends readonly string[]> = Partial<Record<Names[number], string>>;

export interface LogRouteDecision {
  routeKind?: string;
  profile?: OptionalStringFields<typeof PROFILE_FIELDS>;
  selected?: OptionalStringFields<typeof SELECTED_FIELDS>;
  candidates?: Array<
    OptionalStringFields<typeof CANDIDATE_FIELDS>
    & { eligible?: boolean; exclusions?: Array<OptionalStringFields<typeof EXCLUSION_FIELDS>> }
  >;
}

export type LogEntryBase = {
  requestId?: string;
  routeDecision?: LogRouteDecision;
} & { [Field in keyof typeof ENTRY_SCALARS]: typeof ENTRY_SCALARS[Field] extends "number" ? number : string };

type Guard = (value: unknown) => boolean;

function isRecordOf(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null;
}

/** Absent is acceptable; present must pass `guard`. */
function optional(guard: Guard): Guard {
  return value => value === undefined || guard(value);
}

/** Arrays are checked element-wise; a non-array fails. */
function arrayOf(element: Guard): Guard {
  return value => Array.isArray(value) && value.every(element);
}

/**
 * An object whose named fields each pass their guard. A field the table does not name is
 * ignored: the server may add to this envelope without breaking the board that reads it.
 */
function fieldsOf(fieldGuards: Record<string, Guard>): Guard {
  const entries = Object.entries(fieldGuards);
  return value => {
    if (!isRecordOf(value)) return false;
    return entries.every(([field, guard]) => guard(value[field]));
  };
}

/** An object whose listed fields are each an optional string. */
function stringsOf(names: readonly string[]): Guard {
  const fieldGuards = Object.fromEntries(names.map(name => [name, optional(value => typeof value === "string")]));
  return fieldsOf(fieldGuards);
}

/** Absent, or an array whose every element is an optional-string object. */
function optionalStringRecords(names: readonly string[]): Guard {
  return optional(arrayOf(stringsOf(names)));
}

const DECISION_GUARD: Guard = optional(fieldsOf({
  routeKind: optional(value => typeof value === "string"),
  profile: optional(stringsOf(PROFILE_FIELDS)),
  selected: optional(stringsOf(SELECTED_FIELDS)),
  candidates: optional(arrayOf(fieldsOf({
    ...Object.fromEntries(CANDIDATE_FIELDS.map(name => [name, optional(value => typeof value === "string")])),
    eligible: optional(value => typeof value === "boolean"),
    exclusions: optionalStringRecords(EXCLUSION_FIELDS),
  }))),
}));

const ENTRY_GUARD: Guard = fieldsOf(
  Object.fromEntries(
    Object.entries(ENTRY_SCALARS).map(([field, scalar]) => [field, value => typeof value === scalar]),
  ),
);

export function validCachedRouteDecision(routeDecision: LogRouteDecision | undefined): boolean {
  return DECISION_GUARD(routeDecision);
}

export function validCachedLogEntry(entry: unknown): entry is LogEntryBase {
  if (!ENTRY_GUARD(entry)) return false;
  return validCachedRouteDecision((entry as LogEntryBase).routeDecision);
}

export function validCachedLogs<T extends LogEntryBase>(cached: T[] | null): T[] | null {
  if (!Array.isArray(cached)) return null;
  return cached.every(validCachedLogEntry) ? cached : null;
}

/**
 * Drop a decision the inspector cannot render and hand back the row unchanged otherwise.
 * A valid row keeps its identity so callers can compare references; an invalid one is copied
 * rather than mutated, because these rows are shared with the cache they were read from.
 */
export function sanitizeLogEntryRouteDecision<T extends LogEntryBase>(entry: T): T {
  if (entry.routeDecision === undefined) return entry;
  if (validCachedRouteDecision(entry.routeDecision)) return entry;
  const withoutDecision = { ...entry };
  delete withoutDecision.routeDecision;
  return withoutDecision;
}