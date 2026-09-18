import { navigateHash, normalizeHashPath } from "../hash-routing.ts";

export type RoutingSurface =
  | "profiles"
  | "evaluation"
  | "analytics"
  | "compatibility"
  | "combos";

export type RoutingPrimaryTab = "profiles" | "compatibility" | "combos";

export const ROUTING_TABS: readonly RoutingPrimaryTab[] = [
  "profiles",
  "compatibility",
  "combos",
];

export function isRoutingDataSurface(surface: RoutingSurface): boolean {
  return surface === "profiles" || surface === "evaluation" || surface === "analytics";
}

export function routingPrimaryTab(surface: RoutingSurface): RoutingPrimaryTab {
  if (surface === "compatibility" || surface === "combos") return surface;
  return "profiles";
}

export function mountedRoutingSurfaces(
  current: ReadonlySet<RoutingSurface>,
  next: RoutingSurface,
): ReadonlySet<RoutingSurface> {
  const mounted = new Set(current);
  mounted.add(next);
  if (isRoutingDataSurface(next)) mounted.add("profiles");
  return mounted;
}

export function routingSurfaceHash(surface: RoutingSurface): string {
  return surface === "profiles" ? "routing" : `routing/${surface}`;
}

function isCombosHash(raw: string): boolean {
  return raw === "routing/combos"
    || raw === "startup/combos"
    || raw === "models/combos"
    || raw === "combos"
    || raw.startsWith("combos/");
}

function isEvaluationHash(raw: string): boolean {
  return raw === "routing/evaluation"
    || raw === "startup/evaluation"
    || raw === "evaluation"
    || raw.startsWith("evaluation/");
}

function isAnalyticsHash(raw: string): boolean {
  return raw === "routing/analytics"
    || raw === "startup/analytics"
    || raw === "analytics"
    || raw.startsWith("analytics/");
}

function isCompatibilityHash(raw: string): boolean {
  return raw === "routing/compatibility"
    || raw === "startup/compatibility"
    || raw === "models/compatibility"
    || raw === "lab"
    || raw.startsWith("lab/");
}

export function readRoutingSurface(hash = window.location.hash): RoutingSurface {
  const raw = normalizeHashPath(hash);
  if (isCombosHash(raw)) return "combos";
  if (isEvaluationHash(raw)) return "evaluation";
  if (isAnalyticsHash(raw)) return "analytics";
  if (isCompatibilityHash(raw)) return "compatibility";
  return "profiles";
}

/** Deliberate navigation pushes history so Back/Forward restores the Routing surface. */
export function selectRoutingSurface(next: RoutingSurface): void {
  navigateHash(routingSurfaceHash(next));
}

export function routingTabDomId(tab: RoutingPrimaryTab): string {
  return `routing-tab-${tab}`;
}

export function routingPanelDomId(tab: Exclude<RoutingPrimaryTab, "profiles">): string {
  return `routing-panel-${tab}`;
}

export const ROUTING_PROFILES_PANEL_ID = "routing-panel-profiles";

/**
 * The primary tab an arrow (or Home/End) key moves to, or `null` when the key is not one
 * the strip handles.
 *
 * The APG tabs pattern wraps at both ends, so Left from the first tab is the last one and
 * Right from the last is the first. The movement is resolved here, apart from the DOM, so
 * the strip only has to focus the tab this returns and cannot disagree with the registry
 * about what the tabs are or which order they sit in.
 */
export function nextRoutingTab(tab: RoutingPrimaryTab, key: string): RoutingPrimaryTab | null {
  const last = ROUTING_TABS.length - 1;
  const index = ROUTING_TABS.indexOf(tab);
  if (key === "Home") return ROUTING_TABS[0]!;
  if (key === "End") return ROUTING_TABS[last]!;
  if (key === "ArrowLeft") return ROUTING_TABS[(index - 1 + ROUTING_TABS.length) % ROUTING_TABS.length]!;
  if (key === "ArrowRight") return ROUTING_TABS[(index + 1) % ROUTING_TABS.length]!;
  return null;
}
