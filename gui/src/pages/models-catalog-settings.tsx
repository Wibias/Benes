/** Flat Models catalog settings under the toolbar (no card chrome). */
import type { ReactNode } from "react";
import { Switch, Select } from "../ui";
import type { TFn } from "../i18n/shared";
import { CAP_OPTIONS, CAP_OPTION_SET, CUSTOM_OPTION, fmtK } from "./models-shared";

function SettingsField({ label, children }: { label: string; children: ReactNode }) {
  return (
    <label className="models-catalog-settings-field">
      <span className="models-catalog-settings-label">{label}</span>
      {children}
    </label>
  );
}

function contextCapOptions(value: number, custom: boolean, customLabel: string) {
  const options = CAP_OPTIONS.map(cap => ({ value: String(cap), label: fmtK(cap) }));
  if (!custom && !CAP_OPTION_SET.has(value)) {
    options.unshift({ value: String(value), label: fmtK(value) });
  }
  options.push({ value: CUSTOM_OPTION, label: customLabel });
  return options;
}

function ContextCapEditor({
  t,
  busy,
  contextCapValue,
  showCustom,
  customCap,
  onSelectCap,
  onCustomCapChange,
  onApplyCustomCap,
}: {
  t: TFn;
  busy: boolean;
  contextCapValue: number;
  showCustom: boolean;
  customCap: string;
  onSelectCap: (raw: string) => void;
  onCustomCapChange: (value: string) => void;
  onApplyCustomCap: () => void;
}) {
  const label = t("models.settings.defaultContextCap");
  return (
    <div className="models-catalog-settings-field">
      <span className="models-catalog-settings-label">{label}</span>
      <div className="models-catalog-settings-cap">
        <Select
          value={showCustom ? CUSTOM_OPTION : String(contextCapValue)}
          options={contextCapOptions(contextCapValue, showCustom, t("models.custom"))}
          onChange={onSelectCap}
          disabled={busy}
          label={label}
        />
        {showCustom ? (
          <>
            <input
              className="input"
              inputMode="numeric"
              placeholder={t("models.customPlaceholder")}
              value={customCap}
              onChange={event => onCustomCapChange(event.target.value)}
              onKeyDown={event => {
                if (event.key === "Enter") onApplyCustomCap();
              }}
              disabled={busy}
              aria-label={t("models.customPlaceholder")}
            />
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              onClick={onApplyCustomCap}
              disabled={busy}
            >
              {t("models.customApply")}
            </button>
          </>
        ) : null}
      </div>
    </div>
  );
}

function ApplyExistingControl({
  t,
  busy,
  allCapped,
  onSetAll,
}: {
  t: TFn;
  busy: boolean;
  allCapped: boolean;
  onSetAll: () => void;
}) {
  const label = t("models.settings.applyExisting");
  return (
    <div className="models-catalog-settings-apply">
      <div className="models-catalog-settings-apply-copy">
        <span className="models-catalog-settings-label">{label}</span>
        <span className="models-catalog-settings-hint">{t("models.settings.applyExistingHint")}</span>
      </div>
      <Switch on={allCapped} onClick={onSetAll} disabled={busy} label={label} />
    </div>
  );
}

export function ModelsCatalogSettings({
  t,
  busy,
  newModelPolicy,
  contextCapValue,
  showCustom,
  customCap,
  allCapped,
  onNewModelPolicy,
  onSelectCap,
  onCustomCapChange,
  onApplyCustomCap,
  onSetAll,
}: {
  t: TFn;
  busy: boolean;
  newModelPolicy: "on" | "off";
  contextCapValue: number;
  showCustom: boolean;
  customCap: string;
  allCapped: boolean;
  onNewModelPolicy: (policy: "on" | "off") => void;
  onSelectCap: (raw: string) => void;
  onCustomCapChange: (value: string) => void;
  onApplyCustomCap: () => void;
  onSetAll: () => void;
}) {
  const policyLabel = t("models.settings.newModels");
  return (
    <div className="models-catalog-settings">
      <SettingsField label={policyLabel}>
        <Select
          value={newModelPolicy}
          options={[
            { value: "on", label: t("pws.enabledLabel") },
            { value: "off", label: t("pws.disabledLabel") },
          ]}
          onChange={value => onNewModelPolicy(value === "off" ? "off" : "on")}
          disabled={busy}
          label={policyLabel}
        />
      </SettingsField>

      <ContextCapEditor
        t={t}
        busy={busy}
        contextCapValue={contextCapValue}
        showCustom={showCustom}
        customCap={customCap}
        onSelectCap={onSelectCap}
        onCustomCapChange={onCustomCapChange}
        onApplyCustomCap={onApplyCustomCap}
      />

      <ApplyExistingControl t={t} busy={busy} allCapped={allCapped} onSetAll={onSetAll} />
    </div>
  );
}
