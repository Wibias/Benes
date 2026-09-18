import { Select } from "../ui";
import type { TFn } from "../i18n/shared";
import type { UsageRange, UsageSurface } from "./usage-range";

export function UsageFilters({
  surface,
  range,
  customStart,
  customEnd,
  onSurface,
  onRange,
  onCustomStart,
  onCustomEnd,
  t,
}: {
  surface: UsageSurface;
  range: UsageRange;
  customStart: string;
  customEnd: string;
  onSurface: (surface: UsageSurface) => void;
  onRange: (range: UsageRange) => void;
  onCustomStart: (value: string) => void;
  onCustomEnd: (value: string) => void;
  t: TFn;
}) {
  return (
    <div className="usage-filters">
      <label className="usage-filter-field">
        <span>{t("usage.filter.surface")}</span>
        <Select
          value={surface}
          chevron="down"
          label={t("usage.filter.surface")}
          onChange={value => onSurface(value as UsageSurface)}
          options={[
            { value: "all", label: t("logs.filter.surface.all") },
            { value: "codex", label: t("logs.filter.surface.codex") },
            { value: "claude", label: t("logs.filter.surface.claude") },
            { value: "grok", label: t("logs.filter.surface.grok") },
          ]}
        />
      </label>
      <label className="usage-filter-field">
        <span>{t("usage.filter.range")}</span>
        <Select
          value={range}
          chevron="down"
          label={t("usage.filter.range")}
          onChange={value => onRange(value as UsageRange)}
          options={[
            { value: "today", label: t("usage.range.today") },
            { value: "yesterday", label: t("usage.range.yesterday") },
            { value: "7d", label: t("usage.range.7d") },
            { value: "30d", label: t("usage.range.30d") },
            { value: "all", label: t("usage.range.available") },
            { value: "custom", label: t("usage.range.custom") },
          ]}
        />
      </label>
      {range === "custom" && (
        <div className="usage-range-dates">
          <label className="usage-range-date">
            <span>{t("usage.range.start")}</span>
            <input
              type="datetime-local"
              className="input"
              value={customStart}
              aria-label={t("usage.range.start")}
              onChange={event => onCustomStart(event.target.value)}
            />
          </label>
          <label className="usage-range-date">
            <span>{t("usage.range.endInclusive")}</span>
            <input
              type="datetime-local"
              className="input"
              value={customEnd}
              aria-label={t("usage.range.endInclusive")}
              onChange={event => onCustomEnd(event.target.value)}
            />
          </label>
        </div>
      )}
    </div>
  );
}
