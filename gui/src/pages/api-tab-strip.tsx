/**
 * Benes dashboard source. The API workspace's tab strip.
 *
 * A presentational strip: which panel an arrow key moves to is `nextApiTab`'s
 * decision (see `./api-tab`), and this file only turns that decision into focus
 * movement and renders the APG tab/tabpanel wiring. Roving `tabIndex`,
 * `aria-selected`, and each button's `aria-controls` all come from the same
 * registry the panels read their ids from.
 */
import { useRef, type KeyboardEvent } from "react";
import { useT, type TKey } from "../i18n/shared";
import { API_TABS, apiPanelDomId, apiTabDomId, nextApiTab, type ApiTab } from "./api-tab";

const TAB_LABEL: Record<ApiTab, TKey> = {
  keys: "api.section.keys",
  clients: "api.section.clients",
  endpoints: "api.section.endpoints",
  models: "api.section.models",
  examples: "api.section.examples",
};

export function ApiTabStrip({
  tab,
  onSelect,
}: {
  tab: ApiTab;
  onSelect: (next: ApiTab) => void;
}) {
  const t = useT();
  const buttons = useRef(new Map<ApiTab, HTMLButtonElement>());

  const move = (next: ApiTab) => {
    onSelect(next);
    // The moved-to button is focused after the panel re-renders, so focus lands
    // on the tab that is now selected instead of the one that lost selection.
    window.requestAnimationFrame(() => buttons.current.get(next)?.focus({ preventScroll: true }));
  };

  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    const next = nextApiTab(tab, event.key);
    if (next === null) return;
    event.preventDefault();
    move(next);
  };

  return (
    <div className="page-tabs" role="tablist" aria-label={t("api.tabsLabel")}>
      {API_TABS.map(id => (
        <ApiTabButton
          key={id}
          id={id}
          label={t(TAB_LABEL[id])}
          selected={id === tab}
          register={node => {
            if (node) buttons.current.set(id, node);
            else buttons.current.delete(id);
          }}
          onSelect={() => move(id)}
          onKeyDown={onKeyDown}
        />
      ))}
    </div>
  );
}

/** One tab control. `tabIndex` is roving: only the selected panel is reachable. */
function ApiTabButton({
  id,
  label,
  selected,
  register,
  onSelect,
  onKeyDown,
}: {
  id: ApiTab;
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
      id={apiTabDomId(id)}
      aria-selected={selected}
      aria-controls={apiPanelDomId(id)}
      tabIndex={selected ? 0 : -1}
      className={selected ? "page-tab page-tab--active" : "page-tab"}
      onClick={onSelect}
      onKeyDown={onKeyDown}
    >
      {label}
    </button>
  );
}
