/**
 * Benes dashboard source. The proxy ships its own inbound-header rules and the
 * API tab only names them.
 *
 * Acceptance is decided by the listener's admission mode, never by the
 * dashboard, so the vocabulary here is closed: a row that names anything else
 * is a payload the GUI cannot describe honestly and is rejected outright.
 */

/** The only three answers the listener gives for one header on one endpoint. */
export const AUTH_DISPOSITIONS = ["required", "accepted", "rejected"] as const;

export type ApiAuthDisposition = (typeof AUTH_DISPOSITIONS)[number];

export interface ApiAuthMatrixRow {
  endpoint: string;
  bearer: ApiAuthDisposition;
  dedicated: ApiAuthDisposition;
  xApiKey: ApiAuthDisposition;
}

/** One verdict column: the inbound header it describes, and the field behind it. */
export interface AuthMatrixColumn {
  /** The header as the listener spells it, printed as machine text. */
  readonly header: string;
  readonly field: "bearer" | "dedicated" | "xApiKey";
}

/**
 * The three header verdicts, in the order the board prints them.
 *
 * The header named here and the row field it reads are one binding: a column
 * cannot be drawn against a verdict the listener did not send, because this is
 * the same field list `parseApiAuthMatrix` validates.
 */
export const AUTH_MATRIX_COLUMNS = [
  { header: "Authorization: Bearer", field: "bearer" },
  { header: "x-benes-api-key", field: "dedicated" },
  { header: "x-api-key", field: "xApiKey" },
] as const satisfies readonly AuthMatrixColumn[];

function readDisposition(value: unknown): ApiAuthDisposition | null {
  for (const known of AUTH_DISPOSITIONS) {
    if (known === value) return known;
  }
  return null;
}

/**
 * Validate the header matrix before any of it is rendered.
 *
 * An empty table is a failure rather than an empty state: the route always
 * lists the data-plane endpoints, so zero rows means the response was not the
 * matrix. Returning `null` lets the caller show its load-failure path instead
 * of guessing what the listener would accept.
 */
export function parseApiAuthMatrix(input: unknown): ApiAuthMatrixRow[] | null {
  if (!Array.isArray(input) || input.length === 0) return null;
  const parsed: ApiAuthMatrixRow[] = [];
  for (const entry of input) {
    if (entry === null || typeof entry !== "object") return null;
    const row = entry as Record<string, unknown>;
    const endpoint = typeof row.endpoint === "string" ? row.endpoint : "";
    const bearer = readDisposition(row.bearer);
    const dedicated = readDisposition(row.dedicated);
    const xApiKey = readDisposition(row.xApiKey);
    if (endpoint === "" || bearer === null || dedicated === null || xApiKey === null) return null;
    parsed.push({ endpoint, bearer, dedicated, xApiKey });
  }
  return parsed;
}
