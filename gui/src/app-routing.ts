/**
 * The dashboard's route registry.
 *
 * Two contracts live here. *Ownership* says which hashes a page accepts: every page declares
 * a canonical root, the literal sub-hashes and prefix families it adds to that root, and —
 * for the pages whose hash carries real state — the validator that decides whether a query
 * belongs to it. *Resolution* says what a retired bookmark becomes: the roots that no longer
 * exist under their own name, and the surfaces that moved between pages, each with the
 * canonical hash its bookmarks correct to.
 *
 * Nothing here navigates. Resolution returns the page to render plus, at most, a hash for
 * the caller to replace passively; `hash-routing.ts` owns the writing.
 */
import { normalizeHashPath, parseHashRoute } from "./hash-routing.ts";
import { logsHashIsAllowed } from "./pages/logs-session-filter.ts";
import { sessionsHashIsAllowed } from "./pages/sessions-hash-filter.ts";
import { storageHashIsAllowed } from "./pages/storage-tab.ts";
import {
  OPENAI_ACCOUNT_ACCESS_HASH,
  readProviderAccessHash,
} from "./provider-workspace/provider-access-hash.ts";

export type Page =
  | "dashboard"
  | "startup"
  | "routing"
  | "providers"
  | "models"
  | "subagents"
  | "sessions"
  | "logs"
  | "usage"
  | "tasks"
  | "storage"
  | "api"
  | "integrations"
  | "harnesses";

/** The bare `#dashboard` is Overview, so only the named sections are listed here. */
export const DASHBOARD_TAB_HASHES = ["dashboard/providers", "dashboard/models"] as const;

/** Routing's sub-surfaces. Profiles is the bare `#routing`, so it has no entry. */
const ROUTING_SURFACES = new Map<string, string>([
  ["combos", "routing/combos"],
  ["evaluation", "routing/evaluation"],
  ["analytics", "routing/analytics"],
  ["compatibility", "routing/compatibility"],
]);

/** The surface word the pages that used to host Routing used for Routing itself. */
const ROUTING_ROOT_WORD = "routing";

/**
 * Routing's canonical hashes, derived from the surface map so the route table and the legacy
 * rewrite targets cannot drift apart.
 */
export const ROUTING_HASHES: readonly string[] = [...ROUTING_SURFACES.values()];

/** API Access tabs. Keys is the bare `#api`; `#api/keys` is a retired spelling of it. */
export const API_TAB_HASHES = ["api/clients", "api/endpoints", "api/models", "api/examples"] as const;

/** Usage's canonical hashes. */
export const USAGE_HASHES = [
  "usage/breakdown",
  "usage/breakdown/providers",
  "usage/breakdown/accounts",
  "usage/coverage",
] as const;

/** A page's hash vocabulary. */
interface RouteOwnership {
  /** The hash that is the page's canonical entry point. */
  readonly root: string;
  /** Literal sub-hashes accepted besides the root. */
  readonly literals?: readonly string[];
  /** Prefix families accepted besides the root (`harnesses/<id>`). */
  readonly families?: readonly string[];
  /**
   * Recognizer for hashes whose shape is a page-local grammar rather than a literal, such
   * as `providers/<name>/access`. Consulted only for query-free hashes.
   */
  readonly recognizes?: (path: string) => boolean;
  /**
   * The page's own validator for hashes carrying query state. When present it is the whole
   * answer — the page owns its query grammar, including which queries it refuses — so the
   * literal rules below are not consulted at all.
   */
  readonly validateQuery?: (path: string, query: URLSearchParams) => boolean;
}

const PAGE_ROUTES: ReadonlyMap<Page, RouteOwnership> = new Map<Page, RouteOwnership>([
  ["dashboard", {
    root: "dashboard",
    literals: [...DASHBOARD_TAB_HASHES],
  }],
  ["startup", { root: "startup" }],
  ["routing", { root: "routing", literals: ROUTING_HASHES }],
  ["providers", {
    root: "providers",
    recognizes: path => readProviderAccessHash(path) !== null,
  }],
  ["models", { root: "models" }],
  ["subagents", { root: "subagents" }],
  ["sessions", { root: "sessions", validateQuery: sessionsHashIsAllowed }],
  ["logs", { root: "logs", validateQuery: logsHashIsAllowed }],
  ["usage", { root: "usage", literals: USAGE_HASHES }],
  ["tasks", { root: "tasks" }],
  ["storage", { root: "storage", validateQuery: storageHashIsAllowed }],
  ["api", { root: "api", literals: API_TAB_HASHES }],
  ["integrations", { root: "integrations" }],
  ["harnesses", { root: "harnesses", families: ["harnesses/"] }],
]);

/** Every page the router can render. Derived from the registry so a page cannot be half-added. */
export const VALID_PAGES: ReadonlySet<Page> = new Set(PAGE_ROUTES.keys());

/** True when the hash carried at least one query parameter. */
function hasQuery(query: URLSearchParams): boolean {
  return query.keys().next().done === false;
}

function pageOwnsPath(route: RouteOwnership, path: string): boolean {
  if (path === route.root) return true;
  if (route.literals?.includes(path)) return true;
  if (route.families?.some(family => path.startsWith(family))) return true;
  return route.recognizes?.(path) ?? false;
}

/**
 * Whether a page accepts a hash.
 *
 * A page with query state answers through its own validator, because only that page knows
 * which parameters it reads. For every other page a query is not part of the vocabulary at
 * all, so an unknown one is refused rather than ignored.
 */
export function hashBelongsToPage(rawHash: string, page: Page): boolean {
  const route = PAGE_ROUTES.get(page);
  if (route === undefined) return false;
  const { path, query } = parseHashRoute(rawHash);
  if (route.validateQuery !== undefined) return route.validateQuery(path, query);
  if (hasQuery(query)) return false;
  return pageOwnsPath(route, path);
}

/** Result of resolving an incoming hash. */
export type AppHashChangeAction = {
  page: Page;
  /** When non-null, passively replace the hash (no new history entry). */
  replaceTo: string | null;
};

/** Prefix match on `/` boundaries: the hash itself, or anything nested under it. */
function routeMatchesPrefix(rawHash: string, prefix: string): boolean {
  return rawHash === prefix || rawHash.startsWith(`${prefix}/`);
}

const DEBUG_ROOT = "debug";
const CODEX_AUTH_ROOT = "codex-auth";
const LOGS_DEBUG_HASH = "logs/debug";
const INTEGRATIONS_ROOT = "integrations";
/** Its retired API-key surface, spelled in full because it is a bookmark, not a join. */
const INTEGRATIONS_API_KEYS = "integrations/keys";

/**
 * Roots that no longer exist under their own name, and the page that inherited them.
 * Resolved first so a cold bookmark paints the right board instead of flashing another one
 * until the passive replace lands.
 */
const RETIRED_ROOTS = new Map<string, Page>([
  [DEBUG_ROOT, "logs"],
  [CODEX_AUTH_ROOT, "providers"],
  ["combos", "routing"],
  ["evaluation", "routing"],
  ["analytics", "routing"],
  ["lab", "routing"],
  ["claude", "harnesses"],
  ["grok", "harnesses"],
]);

/**
 * Surfaces Routing absorbed from the pages that used to host them. The sets differ per host
 * — `models` never hosted Evaluation or Analytics — so this stays a map, not one shared set.
 */
const FORMER_ROUTING_SURFACES = new Map<string, ReadonlySet<string>>([
  ["startup", new Set(["combos", ROUTING_ROOT_WORD, "evaluation", "analytics", "compatibility"])],
  ["models", new Set(["combos", ROUTING_ROOT_WORD, "compatibility"])],
]);

/** Standalone roots Routing absorbed, and the surface word each of them became. */
const RETIRED_ROUTING_ROOTS = new Map<string, string>([
  ["combos", "combos"],
  ["evaluation", "evaluation"],
  ["analytics", "analytics"],
  ["lab", "compatibility"],
]);

/** Standalone harness roots, and the harness id each of them became. */
const RETIRED_HARNESS_ROOTS = new Map<string, string>([
  ["claude", "claude"],
  ["grok", "grok"],
]);

/** Integrations sub-paths that were renamed on the way to Harnesses. */
const RENAMED_INTEGRATIONS_SURFACES = new Map<string, string>([
  [`${INTEGRATIONS_ROOT}/claude/desktop`, "claude-desktop"],
]);

/** The current location hash, or `""` outside a browser. */
function liveHash(): string {
  return typeof window === "undefined" ? "" : window.location.hash;
}

/**
 * Integrations is a transition alias rather than a destination: its API-key surface moved to
 * API Access and everything else moved to Harnesses. `raw` keeps the query, because that is
 * how the retired page was bookmarked.
 */
function integrationsInheritor(raw: string): Page {
  if (raw === INTEGRATIONS_API_KEYS || raw.startsWith(`${INTEGRATIONS_API_KEYS}/`)) return "api";
  return "harnesses";
}

/**
 * The page a hash resolves to on a cold load.
 *
 * A retired root is answered here as well as in `resolveAppHashChange`, because that rewrite
 * is applied with `replaceState`, which emits no `hashchange`: a bookmark relying on the
 * rewrite alone would render the wrong board for one paint.
 */
export function readPageFromHash(hash?: string): Page {
  const raw = normalizeHashPath(hash ?? liveHash());
  const [root = "", surface = ""] = parseHashRoute(raw).path.split("/");

  const inherited = RETIRED_ROOTS.get(root);
  if (inherited !== undefined) return inherited;
  if (FORMER_ROUTING_SURFACES.get(root)?.has(surface)) return "routing";
  if (root === INTEGRATIONS_ROOT) return integrationsInheritor(raw);
  return VALID_PAGES.has(root as Page) ? root as Page : "dashboard";
}

/** Routing's canonical hash for a surface word. */
function routingHashForSurface(surface: string): string {
  if (surface === ROUTING_ROOT_WORD) return ROUTING_ROOT_WORD;
  return ROUTING_SURFACES.get(surface) ?? ROUTING_ROOT_WORD;
}

/** The Routing surface a retired hash ended on, or null when it is not a Routing bookmark. */
function retiredRoutingSurface(raw: string): string | null {
  for (const [root, surface] of RETIRED_ROUTING_ROOTS) {
    if (routeMatchesPrefix(raw, root)) return surface;
  }
  for (const [host, surfaces] of FORMER_ROUTING_SURFACES) {
    for (const surface of surfaces) {
      if (routeMatchesPrefix(raw, `${host}/${surface}`)) return surface;
    }
  }
  return null;
}

/** The canonical hash for a retired API Access spelling, or null. */
function isRetiredApiSpelling(raw: string): boolean {
  return raw === "api/keys" || routeMatchesPrefix(raw, INTEGRATIONS_API_KEYS);
}

/** The canonical hash for a retired Harness spelling, or null. */
function retiredHarnessTarget(raw: string): string | null {
  const renamed = RENAMED_INTEGRATIONS_SURFACES.get(raw);
  if (renamed !== undefined) return `harnesses/${renamed}`;
  if (raw === INTEGRATIONS_ROOT || raw === `${INTEGRATIONS_ROOT}/`) return "harnesses";
  if (raw.startsWith(`${INTEGRATIONS_ROOT}/`)) {
    const id = raw.slice(INTEGRATIONS_ROOT.length + 1);
    return id === "" ? "harnesses" : `harnesses/${id}`;
  }
  const retired = RETIRED_HARNESS_ROOTS.get(raw);
  return retired === undefined ? null : `harnesses/${retired}`;
}

/** Legacy deep link from the removed dual-layout era; Providers has no workspace surface. */
const RETIRED_PROVIDER_SUBROUTE = "providers/workspace";

/**
 * Resolve what App should do for the current location hash.
 *
 * Any rewrite this returns is passive: callers apply it with `replaceState`, never a push, so
 * Back is never trapped on a hash the router immediately corrects.
 */
export function resolveAppHashChange(rawHash: string): AppHashChangeAction {
  const page = readPageFromHash(rawHash);

  // Retired standalone pages whose surface now lives under a different hash.
  if (routeMatchesPrefix(rawHash, DEBUG_ROOT)) return { page: "logs", replaceTo: LOGS_DEBUG_HASH };
  if (routeMatchesPrefix(rawHash, CODEX_AUTH_ROOT)) {
    return { page: "providers", replaceTo: OPENAI_ACCOUNT_ACCESS_HASH };
  }

  const routingSurface = retiredRoutingSurface(rawHash);
  if (routingSurface !== null) return { page: "routing", replaceTo: routingHashForSurface(routingSurface) };

  // Retired spellings of the bare `#api` Keys tab.
  if (isRetiredApiSpelling(rawHash)) return { page: "api", replaceTo: "api" };

  const harnessTarget = retiredHarnessTarget(rawHash);
  if (harnessTarget !== null) return { page: "harnesses", replaceTo: harnessTarget };

  // Coincides with the general unowned-subpath rule today; kept named because it is a
  // documented legacy deep link rather than a typo in a bookmark.
  if (rawHash === RETIRED_PROVIDER_SUBROUTE) return { page: "providers", replaceTo: "providers" };

  // An unrecognised sub-hash is normalized away rather than left in the URL.
  if (!hashBelongsToPage(rawHash, page)) return { page, replaceTo: page };
  return { page, replaceTo: null };
}
