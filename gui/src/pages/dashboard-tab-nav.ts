import { DASHBOARD_TAB_HASHES } from "../app-routing.ts";
import { normalizeHashPath } from "../hash-routing.ts";

/** The Dashboard's sections. Overview is the page root; the others are named tabs. */
export type DashboardSection = "overview" | "providers" | "models";

/** A page knows its own hash root; the named section hashes belong to the route registry. */
const DASHBOARD_ROOT = "dashboard";

/** The marker stripping belongs to hash routing; this only classifies the resulting tail. */
function dashboardHashTail(): string {
  return normalizeHashPath(window.location.hash);
}

/** The registry owns which sections exist; this only names the one the tail selects. */
export function readDashboardSectionFromHash(): DashboardSection {
  const tail = dashboardHashTail();
  const match = DASHBOARD_TAB_HASHES.find(hash => hash === tail);
  if (!match) return "overview";
  return match.slice(match.lastIndexOf("/") + 1) as DashboardSection;
}

/** Overview is the bare `#dashboard`; the other sections carry the registry's own suffix. */
export function dashboardHashForSection(section: DashboardSection): string {
  if (section === "overview") return DASHBOARD_ROOT;
  return DASHBOARD_TAB_HASHES.find(hash => hash.endsWith(`/${section}`)) ?? DASHBOARD_ROOT;
}

export const DASHBOARD_TAB_IDS: DashboardSection[] = ["overview", "providers", "models"];

/**
 * Keyboard target for the Dashboard tablist.
 * Arrow keys wrap. Home/End jump to the ends. Other keys are ignored.
 */
export function nextDashboardTabIndex(
  currentIndex: number,
  key: string,
  tabCount: number,
): number | null {
  if (tabCount <= 0) return null;
  if (key === "ArrowRight") return (currentIndex + 1) % tabCount;
  if (key === "ArrowLeft") return (currentIndex - 1 + tabCount) % tabCount;
  if (key === "Home") return 0;
  if (key === "End") return tabCount - 1;
  return null;
}

export function dashboardTabButtonId(section: DashboardSection): string {
  return `dashboard-tab-${section}`;
}

export function dashboardPanelId(section: DashboardSection): string {
  return `dashboard-panel-${section}`;
}

export function dashboardTabA11y(section: DashboardSection, selected: DashboardSection) {
  const active = section === selected;
  return {
    id: dashboardTabButtonId(section),
    role: "tab" as const,
    "aria-selected": active,
    "aria-controls": dashboardPanelId(section),
    tabIndex: active ? 0 : -1,
  };
}

export function dashboardPanelA11y(section: DashboardSection, overview: boolean) {
  return {
    id: dashboardPanelId(section),
    role: overview ? undefined : "tabpanel" as const,
    "aria-labelledby": overview ? undefined : dashboardTabButtonId(section),
    tabIndex: 0 as const,
  };
}

/** Resolve a tablist key to the next section, or null when the key is not a tab move. */
export function dashboardSectionAfterTabKey(
  selected: DashboardSection,
  key: string,
): DashboardSection | null {
  const current = DASHBOARD_TAB_IDS.indexOf(selected);
  const nextIndex = nextDashboardTabIndex(current, key, DASHBOARD_TAB_IDS.length);
  if (nextIndex == null) return null;
  return DASHBOARD_TAB_IDS[nextIndex] ?? null;
}

/** Overview uses the plane shell and omits the tablist. */
export function dashboardWorkspaceChrome(section: DashboardSection) {
  const overview = section === "overview";
  return {
    overview,
    shellClass: overview
      ? "dashboard-workspace-shell dashboard-workspace-shell--plane"
      : "dashboard-workspace-shell",
    showTablist: !overview,
  };
}
