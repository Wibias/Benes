import { IconRefresh } from "../icons";
import { navigateHash } from "../hash-routing";
import { Select } from "../ui";
import type { TFn } from "../i18n/shared";
import { protocolOptionLabel } from "./sessions-protocol-label";
import {
  DIAGNOSTICS_PROTOCOLS,
  DIAGNOSTICS_TIME_RANGES,
  type DiagnosticsTimeRange,
  type DiagnosticsToolbarFilters,
  toolbarFiltersActive,
  uniqueSorted,
} from "./diagnostics-contract";

function timeRangeLabel(t: TFn, value: DiagnosticsTimeRange): string {
  if (value === "15m") return t("logs.filter.time.15m");
  if (value === "1h") return t("logs.filter.time.1h");
  if (value === "24h") return t("logs.filter.time.24h");
  return t("logs.filter.time.all");
}

function FilterSelect({
  label,
  value,
  options,
  onChange,
}: {
  label: string;
  value: string;
  options: Array<{ value: string; label: string }>;
  onChange: (value: string) => void;
}) {
  return (
    <div className="diagnostics-filter-field">
      <span>{label}</span>
      <Select
        value={value}
        options={options}
        onChange={onChange}
        label={label}
        chevron="down"
      />
    </div>
  );
}

export function DiagnosticsFilters({
  t,
  filters,
  sessionId,
  options,
  autoRefresh,
  refreshing,
  onChange,
  onClear,
  onAutoRefresh,
  onRefresh,
}: {
  t: TFn;
  filters: DiagnosticsToolbarFilters;
  sessionId: string;
  options: { statuses: string[]; protocols: string[]; providers: string[]; models: string[] };
  autoRefresh: boolean;
  refreshing: boolean;
  onChange: (patch: Partial<DiagnosticsToolbarFilters>) => void;
  onClear: () => void;
  onAutoRefresh: (value: boolean) => void;
  onRefresh: () => void;
}) {
  const protocols = uniqueSorted([...DIAGNOSTICS_PROTOCOLS, ...options.protocols, filters.protocol]);
  const statuses = uniqueSorted([...options.statuses, filters.status]);
  const providers = uniqueSorted([...options.providers, filters.provider]);
  const models = uniqueSorted([...options.models, filters.model]);
  const filtersOn = toolbarFiltersActive(filters);
  return (
    <div className="diagnostics-toolbar">
      <FilterSelect
        label={t("logs.filter.time")}
        value={filters.timeRange}
        options={DIAGNOSTICS_TIME_RANGES.map(value => ({ value, label: timeRangeLabel(t, value) }))}
        onChange={value => onChange({ timeRange: value as DiagnosticsTimeRange })}
      />
      <FilterSelect
        label={t("logs.col.status")}
        value={filters.status}
        options={[{ value: "", label: t("logs.filter.all") }, ...statuses.map(value => ({ value, label: value }))]}
        onChange={status => onChange({ status })}
      />
      <FilterSelect
        label={t("logs.filter.protocol")}
        value={filters.protocol}
        options={[{ value: "", label: t("logs.filter.all") }, ...protocols.map(value => ({ value, label: protocolOptionLabel(t, value) }))]}
        onChange={protocol => onChange({ protocol })}
      />
      <FilterSelect
        label={t("logs.col.provider")}
        value={filters.provider}
        options={[{ value: "", label: t("logs.filter.all") }, ...providers.map(value => ({ value, label: value }))]}
        onChange={provider => onChange({ provider })}
      />
      <FilterSelect
        label={t("logs.col.model")}
        value={filters.model}
        options={[{ value: "", label: t("logs.filter.all") }, ...models.map(value => ({ value, label: value }))]}
        onChange={model => onChange({ model })}
      />
      {sessionId && (
        <div className="diagnostics-session-filter">
          <span className="diagnostics-filter-field">
            {t("logs.filter.session")}
            <span className="diagnostics-session-id mono" title={sessionId}>{sessionId}</span>
          </span>
          <button type="button" className="diagnostics-filter-clear" onClick={() => navigateHash("logs")}>
            {t("logs.filter.session.clear")}
          </button>
        </div>
      )}
      {filtersOn && (
        <button type="button" className="diagnostics-filter-clear" onClick={onClear}>
          {t("logs.filter.clear")}
        </button>
      )}
      <div className="diagnostics-toolbar-end">
        <label className="muted text-control diagnostics-auto-refresh">
          <input type="checkbox" checked={autoRefresh} onChange={event => onAutoRefresh(event.target.checked)} />
          {t("logs.autoRefresh")}
        </label>
        <button type="button" className="btn btn-ghost btn-sm" onClick={onRefresh} disabled={refreshing}>
          <IconRefresh /> {t("debug.refresh")}
        </button>
      </div>
    </div>
  );
}
