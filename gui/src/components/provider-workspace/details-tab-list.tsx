import { useRef } from "react";
import { nextDetailTabIndex } from "../../provider-workspace/connection-test";
import type { DetailTab } from "../../provider-workspace/detail-tabs";
import { detailTabDomIds, type DetailTabEntry } from "../../provider-workspace/details-view";

/**
 * Head tablist for the provider detail pane.
 *
 * Arrow/Home/End movement keeps focus and selection together: the buttons are
 * held by index so the moved-to tab can take focus directly, without a DOM query
 * for a sibling button.
 */
export function DetailTabList({ tabs, tab, onSwitch }: {
  tabs: DetailTabEntry[];
  tab: DetailTab;
  onSwitch: (next: DetailTab) => void;
}) {
  const buttons = useRef<Array<HTMLButtonElement | null>>([]);

  const selectTab = (index: number) => {
    const entry = tabs[index];
    if (!entry) return;
    onSwitch(entry.id);
    buttons.current[index]?.focus();
  };

  return (
    <div className="pws-detail-tabs" role="tablist">
      {tabs.map((entry, index) => {
        const ids = detailTabDomIds(entry.id);
        const active = entry.id === tab;
        return (
          <button
            key={entry.id}
            ref={node => { buttons.current[index] = node; }}
            type="button"
            role="tab"
            id={ids.tabId}
            aria-controls={ids.panelId}
            aria-selected={active}
            tabIndex={active ? 0 : -1}
            className="pws-detail-tab"
            onClick={() => onSwitch(entry.id)}
            onKeyDown={event => {
              const next = nextDetailTabIndex(event.key, index, tabs.length);
              if (next === null) return;
              event.preventDefault();
              selectTab(next);
            }}
          >
            {entry.label}
          </button>
        );
      })}
    </div>
  );
}
