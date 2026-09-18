/**
 * Benes dashboard source. The Routing workspace's primary tab strip.
 *
 * A presentational strip: which tab an arrow key moves to is `nextRoutingTab`'s decision
 * (see `./routing-tab`), and this file only turns that decision into focus movement and
 * renders the APG tab/tabpanel wiring. Roving `tabIndex`, `aria-selected`, and each
 * button's `aria-controls` come from the same registry the panels read their ids from, so
 * a tab and the panel it addresses cannot drift apart.
 */
import { useRef, type KeyboardEvent } from "react";
import { useT, type TKey } from "../i18n/shared";
import {
  ROUTING_PROFILES_PANEL_ID,
  ROUTING_TABS,
  nextRoutingTab,
  routingPanelDomId,
  routingPrimaryTab,
  routingTabDomId,
  type RoutingPrimaryTab,
  type RoutingSurface,
} from "./routing-tab";

const TAB_LABEL: Record<RoutingPrimaryTab, TKey> = {
  profiles: "routing.tab.profiles",
  compatibility: "models.tab.compatibility",
  combos: "models.tab.combos",
};

/** The panel a tab addresses; the profiles panel publishes its own id. */
function panelIdFor(tab: RoutingPrimaryTab): string {
  return tab === "profiles" ? ROUTING_PROFILES_PANEL_ID : routingPanelDomId(tab);
}

export function RoutingTabStrip({
  surface,
  onSelect,
}: {
  surface: RoutingSurface;
  onSelect: (next: RoutingSurface) => void;
}) {
  const t = useT();
  const buttons = useRef(new Map<RoutingPrimaryTab, HTMLButtonElement>());
  const active = routingPrimaryTab(surface);

  const move = (next: RoutingPrimaryTab) => {
    onSelect(next);
    // The moved-to tab is focused once the panel re-renders, so focus lands on the tab
    // that is now selected instead of the one that lost selection.
    window.requestAnimationFrame(() => buttons.current.get(next)?.focus({ preventScroll: true }));
  };

  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    const next = nextRoutingTab(active, event.key);
    if (next === null) return;
    event.preventDefault();
    move(next);
  };

  return (
    <div className="page-tabs" role="tablist" aria-label={t("routing.tabsLabel")}>
      {ROUTING_TABS.map(tab => (
        <RoutingTabButton
          key={tab}
          tab={tab}
          label={t(TAB_LABEL[tab])}
          selected={tab === active}
          register={node => {
            if (node) buttons.current.set(tab, node);
            else buttons.current.delete(tab);
          }}
          onSelect={() => move(tab)}
          onKeyDown={onKeyDown}
        />
      ))}
    </div>
  );
}

/** One tab control. `tabIndex` is roving: only the selected tab is reachable by Tab. */
function RoutingTabButton({
  tab,
  label,
  selected,
  register,
  onSelect,
  onKeyDown,
}: {
  tab: RoutingPrimaryTab;
  label: string;
  selected: boolean;
  register: (node: HTMLButtonElement | null) => void;
  onSelect: () => void;
  onKeyDown: (event: KeyboardEvent<HTMLButtonElement>) => void;
}) {
  return (
    <button
      ref={register}
      type="button"
      role="tab"
      id={routingTabDomId(tab)}
      aria-selected={selected}
      aria-controls={panelIdFor(tab)}
      tabIndex={selected ? 0 : -1}
      className={selected ? "page-tab page-tab--active" : "page-tab"}
      onClick={onSelect}
      onKeyDown={onKeyDown}
    >
      {label}
    </button>
  );
}
