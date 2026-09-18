/**
 * Benes dashboard source. The API workspace's panels, their hash addresses, and
 * the keyboard contract its tab strip follows.
 *
 * Each panel is one row of `API_TAB_SPECS`: the id, and the hash segment it owns.
 * Everything else — the ordered tab list, the hash writer, the hash reader, the
 * two DOM ids the tab/tabpanel pair share — is derived from that table, so a
 * panel's address and its aria wiring cannot drift apart, and adding a panel is
 * one row rather than an edit in five places.
 *
 * Keys is the workspace's landing panel and therefore owns the bare `#api`.
 */
import { navigateHash, normalizeHashPath } from "../hash-routing.ts";

/** How one panel is addressed: its id, and the segment under `#api`. */
interface ApiTabSpec {
  readonly id: string;
  /** `""` for the landing panel, which has no segment of its own. */
  readonly segment: string;
}

const API_TAB_SPECS = [
  { id: "keys", segment: "" },
  { id: "clients", segment: "clients" },
  { id: "endpoints", segment: "endpoints" },
  { id: "models", segment: "models" },
  { id: "examples", segment: "examples" },
] as const satisfies readonly ApiTabSpec[];

export type ApiTab = (typeof API_TAB_SPECS)[number]["id"];

/** Panel order, which is also the strip's left-to-right and arrow-key order. */
export const API_TABS: readonly ApiTab[] = API_TAB_SPECS.map(spec => spec.id);

/** The `#api` root every panel address extends. */
const API_ROOT = "api";

/** Landing panel, and the answer for any hash this workspace does not own. */
export const DEFAULT_API_TAB: ApiTab = "keys";

/** One entry per panel, keyed by id. */
function tabIndex<TValue>(value: (spec: ApiTabSpec) => TValue): Record<ApiTab, TValue> {
  const table = {} as Record<ApiTab, TValue>;
  for (const spec of API_TAB_SPECS) table[spec.id as ApiTab] = value(spec);
  return table;
}

/** The hash a panel is reached by, without the leading `#`. */
const HASH_BY_TAB: Record<ApiTab, string> = tabIndex(
  spec => (spec.segment ? `${API_ROOT}/${spec.segment}` : API_ROOT),
);

/** The reverse of `HASH_BY_TAB`, so an address and its panel can never disagree. */
function buildHashIndex(): Record<string, ApiTab> {
  const table: Record<string, ApiTab> = {};
  for (const spec of API_TAB_SPECS) {
    table[spec.segment ? `${API_ROOT}/${spec.segment}` : API_ROOT] = spec.id as ApiTab;
  }
  return table;
}

const TAB_BY_HASH: Record<string, ApiTab> = buildHashIndex();

/** The hash a panel is reached by, without the leading `#`. */
export function apiTabHash(tab: ApiTab): string {
  return HASH_BY_TAB[tab];
}

/** The tab button's id, which the panel points back at through `aria-labelledby`. */
export function apiTabDomId(tab: ApiTab): string {
  return `api-tab-${tab}`;
}

/** The panel's id, which the tab points at through `aria-controls`. */
export function apiPanelDomId(tab: ApiTab): string {
  return `api-panel-${tab}`;
}

/**
 * The panel a hash addresses, or the landing panel.
 *
 * A retired bookmark, a `#logs` deep link, or any other workspace's hash reads
 * as Keys rather than as an unknown panel, which is what lets the router land
 * here without a separate existence check.
 */
export function readApiTab(hash = typeof window !== "undefined" ? window.location.hash : ""): ApiTab {
  return TAB_BY_HASH[normalizeHashPath(hash)] ?? DEFAULT_API_TAB;
}

/** Deliberate navigation: push the panel's hash so `hashchange` runs for listeners. */
export function selectApiTab(tab: ApiTab): void {
  navigateHash(apiTabHash(tab));
}

/**
 * The panel an arrow (or Home/End) key moves to, or `null` when the key is not
 * one this strip handles.
 *
 * The APG tabs pattern wraps at both ends, so Left from the first panel is the
 * last one and Right from the last is the first. Movement is resolved apart from
 * the DOM, so the strip only has to focus the panel this returns.
 */
export function nextApiTab(tab: ApiTab, key: string): ApiTab | null {
  const last = API_TABS.length - 1;
  const index = API_TABS.indexOf(tab);
  if (key === "Home") return API_TABS[0]!;
  if (key === "End") return API_TABS[last]!;
  if (key === "ArrowLeft") return API_TABS[(index - 1 + API_TABS.length) % API_TABS.length]!;
  if (key === "ArrowRight") return API_TABS[(index + 1) % API_TABS.length]!;
  return null;
}
