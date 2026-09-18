/**
 * The dashboard's two ways of writing the address bar.
 *
 * A *correction* is passive: the router noticed a retired bookmark or an unowned sub-path
 * and rewrites the address the browser is already showing, keeping the history entry the
 * user is standing on. A *navigation* is deliberate: the user picked a destination, so the
 * browser pushes an entry and `hashchange` runs for every listener.
 *
 * Both writers take the same normalized target and settle "is this already the address?"
 * through the same comparison, so the two cannot disagree about what the same hash is.
 */

/** The `#` or `#/` marker a hash may carry. Only one is ever consumed. */
const HASH_MARKER = /^#\/?/;

/** A dashboard hash without its marker, as stored in `location.hash` bodies and route tables. */
export function normalizeHashPath(hash: string): string {
  return hash.replace(HASH_MARKER, "");
}

/** A hash split at the first `?`, before the query text is interpreted. */
interface HashTarget {
  /** Everything before the first `?`. */
  path: string;
  /** Everything after it, or `""` when the hash carried no query. */
  queryText: string;
}

function splitTarget(target: string): HashTarget {
  const separator = target.indexOf("?");
  if (separator === -1) return { path: target, queryText: "" };
  return { path: target.slice(0, separator), queryText: target.slice(separator + 1) };
}

/** A dashboard hash's path and its parsed query. */
export interface HashRoute {
  path: string;
  query: URLSearchParams;
}

/**
 * Split a dashboard hash into path and query (`#logs?sessionId=ses_…`).
 *
 * The query grammar is `URLSearchParams`, not a local parser: `+`, `%20`, and repeated
 * keys all behave the way the page-local readers already expect them to.
 */
export function parseHashRoute(hash: string): HashRoute {
  const { path, queryText } = splitTarget(normalizeHashPath(hash));
  return { path, query: new URLSearchParams(queryText) };
}

/**
 * The normalized target, or `null` when the address bar already shows it.
 *
 * Shared by both writers so an unchanged hash can never be rewritten twice, and so
 * `#logs` and `logs` compare equal whichever spelling the caller passed.
 */
function pendingTarget(hash: string, win: Window): string | null {
  const target = normalizeHashPath(hash);
  return normalizeHashPath(win.location.hash) === target ? null : target;
}

/**
 * Passive URL correction: replace the current history entry.
 *
 * `history.replaceState` emits no `hashchange`, so callers update their own state; the
 * point of the replace is that Back leaves the page instead of bouncing off a hash the
 * router would immediately rewrite again.
 */
export function replaceHash(hash: string, win: Window = window): void {
  const target = pendingTarget(hash, win);
  if (target === null) return;
  const { pathname, search } = win.location;
  win.history.replaceState(win.history.state, "", `${pathname}${search}#${target}`);
}

/**
 * Deliberate user navigation: assign `location.hash` so the browser pushes an entry and
 * emits `hashchange` for listeners.
 */
export function navigateHash(hash: string, win: Window = window): void {
  const target = pendingTarget(hash, win);
  if (target === null) return;
  win.location.hash = target;
}
