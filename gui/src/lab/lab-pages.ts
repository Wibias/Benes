/**
 * Paginated `/api/lab/*` collections.
 *
 * The network boundary is stricter than the permissive record readers: a page
 * either satisfies the contract in full or is rejected. Partial pages are never
 * silently trimmed into something that looks complete.
 */

import { isPlainObject } from "./lab-records.ts";

/**
 * Thrown when a Lab response cannot satisfy the dashboard read contract.
 *
 * The reason is diagnostic only and never becomes `message`: the dashboard
 * shows its own localized copy instead of raw English from this layer.
 */
export class LabDataContractError extends Error {
  readonly reason: string;

  constructor(reason: string) {
    super();
    this.name = "LabDataContractError";
    this.reason = reason;
  }
}

/**
 * One page of a Lab collection. A continuing page always carries its cursor, so
 * "more rows with no way to reach them" cannot be represented.
 */
export type LabPage<TResult> =
  | { rows: TResult[]; hasMore: false }
  | { rows: TResult[]; hasMore: true; nextCursor: string };

export type CollectedRows<TResult> = {
  rows: TResult[];
  truncated: boolean;
};

/** Collection key plus row reader for one paginated Lab route. */
export type LabCollection<TResult> = {
  readonly key: string;
  readonly readRow: (value: unknown) => TResult | null;
};

/** Page size every paginated Lab request sends. */
export const LAB_PAGE_SIZE = 50;

/** Hard bound on pages collected for one logical list. */
export const LAB_PAGE_WALK_MAX = 200;

/** Pagination metadata that survived validation. */
type PageContract = { hasMore: false } | { hasMore: true; nextCursor: string };

function readPageContract(cells: Record<string, unknown>): PageContract {
  const rawHasMore = cells.hasMore;
  if (rawHasMore !== undefined && typeof rawHasMore !== "boolean") {
    throw new LabDataContractError("hasMore is not a boolean");
  }
  const rawCursor = cells.nextCursor;
  if (rawCursor !== undefined && typeof rawCursor !== "string") {
    throw new LabDataContractError("nextCursor is not a string");
  }
  if (rawHasMore !== true) return { hasMore: false };
  if (!rawCursor) throw new LabDataContractError("hasMore without nextCursor");
  return { hasMore: true, nextCursor: rawCursor };
}

/** Read one page, rejecting any row the collection cannot represent. */
export function readLabPage<TResult>(raw: unknown, collection: LabCollection<TResult>): LabPage<TResult> {
  if (!isPlainObject(raw)) throw new LabDataContractError("Lab page is not an object");
  const list = raw[collection.key];
  if (!Array.isArray(list)) throw new LabDataContractError(`missing ${collection.key} collection`);
  const rows: TResult[] = [];
  for (const entry of list as unknown[]) {
    const row = collection.readRow(entry);
    if (!row) throw new LabDataContractError(`unreadable ${collection.key} row`);
    rows.push(row);
  }
  return { rows, ...readPageContract(raw) };
}

/** Cursors already visited in one walk; a repeat means the server looped. */
class CursorTrail {
  private readonly visited = new Set<string>();

  /** Record the cursor a continuing page points at, rejecting a stalled one. */
  advance(from: string | undefined, to: unknown): string {
    if (typeof to !== "string" || to === "" || to === from) {
      throw new LabDataContractError("pagination cursor did not advance");
    }
    if (this.visited.has(to)) throw new LabDataContractError("pagination cursor repeated");
    this.visited.add(to);
    return to;
  }
}

/**
 * Collect every page of one list. A cursor that does not advance, or a cursor
 * seen before, is a contract violation. Reaching the walk bound returns a
 * truncated result instead of pretending the list is complete.
 */
export async function walkPages<TResult>(
  loadPage: (cursor: string | undefined) => Promise<LabPage<TResult>>,
): Promise<CollectedRows<TResult>> {
  const rows: TResult[] = [];
  const trail = new CursorTrail();
  let cursor: string | undefined;
  for (let hop = 0; hop < LAB_PAGE_WALK_MAX; hop += 1) {
    const page = await loadPage(cursor);
    rows.push(...page.rows);
    if (!page.hasMore) return { rows, truncated: false };
    cursor = trail.advance(cursor, page.nextCursor);
  }
  return { rows, truncated: true };
}
