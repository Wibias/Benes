/**
 * Provider detail view projections.
 *
 * The detail shell owns two facts that the tab head and the panel host have to
 * agree on: which tabs exist for the resolved provider shape, and the DOM id
 * pairing that binds a tab to its panel. Both live here so the tab strip, the
 * buttons, and the panel container cannot drift apart.
 */
import type { TFn } from "../i18n/shared";
import type { DetailTab } from "./detail-tabs";

export interface DetailTabEntry {
  id: DetailTab;
  label: string;
}

/**
 * Tab order for every provider. Access appears only when the resolved access
 * descriptor has at least one lane, so a provider with no credential surface
 * never renders an empty tab.
 */
export function detailTabEntries(showAccess: boolean, t: TFn): DetailTabEntry[] {
  const entries: DetailTabEntry[] = [{ id: "overview", label: t("pws.tab.overview") }];
  if (showAccess) entries.push({ id: "access", label: t("pws.tab.access") });
  entries.push({ id: "configuration", label: t("pws.tab.configuration") });
  return entries;
}

export interface DetailTabDomIds {
  tabId: string;
  panelId: string;
}

/** Single mint for the `pws-tab-*` / `pws-panel-*` pair the ARIA contract names. */
export function detailTabDomIds(tab: DetailTab): DetailTabDomIds {
  return { tabId: `pws-tab-${tab}`, panelId: `pws-panel-${tab}` };
}
