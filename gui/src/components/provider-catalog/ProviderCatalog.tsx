/**
 * ProviderCatalog — browse surface of the add-provider modal: searchable
 * provider rail with Access / Connection filters, plus account login rows
 * when Access is Accounts.
 */
import { useId, useMemo, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { useT, type TFn } from "../../i18n/shared";
import { IconPlus, IconSearch } from "../../icons";
import { Select } from "../../ui";
import { cssPx, cssTranslateY } from "../../css-transform";
import { ProviderMark } from "../ProviderMark";
import {
  accessFromTier,
  CATALOG_ACCESS_FILTER_KEYS,
  CATALOG_CONNECTION_FILTER_KEYS,
  catalogAccountStatusText,
  catalogEmptyKind,
  familyAccessLabel,
  familyConnectionLabel,
  filterAccountRows,
  isCatalogAccessFilter,
  isCatalogConnectionFilter,
  matchesAccessFilter,
  matchesConnectionFilter,
  type AccountLoginRow,
  type AccountLoginStatus,
  type CatalogAccessFilter,
  type CatalogConnectionFilter,
  type CatalogEmptyKind,
  type CatalogTier,
} from "./catalog-policy";
import {
  preferredCatalogFamilyMember,
  visibleCatalogFamilies,
  type CatalogFamily,
} from "./catalog-families";
import { FEATURED_PRESET_IDS, type CatalogPreset } from "./provider-presets";

export type { AccountLoginRow, AccountLoginStatus, CatalogTier };

const EMPTY_ACCOUNT_ROWS: AccountLoginRow[] = [];
const EMPTY_ACCOUNT_STATUS: Record<string, AccountLoginStatus> = {};
const EMPTY_EXISTING_NAMES: string[] = [];

function catalogSelectOptions<Value extends string>(
  rows: { value: Value; labelKey: Parameters<TFn>[0] }[],
  t: TFn,
) {
  return rows.map((row) => ({ value: row.value, label: t(row.labelKey) }));
}

function CatalogFilter({
  id,
  name,
  value,
  options,
  onChange,
}: {
  id: string;
  name: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
}) {
  return (
    <div className="add-provider-filter">
      <label className="add-provider-filter-name" htmlFor={id}>{name}</label>
      <Select
        id={id}
        label={name}
        value={value}
        options={options}
        onChange={onChange}
        dropdownStyle={{ background: "var(--surface)", color: "var(--text)" }}
        chevron="down"
      />
    </div>
  );
}

function CatalogToolbar({
  query,
  access,
  connection,
  onQuery,
  onAccess,
  onConnection,
}: {
  query: string;
  access: CatalogAccessFilter;
  connection: CatalogConnectionFilter;
  onQuery: (value: string) => void;
  onAccess: (value: CatalogAccessFilter) => void;
  onConnection: (value: CatalogConnectionFilter) => void;
}) {
  const t = useT();
  const accessId = useId();
  const connectionId = useId();
  return (
    <div className="add-provider-toolbar">
      <label className="add-provider-search">
        <IconSearch />
        <span className="sr-only">{t("modal.search")}</span>
        <input
          className="input"
          value={query}
          onChange={e => onQuery(e.target.value)}
          placeholder={t("modal.search")}
        />
      </label>
      <div className="add-provider-toolbar-filters">
        <CatalogFilter
          id={accessId}
          name={t("modal.filter.access")}
          value={access}
          options={catalogSelectOptions(CATALOG_ACCESS_FILTER_KEYS, t)}
          onChange={value => {
            if (isCatalogAccessFilter(value)) onAccess(value);
          }}
        />
        <CatalogFilter
          id={connectionId}
          name={t("modal.filter.connection")}
          value={connection}
          options={catalogSelectOptions(CATALOG_CONNECTION_FILTER_KEYS, t)}
          onChange={value => {
            if (isCatalogConnectionFilter(value)) onConnection(value);
          }}
        />
      </div>
    </div>
  );
}

function CatalogEmpty({ kind }: { kind: CatalogEmptyKind }) {
  const t = useT();
  if (kind === "loading") {
    return <div className="muted text-control add-provider-empty">{t("modal.catalogLoading")}</div>;
  }
  if (kind === "no-match") {
    return <div className="muted text-control add-provider-empty">{t("modal.noMatch")}</div>;
  }
  return null;
}

function CatalogTableHead() {
  const t = useT();
  return (
    <div className="add-provider-table-head">
      <span className="add-provider-table-pick" aria-hidden="true" />
      <span>{t("modal.step.provider")}</span>
      <span>{t("modal.filter.access")}</span>
      <span>{t("modal.filter.connection")}</span>
    </div>
  );
}

function CatalogAccountList({
  accounts,
  accountStatus,
  selectedAccountId,
  onSelectAccount,
  emptyKind,
}: {
  accounts: AccountLoginRow[];
  accountStatus: Record<string, AccountLoginStatus>;
  selectedAccountId?: string | null;
  onSelectAccount?: (row: AccountLoginRow) => void;
  emptyKind: CatalogEmptyKind;
}) {
  const t = useT();
  return (
    <>
      {accounts.map(row => {
        const status = accountStatus[row.id];
        const selected = selectedAccountId === row.id;
        return (
          <label
            key={row.id}
            className={`add-provider-table-row${selected ? " is-selected" : ""}`}
          >
            <input
              type="radio"
              name="add-provider-catalog"
              checked={selected}
              onChange={() => onSelectAccount?.(row)}
            />
            <span className="add-provider-table-provider">
              <ProviderMark name={row.id} className="add-provider-rail-icon" />
              <span className="add-provider-table-name">{row.label}</span>
            </span>
            <span className="muted">{t("modal.tab.accounts")}</span>
            <span className="muted">{catalogAccountStatusText(row, status, t)}</span>
          </label>
        );
      })}
      <CatalogEmpty kind={emptyKind} />
    </>
  );
}

function CatalogPresetList({
  rows,
  selectedId,
  existingNames,
  access,
  connection,
  onSelectPreset,
  emptyKind,
}: {
  rows: CatalogFamily<CatalogPreset>[];
  selectedId?: string | null;
  existingNames: string[];
  access: CatalogAccessFilter;
  connection: CatalogConnectionFilter;
  onSelectPreset: (preset: CatalogPreset) => void;
  emptyKind: CatalogEmptyKind;
}) {
  const t = useT();
  const parentRef = useRef<HTMLDivElement>(null);
  // eslint-disable-next-line react-hooks/incompatible-library -- known useVirtualizer limitation
  const virtualizer = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parentRef.current,
    estimateSize: () => 41,
    overscan: 8,
  });
  return (
    <div ref={parentRef} className="add-provider-rail-list" role="radiogroup" aria-label={t("modal.rail.providers")}>
      <div className="add-provider-rail-virtual" style={{ height: cssPx(virtualizer.getTotalSize()) }}>
        {virtualizer.getVirtualItems().map(virtualRow => {
          const family = rows[virtualRow.index]!;
          const selected = family.members.some(row => row.id === selectedId);
          const icon = family.members[0]!;
          return (
            <label
              key={family.id}
              data-index={virtualRow.index}
              ref={virtualizer.measureElement}
              className={`add-provider-table-row${selected ? " is-selected" : ""}`}
              style={{ transform: cssTranslateY(virtualRow.start) }}
            >
              <input
                type="radio"
                name="add-provider-catalog"
                checked={selected}
                onChange={() => {
                  if (selected) return;
                  const hinted = new Set(family.members.filter(row => (
                    matchesAccessFilter(row, access) && matchesConnectionFilter(row, connection)
                  )).map(row => row.id));
                  onSelectPreset(preferredCatalogFamilyMember(family, existingNames, hinted));
                }}
              />
              <span className="add-provider-table-provider">
                <ProviderMark name={icon.id} adapter={icon.adapter} baseUrl={icon.baseUrl} className="add-provider-rail-icon" />
                <span className="add-provider-table-name">{family.label}</span>
              </span>
              <span className="muted">{familyAccessLabel(family, t)}</span>
              <span className="muted">{familyConnectionLabel(family, t)}</span>
            </label>
          );
        })}
      </div>
      <CatalogEmpty kind={emptyKind} />
    </div>
  );
}

function CatalogRail({
  showAccounts,
  presetsLoading,
  rows,
  accounts,
  selectedId,
  selectedAccountId,
  accountStatus,
  existingNames,
  access,
  connection,
  onSelectPreset,
  onSelectCustom,
  onSelectAccount,
}: {
  showAccounts: boolean;
  presetsLoading: boolean;
  rows: CatalogFamily<CatalogPreset>[];
  accounts: AccountLoginRow[];
  selectedId?: string | null;
  selectedAccountId?: string | null;
  accountStatus: Record<string, AccountLoginStatus>;
  existingNames: string[];
  access: CatalogAccessFilter;
  connection: CatalogConnectionFilter;
  onSelectPreset: (preset: CatalogPreset) => void;
  onSelectCustom: () => void;
  onSelectAccount?: (row: AccountLoginRow) => void;
}) {
  const t = useT();
  const emptyKind = catalogEmptyKind({
    presetsLoading,
    showAccounts,
    accountCount: accounts.length,
    rowCount: rows.length,
  });
  return (
    <div className="add-provider-rail">
      <div className="add-provider-table">
        <CatalogTableHead />
        {showAccounts ? (
          <div className="add-provider-rail-list" role="radiogroup" aria-label={t("modal.rail.providers")}>
            <CatalogAccountList
              accounts={accounts}
              accountStatus={accountStatus}
              selectedAccountId={selectedAccountId}
              onSelectAccount={onSelectAccount}
              emptyKind={emptyKind}
            />
          </div>
        ) : (
          <CatalogPresetList
            rows={rows}
            selectedId={selectedId}
            existingNames={existingNames}
            access={access}
            connection={connection}
            onSelectPreset={onSelectPreset}
            emptyKind={emptyKind}
          />
        )}
      </div>
      {showAccounts ? null : (
        <button
          type="button"
          className={`add-provider-custom${selectedId === "custom" ? " is-selected" : ""}`}
          onClick={onSelectCustom}
        >
          <IconPlus />
          <span>{t("modal.customProvider")}</span>
        </button>
      )}
    </div>
  );
}

export default function ProviderCatalog({
  presets,
  presetsLoading = false,
  initialTier,
  selectedId,
  existingNames = EMPTY_EXISTING_NAMES,
  onSelectPreset,
  onSelectCustom,
  accountRows = EMPTY_ACCOUNT_ROWS,
  accountStatus = EMPTY_ACCOUNT_STATUS,
  selectedAccountId,
  onSelectAccount,
  onAccessChange,
}: {
  presets: CatalogPreset[];
  presetsLoading?: boolean;
  initialTier?: CatalogTier;
  selectedId?: string | null;
  existingNames?: string[];
  onSelectPreset: (preset: CatalogPreset) => void;
  onSelectCustom: () => void;
  accountRows?: AccountLoginRow[];
  accountStatus?: Record<string, AccountLoginStatus>;
  selectedAccountId?: string | null;
  onSelectAccount?: (row: AccountLoginRow) => void;
  onAccessChange?: (access: CatalogAccessFilter) => void;
}) {
  const [access, setAccess] = useState<CatalogAccessFilter>(() => accessFromTier(initialTier));
  const [connection, setConnection] = useState<CatalogConnectionFilter>("all");
  const [query, setQuery] = useState("");

  const rows = useMemo(
    () => visibleCatalogFamilies(
      presets,
      query,
      FEATURED_PRESET_IDS,
      row => matchesAccessFilter(row, access) && matchesConnectionFilter(row, connection),
    ),
    [presets, query, access, connection],
  );
  const accounts = useMemo(() => filterAccountRows(accountRows, query), [accountRows, query]);
  const showAccounts = access === "accounts";

  return (
    <div className="add-provider-browse">
      <CatalogToolbar
        query={query}
        access={access}
        connection={connection}
        onQuery={setQuery}
        onAccess={next => {
          setAccess(next);
          onAccessChange?.(next);
        }}
        onConnection={setConnection}
      />
      <CatalogRail
        showAccounts={showAccounts}
        presetsLoading={presetsLoading}
        rows={rows}
        accounts={accounts}
        selectedId={selectedId}
        selectedAccountId={selectedAccountId}
        accountStatus={accountStatus}
        existingNames={existingNames}
        access={access}
        connection={connection}
        onSelectPreset={onSelectPreset}
        onSelectCustom={onSelectCustom}
        onSelectAccount={onSelectAccount}
      />
    </div>
  );
}
