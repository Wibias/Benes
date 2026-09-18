import type { KeyboardEvent } from "react";
import { useT, type TKey } from "../i18n/shared";
import {
  STORAGE_TABS,
  storagePanelDomId,
  storageTabDomId,
  type StorageTab,
} from "./storage-tab";

/** The copy each tab shows; the strip renders the tabs in the order the page owns. */
const TAB_LABEL: Record<StorageTab, TKey> = {
  overview: "storage.tab.overview",
  cleanup: "storage.tab.cleanup",
  quarantine: "storage.tab.quarantine",
};

/**
 * Where a claimed roving-focus key lands in a wrapping tablist.
 *
 * The list wraps end to end, so the two edge keys and the two step keys describe the same two
 * moves; a key this function does not name keeps its browser meaning.
 */
function nextTabIndex(key: string, current: number, count: number): number | null {
  if (key === "ArrowLeft") return (current - 1 + count) % count;
  if (key === "ArrowRight") return (current + 1) % count;
  if (key === "Home") return 0;
  if (key === "End") return count - 1;
  return null;
}

export function StorageTabStrip({
  tab,
  onSelect,
}: {
  tab: StorageTab;
  onSelect: (next: StorageTab) => void;
}) {
  const t = useT();

  // The tabs stamp their own ids, so a move can hand focus to the tab it selects directly.
  const move = (next: StorageTab) => {
    onSelect(next);
    document.getElementById(storageTabDomId(next))?.focus({ preventScroll: true });
  };

  const onKeyDown = (event: KeyboardEvent<HTMLButtonElement>) => {
    const index = nextTabIndex(event.key, STORAGE_TABS.indexOf(tab), STORAGE_TABS.length);
    if (index === null) return;
    event.preventDefault();
    move(STORAGE_TABS[index]!);
  };

  const tabs = STORAGE_TABS.map(candidate => ({
    candidate,
    selected: candidate === tab,
    domId: storageTabDomId(candidate),
    panelId: storagePanelDomId(candidate),
    label: t(TAB_LABEL[candidate]),
  }));

  return (
    <div className="page-tabs storage-board-tablist" role="tablist" aria-label={t("storage.tabsLabel")}>
      {tabs.map(item => (
        <button
          key={item.candidate}
          type="button"
          role="tab"
          id={item.domId}
          aria-selected={item.selected}
          aria-controls={item.panelId}
          tabIndex={item.selected ? 0 : -1}
          className={item.selected ? "page-tab page-tab--active" : "page-tab"}
          onClick={() => move(item.candidate)}
          onKeyDown={onKeyDown}
        >
          {item.label}
        </button>
      ))}
    </div>
  );
}