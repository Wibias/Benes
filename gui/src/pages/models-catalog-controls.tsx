/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useEffect, useRef } from "react";
import { IconRefresh, IconSearch } from "../icons";
import type { TFn } from "../i18n/shared";
import { Select } from "../ui";
import {
  catalogSearchShortcutKey,
  type CatalogVisibilityFilter,
} from "./models-catalog-filter";
import { formatProviderDisplayName } from "../provider-icons";

export function ModelsCollapseControls({
  t,
  busy,
  onCollapseAll,
  onExpandAll,
}: {
  t: TFn;
  busy: boolean;
  onCollapseAll: () => void;
  onExpandAll: () => void;
}) {
  return (
    <span className="models-collapse-controls">
      <button type="button" className="models-catalog-text-link" onClick={onCollapseAll} disabled={busy}>
        {t("models.collapseAll")}
      </button>
      <span className="models-catalog-bulk-sep" aria-hidden="true">|</span>
      <button type="button" className="models-catalog-text-link" onClick={onExpandAll} disabled={busy}>
        {t("models.expandAll")}
      </button>
    </span>
  );
}

export function ModelsCatalogToolbar({
  t,
  busy,
  refreshing,
  query,
  provider,
  visibility,
  providers,
  listenForShortcut,
  onQueryChange,
  onProviderChange,
  onVisibilityChange,
  onRefresh,
}: {
  t: TFn;
  busy: boolean;
  refreshing: boolean;
  query: string;
  provider: string | null;
  visibility: CatalogVisibilityFilter;
  providers: string[];
  listenForShortcut: boolean;
  onQueryChange: (value: string) => void;
  onProviderChange: (provider: string | null) => void;
  onVisibilityChange: (value: CatalogVisibilityFilter) => void;
  onRefresh: () => void;
}) {
  const searchRef = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (!listenForShortcut) return;
    const onKey = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        searchRef.current?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [listenForShortcut]);

  return (
    <div className="models-catalog-toolbar">
      <label className="models-catalog-search">
        <IconSearch aria-hidden="true" />
        <span className="sr-only">{t("models.search")}</span>
        <input
          ref={searchRef}
          type="search"
          value={query}
          onChange={event => onQueryChange(event.target.value)}
          placeholder={t("models.search")}
        />
        <kbd>{t(catalogSearchShortcutKey())}</kbd>
      </label>
      <Select
        value={provider ?? ""}
        options={[
          { value: "", label: t("models.workspace.allProviders") },
          ...providers.map(name => ({ value: name, label: formatProviderDisplayName(name, t) })),
        ]}
        onChange={value => onProviderChange(value === "" ? null : value)}
        disabled={busy}
        label={t("models.workspace.allProviders")}
      />
      <Select
        value={visibility}
        options={[
          { value: "all", label: t("models.filterVisibility") },
          { value: "visible", label: t("models.filterVisible") },
          { value: "hidden", label: t("models.filterHidden") },
        ]}
        onChange={value => onVisibilityChange(value === "visible" || value === "hidden" ? value : "all")}
        disabled={busy}
        label={t("models.filterVisibility")}
      />
      <div className="models-catalog-toolbar-end">
        <button
          type="button"
          className="models-catalog-refresh"
          onClick={onRefresh}
          disabled={busy || refreshing}
          aria-label={t("models.catalogRefresh")}
          title={t("models.catalogRefresh")}
        >
          <IconRefresh />
        </button>
      </div>
    </div>
  );
}
