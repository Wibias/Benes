import { useEffect, useRef } from "react";
import { useT, type TFn } from "../../i18n/shared";
import { IconFilter, IconSearch, IconStar, IconTrash } from "../../icons";
import { binProviderStatus, isFreeProvider, isLocalProvider, providerKind, type ProviderSortMode, type WorkspaceItem, type WorkspaceSections } from "../../provider-workspace/catalog";
import { formatProviderDisplayName } from "../../provider-icons";
import { ProviderMark } from "../ProviderMark";
import type { PricingFilter, StatusFilter, TypeFilter } from "../../provider-workspace/workspace-shell";
import { nextRailTabbableName, railRowSelected, workspaceRailGroups } from "../../provider-workspace/workspace-shell";

const SORT_DEFS: { id: ProviderSortMode; labelKey: "pws.sort.az" | "pws.sort.za" | "pws.sort.freePaid" | "pws.sort.paidFree" | "pws.sort.accountsFirst" }[] = [
  { id: "az", labelKey: "pws.sort.az" },
  { id: "za", labelKey: "pws.sort.za" },
  { id: "free-paid", labelKey: "pws.sort.freePaid" },
  { id: "paid-free", labelKey: "pws.sort.paidFree" },
  { id: "accounts-first", labelKey: "pws.sort.accountsFirst" },
];

export function ProviderWorkspaceRail({
  search,
  onSearch,
  filterOpen,
  onFilterOpen,
  filterActive,
  statusFilter,
  onStatusFilter,
  pricingFilter,
  onPricingFilter,
  typeFilter,
  onTypeFilter,
  sortMode,
  onSortMode,
  onResetFilters,
  sections,
  filteredSections,
  filteredItems,
  selectedName,
  onSelect,
  onRemoveProvider,
  defaultProvider,
  modelCountFor,
  duplicateDisplayNames,
  railFocusName,
  onRailFocus,
}: {
  search: string;
  onSearch: (value: string) => void;
  filterOpen: boolean;
  onFilterOpen: (open: boolean | ((open: boolean) => boolean)) => void;
  filterActive: boolean;
  statusFilter: StatusFilter;
  onStatusFilter: (next: StatusFilter | ((previous: StatusFilter) => StatusFilter)) => void;
  pricingFilter: PricingFilter;
  onPricingFilter: (next: PricingFilter | ((previous: PricingFilter) => PricingFilter)) => void;
  typeFilter: TypeFilter;
  onTypeFilter: (next: TypeFilter | ((previous: TypeFilter) => TypeFilter)) => void;
  sortMode: ProviderSortMode;
  onSortMode: (mode: ProviderSortMode) => void;
  onResetFilters: () => void;
  sections: WorkspaceSections;
  filteredSections: WorkspaceSections;
  filteredItems: WorkspaceItem[];
  selectedName: string | null;
  onSelect: (name: string | null) => void;
  onRemoveProvider?: (name: string) => void;
  defaultProvider: string;
  modelCountFor: (name: string) => number | undefined;
  duplicateDisplayNames: ReadonlySet<string>;
  railFocusName: string | null;
  onRailFocus: (name: string) => void;
}) {
  const t = useT();
  const filterWrapRef = useRef<HTMLDivElement>(null);
  const allItems = [...sections.ready, ...sections.needsSetup, ...sections.disabled];
  const free = allItems.filter(isFreeProvider).length;
  const paid = allItems.length - free;
  const typeCounts = { cloud: 0, local: 0, selfHosted: 0, login: 0 };
  for (const item of allItems) typeCounts[providerKind(item)] += 1;

  const statusFilterOptions = [
    { key: "ready" as const, label: t("pws.status.ready"), count: sections.ready.length },
    { key: "needsSetup" as const, label: t("pws.status.needsSetup"), count: sections.needsSetup.length },
    { key: "disabled" as const, label: t("prov.disabledBadge"), count: sections.disabled.length },
  ];
  const railGroups = workspaceRailGroups(filteredItems).map(group => ({
    ...group,
    label: group.id === "connected" ? t("prov.group.healthy") : group.id === "attention" ? t("prov.group.attention") : t("prov.group.disabled"),
  }));
  const visibleRailNames = railGroups.flatMap(group => group.items.map(item => item.name));
  const railTabbableName = nextRailTabbableName(visibleRailNames, railFocusName, selectedName);

  useEffect(() => {
    if (!filterOpen) return;
    const onDoc = (event: MouseEvent) => {
      if (filterWrapRef.current && !filterWrapRef.current.contains(event.target as Node)) onFilterOpen(false);
    };
    const onKey = (event: KeyboardEvent) => { if (event.key === "Escape") onFilterOpen(false); };
    document.addEventListener("mousedown", onDoc);
    window.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      window.removeEventListener("keydown", onKey);
    };
  }, [filterOpen, onFilterOpen]);

  return (
    <aside className="pws-rail" aria-label={t("pws.providerList")}>
      <div className="pws-search-row">
        <div className="pws-search-wrap">
          <IconSearch className="pws-search-icon" width={14} height={14} aria-hidden="true" />
          <input type="search" className="input pws-search-input" placeholder={t("pws.searchPlaceholder")} value={search} onChange={event => onSearch(event.target.value)} aria-label={t("pws.searchPlaceholder")} />
        </div>
        <div className="pws-filter-wrap" ref={filterWrapRef}>
          <button type="button" className="pws-filter-btn" onClick={() => onFilterOpen(open => !open)} aria-label={t("pws.filterAria")} aria-expanded={filterOpen} aria-controls="pws-provider-filters">
            <IconFilter width={18} height={18} aria-hidden="true" />
            {filterActive && <span className="pws-filter-dot" aria-hidden="true" />}
          </button>
          {filterOpen && (
            <ProviderFilterMenu
              statusFilterOptions={statusFilterOptions}
              statusFilter={statusFilter}
              onStatusFilter={onStatusFilter}
              pricingFilter={pricingFilter}
              onPricingFilter={onPricingFilter}
              typeFilter={typeFilter}
              onTypeFilter={onTypeFilter}
              sortMode={sortMode}
              onSortMode={onSortMode}
              freeCount={free}
              paidCount={paid}
              typeCounts={typeCounts}
              filterActive={filterActive}
              onResetFilters={onResetFilters}
            />
          )}
        </div>
      </div>
      <div className="pws-rail-list" role="listbox" aria-label={t("pws.providersAria")} onKeyDown={event => {
        const options = Array.from(event.currentTarget.querySelectorAll<HTMLElement>('[role="option"]'));
        if (options.length === 0) return;
        const active = document.activeElement as HTMLElement | null;
        const index = options.findIndex(element => element === active || element.contains(active));
        if (event.key === "ArrowDown" || event.key === "ArrowUp") {
          event.preventDefault();
          const delta = event.key === "ArrowDown" ? 1 : -1;
          const next = index < 0 ? (delta > 0 ? 0 : options.length - 1) : (index + delta + options.length) % options.length;
          options[next]?.focus();
          return;
        }
        if (event.key === "Home") { event.preventDefault(); options[0]?.focus(); return; }
        if (event.key === "End") { event.preventDefault(); options[options.length - 1]?.focus(); }
      }}>
        {Object.values(filteredSections).every(items => items.length === 0) && <span className="muted pws-rail-empty" role="status">{search ? t("pws.noSearchResults") : filterActive ? t("pws.noMatchFilters") : t("pws.noProvidersConfigured")}</span>}
        {railGroups.map(group => group.items.length === 0 ? null : (
          <div key={group.id} className="pws-rail-group" role="group" aria-label={`${group.label} (${group.items.length})`}>
            <div className="pws-rail-group-head" aria-hidden="true"><span className="pws-rail-group-label">{group.label} ({group.items.length})</span></div>
            {group.items.map(item => (
              <div key={item.name} className="providers-workspace-rail-row-wrap" data-selected={item.name === selectedName ? "true" : "false"}>
                <WorkspaceRailOption
                  item={item}
                  selected={railRowSelected(selectedName, item.name)}
                  tabbable={railTabbableName === item.name}
                  modelCount={modelCountFor(item.name)}
                  isDefault={defaultProvider === item.name}
                  showConfigId={duplicateDisplayNames.has(formatProviderDisplayName(item.name, t))}
                  onClick={() => onSelect(railRowSelected(selectedName, item.name) ? null : item.name)}
                  onFocus={() => onRailFocus(item.name)}
                />
                {onRemoveProvider && <button type="button" className="providers-workspace-rail-row-remove" onClick={event => { event.stopPropagation(); onRemoveProvider(item.name); }} title={t("pws.removeConfirmTitle")} aria-label={t("pws.removeConfirmTitle")}><IconTrash width={14} height={14} aria-hidden="true" /></button>}
              </div>
            ))}
          </div>
        ))}
      </div>
    </aside>
  );
}

function railStatusCopy(item: WorkspaceItem, t: TFn): string {
  const status = binProviderStatus(item);
  if (status === "disabled") return t("prov.disabledBadge");
  if (status === "ready") return t("pws.status.ready");
  if (item.activeNeedsReauth) return t("pws.status.needsAttention");
  return t("pws.status.needsSetup");
}

/**
 * The rail dot and the detail pill are the same three states in the same three
 * colours, so the bin maps onto the shared modifier names rather than a
 * rail-local vocabulary.
 */
function railStatusClass(item: WorkspaceItem): string {
  const status = binProviderStatus(item);
  if (status === "disabled") return "providers-workspace-rail-status providers-workspace-rail-status--inactive";
  if (status === "ready") return "providers-workspace-rail-status providers-workspace-rail-status--active";
  return "providers-workspace-rail-status providers-workspace-rail-status--warning";
}

function WorkspaceRailOption({
  item,
  selected,
  tabbable,
  modelCount,
  isDefault,
  showConfigId,
  onClick,
  onFocus,
}: {
  item: WorkspaceItem;
  selected: boolean;
  tabbable: boolean;
  modelCount?: number;
  isDefault?: boolean;
  showConfigId?: boolean;
  onClick: () => void;
  onFocus: () => void;
}) {
  const t = useT();
  const free = isFreeProvider(item);
  const local = isLocalProvider(item);
  const status = railStatusCopy(item, t);
  const displayName = formatProviderDisplayName(item.name, t);
  const nameTitle = showConfigId ? `${displayName} (${item.name})` : displayName;
  const suffix = `${isDefault ? t("pws.rail.suffixDefault") : ""}${local ? t("pws.rail.suffixLocal") : free ? t("pws.rail.suffixFree") : ""}`;
  const countLabel = modelCount !== undefined && modelCount > 0
    ? (modelCount === 1 ? t("pws.modelCountOne") : t("pws.modelCount", { count: modelCount }))
    : "";
  const secondaryLabel = [showConfigId ? item.name : "", countLabel].filter(Boolean).join(" · ");
  return (
    <button
      type="button"
      className="providers-workspace-rail-row"
      data-status={binProviderStatus(item)}
      onClick={onClick}
      role="option"
      aria-selected={selected}
      tabIndex={tabbable ? 0 : -1}
      aria-label={t("pws.rail.selectAria", { name: nameTitle, status, suffix })}
      title={nameTitle}
      onFocus={onFocus}
    >
      <ProviderMark name={item.name} adapter={item.adapter} baseUrl={item.baseUrl} className="providers-workspace-rail-icon" aria-hidden="true" />
      <span className="providers-workspace-rail-copy">
        <span className="providers-workspace-rail-primary">
          <span className="providers-workspace-rail-name-label" title={displayName}>{displayName}</span>
          {local ? (
            <span className="pwi-rail-badge pwi-rail-badge--local" title={t("pws.localTitle")}>{t("modal.badge.local")}</span>
          ) : free ? (
            <span className="pwi-rail-badge pwi-rail-badge--free" title={t("pws.freeTitle")}>{t("modal.badge.free")}</span>
          ) : null}
        </span>
        {secondaryLabel ? (
          <span className="providers-workspace-rail-secondary" title={secondaryLabel}>{secondaryLabel}</span>
        ) : null}
      </span>
      <span className="providers-workspace-rail-trail">
        {isDefault && (
          <span className="pwi-default-star" title={t("prov.defaultBadge")} aria-label={t("prov.defaultBadge")}>
            <IconStar width={17} height={17} aria-hidden="true" />
          </span>
        )}
        <span className={railStatusClass(item)} title={status} aria-hidden="true" />
      </span>
    </button>
  );
}

function ProviderFilterMenu({
  statusFilterOptions,
  statusFilter,
  onStatusFilter,
  pricingFilter,
  onPricingFilter,
  typeFilter,
  onTypeFilter,
  sortMode,
  onSortMode,
  freeCount,
  paidCount,
  typeCounts,
  filterActive,
  onResetFilters,
}: {
  statusFilterOptions: { key: keyof StatusFilter; label: string; count: number }[];
  statusFilter: StatusFilter;
  onStatusFilter: (next: StatusFilter | ((previous: StatusFilter) => StatusFilter)) => void;
  pricingFilter: PricingFilter;
  onPricingFilter: (next: PricingFilter | ((previous: PricingFilter) => PricingFilter)) => void;
  typeFilter: TypeFilter;
  onTypeFilter: (next: TypeFilter | ((previous: TypeFilter) => TypeFilter)) => void;
  sortMode: ProviderSortMode;
  onSortMode: (mode: ProviderSortMode) => void;
  freeCount: number;
  paidCount: number;
  typeCounts: { cloud: number; local: number; selfHosted: number; login: number };
  filterActive: boolean;
  onResetFilters: () => void;
}) {
  const t = useT();
  return (
    <div id="pws-provider-filters" className="pws-filter-menu" role="group" aria-label={t("pws.providerFiltersAria")}>
      <div className="pws-filter-title">{t("pws.filters")}</div>
      <div className="pws-filter-head">{t("pws.filterStatus")}</div>
      {statusFilterOptions.map(({ key, label, count }) => (
        <label key={key} className="pws-filter-option">
          <input type="checkbox" checked={statusFilter[key]} onChange={() => onStatusFilter(previous => ({ ...previous, [key]: !previous[key] }))} />
          <span className="pws-filter-label">{label}</span>
          <span className="pws-filter-count">{count}</span>
        </label>
      ))}
      <div className="pws-filter-head">{t("pws.pricing")}</div>
      <label className="pws-filter-option"><input type="checkbox" checked={pricingFilter.free} onChange={() => onPricingFilter(previous => ({ ...previous, free: !previous.free }))} /><span className="pws-filter-label">{t("modal.badge.free")}</span><span className="pws-filter-count">{freeCount}</span></label>
      <label className="pws-filter-option"><input type="checkbox" checked={pricingFilter.paid} onChange={() => onPricingFilter(previous => ({ ...previous, paid: !previous.paid }))} /><span className="pws-filter-label">{t("pws.paid")}</span><span className="pws-filter-count">{paidCount}</span></label>
      <div className="pws-filter-head">{t("pws.filterType")}</div>
      {([
        { key: "cloud" as const, label: t("pws.type.cloud"), count: typeCounts.cloud },
        { key: "local" as const, label: t("pws.type.local"), count: typeCounts.local },
        { key: "selfHosted" as const, label: t("pws.type.selfHosted"), count: typeCounts.selfHosted },
        { key: "login" as const, label: t("pws.type.login"), count: typeCounts.login },
      ]).map(({ key, label, count }) => (
        <label key={key} className="pws-filter-option"><input type="checkbox" checked={typeFilter[key]} onChange={() => onTypeFilter(previous => ({ ...previous, [key]: !previous[key] }))} /><span className="pws-filter-label">{label}</span><span className="pws-filter-count">{count}</span></label>
      ))}
      <div className="pws-filter-head">{t("pws.sort")}</div>
      <div className="pws-sort-grid" role="group" aria-label={t("pws.sortProvidersAria")}>
        {SORT_DEFS.map(option => <button key={option.id} type="button" className="pws-sort-btn" onClick={() => onSortMode(option.id)} aria-pressed={sortMode === option.id}>{t(option.labelKey)}</button>)}
      </div>
      <div className="pws-filter-footer"><button type="button" className="link-btn" onClick={onResetFilters} disabled={!filterActive}>{t("pws.resetAll")}</button></div>
    </div>
  );
}
