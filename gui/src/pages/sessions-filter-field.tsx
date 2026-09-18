/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { TFn } from "../i18n/shared";

export function SessionFilterField({
  t,
  label,
  value,
  options,
  optionLabel,
  onChange,
}: {
  t: TFn;
  label: string;
  value: string;
  options: string[];
  optionLabel?: (value: string) => string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="sessions-filter-field">
      {label}
      <select className="input" value={value} onChange={event => onChange(event.target.value)}>
        <option value="">{t("sessions.filter.all")}</option>
        {options.map(option => (
          <option key={option} value={option}>{optionLabel ? optionLabel(option) : option}</option>
        ))}
      </select>
    </label>
  );
}
