/** Benes dashboard client for the Go proxy (`internal/server`). */
import { IconSearch } from "../icons";
import type { TFn } from "../i18n/shared";

export function SessionsSearch({
  t,
  value,
  onChange,
}: {
  t: TFn;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="sessions-search">
      <IconSearch aria-hidden="true" />
      <span className="sr-only">{t("sessions.searchAria")}</span>
      <input
        type="search"
        className="input"
        value={value}
        onChange={event => onChange(event.target.value)}
        placeholder={t("sessions.search")}
        autoComplete="off"
      />
    </label>
  );
}
