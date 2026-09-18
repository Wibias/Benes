/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useEffect, useRef } from "react";
import { IconFilter } from "../icons";
import type { TFn } from "../i18n/shared";
import { SessionFilterField } from "./sessions-filter-field";
import { protocolOptionLabel } from "./sessions-protocol-label";
import { type SessionFilterValues, type SessionListQuery } from "./sessions-contract";

export function SessionsFilters({
  t,
  open,
  values,
  query,
  onToggle,
  onChange,
  onClear,
}: {
  t: TFn;
  open: boolean;
  values: SessionFilterValues;
  query: SessionListQuery;
  onToggle: () => void;
  onChange: (patch: Partial<SessionListQuery>) => void;
  onClear: () => void;
}) {
  const rootRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!open) return;
    const onPointer = (event: MouseEvent) => {
      if (!rootRef.current?.contains(event.target as Node)) onToggle();
    };
    document.addEventListener("mousedown", onPointer);
    return () => document.removeEventListener("mousedown", onPointer);
  }, [open, onToggle]);

  return (
    <div className="sessions-filter" ref={rootRef}>
      <button
        type="button"
        className={`sessions-filter-btn${open ? " is-open" : ""}`}
        aria-expanded={open}
        aria-haspopup="dialog"
        aria-label={t("sessions.filtersAria")}
        onClick={onToggle}
      >
        <IconFilter aria-hidden="true" />
        {t("sessions.filters")}
      </button>
      {open && (
        <div className="sessions-filter-pop" role="dialog" aria-label={t("sessions.filtersAria")}>
          <SessionFilterField
            label={t("sessions.filter.namespace")}
            value={query.namespace ?? ""}
            options={values.namespaces}
            onChange={namespace => onChange({ namespace: namespace || undefined })}
            t={t}
          />
          <SessionFilterField
            label={t("sessions.filter.protocol")}
            value={query.protocol ?? ""}
            options={values.protocols}
            optionLabel={id => protocolOptionLabel(t, id)}
            onChange={protocol => onChange({ protocol: protocol || undefined })}
            t={t}
          />
          <SessionFilterField
            label={t("sessions.filter.provider")}
            value={query.provider ?? ""}
            options={values.providers}
            onChange={provider => onChange({ provider: provider || undefined })}
            t={t}
          />
          <SessionFilterField
            label={t("sessions.filter.model")}
            value={query.model ?? ""}
            options={values.models}
            onChange={model => onChange({ model: model || undefined })}
            t={t}
          />
          <SessionFilterField
            label={t("sessions.filter.policy")}
            value={query.policy ?? ""}
            options={values.policyIds}
            onChange={policy => onChange({ policy: policy || undefined })}
            t={t}
          />
          <SessionFilterField
            label={t("sessions.filter.combo")}
            value={query.combo ?? ""}
            options={values.comboIds}
            onChange={combo => onChange({ combo: combo || undefined })}
            t={t}
          />
          <button type="button" className="sessions-filter-clear" onClick={onClear}>
            {t("sessions.filter.clear")}
          </button>
        </div>
      )}
    </div>
  );
}
