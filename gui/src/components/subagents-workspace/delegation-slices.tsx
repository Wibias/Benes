/** Presentation slices for Subagents delegation / Ultra controls. */
import { useMemo, useState } from "react";
import { Select } from "../../ui";
import { useT } from "../../i18n/shared";
import { formatNamespacedModelId } from "../../provider-icons";
import {
  ULTRA_MODE_PRESET,
  ultraModeHintActive,
  type DelegationModelOption,
  type DelegationPatch,
  type UltraModePatch,
  type UltraModeState,
} from "../../pages/subagents-delegation-contract.ts";

export { ULTRA_MODE_PRESET };

function SettingCopy(props: { title: string; hint?: string }) {
  return (
    <div className="setting-copy">
      <div className="font-semibold">{props.title}</div>
      {props.hint ? <div className="muted setting-hint">{props.hint}</div> : null}
    </div>
  );
}

export function DelegationLoadFailed(props: { compact: boolean; onRetry: () => void }) {
  const t = useT();
  return (
    <div className="swi-delegation-row">
      <SettingCopy title={t("sub.ultraMode")} hint={props.compact ? undefined : t("sub.ultraModeLoadFail")} />
      <button type="button" className="btn btn-ghost btn-sm" onClick={props.onRetry}>
        {t("common.retry")}
      </button>
    </div>
  );
}

function buildModelSelectOptions(
  available: DelegationModelOption[],
  noneLabel: string,
  formatLabel: (namespaced: string) => string,
) {
  return [
    { value: "", label: noneLabel },
    ...available.map((entry) => ({
      value: entry.namespaced,
      label: formatLabel(`${entry.provider}/${entry.model}`),
    })),
  ];
}

function buildEffortSelectOptions(efforts: string[], noneLabel: string) {
  return [
    { value: "", label: noneLabel },
    ...efforts.map((entry) => ({ value: entry, label: entry })),
  ];
}

export function DelegationModelSlice(props: {
  model: string;
  effort: string;
  efforts: string[];
  available: DelegationModelOption[];
  saving: boolean;
  onSave: (patch: DelegationPatch) => void;
}) {
  const t = useT();
  const modelOptions = useMemo(
    () => buildModelSelectOptions(props.available, t("dash.injectionNone"), (id) => formatNamespacedModelId(id, t)),
    [props.available, t],
  );
  const effortOptions = useMemo(
    () => buildEffortSelectOptions(props.efforts, t("dash.injectionEffortNone")),
    [props.efforts, t],
  );
  const showEffort = props.model.length > 0 && props.efforts.length > 0;

  return (
    <div className="swi-delegation-row">
      <SettingCopy title={t("sub.delegation.model")} hint={t("sub.delegation.modelHint")} />
      <div className="swi-delegation-controls">
        <Select
          value={props.model}
          options={modelOptions}
          onChange={(value) => props.onSave({ model: value || null, effort: props.effort || null })}
          disabled={props.saving}
          label={t("dash.injectionLabel")}
          align="right"
        />
        {showEffort ? (
          <Select
            value={props.effort}
            options={effortOptions}
            onChange={(value) => props.onSave({ model: props.model || null, effort: value || null })}
            disabled={props.saving}
            label={t("dash.injectionEffortLabel")}
            align="right"
          />
        ) : null}
      </div>
    </div>
  );
}

function SwitchRow(props: {
  title: string;
  hint?: string;
  pressed: boolean;
  disabled: boolean;
  ariaLabel: string;
  onToggle: () => void;
  footnote?: string;
}) {
  return (
    <div className="swi-delegation-row">
      <SettingCopy title={props.title} hint={props.hint} />
      <button
        type="button"
        className={props.pressed ? "switch on" : "switch"}
        onClick={props.onToggle}
        disabled={props.disabled}
        aria-label={props.ariaLabel}
        aria-pressed={props.pressed}
      >
        <span className="knob" />
      </button>
      {props.footnote ? <div className="muted setting-hint">{props.footnote}</div> : null}
    </div>
  );
}

function resolveUltraFootnote(v2Enabled: boolean, requiredCopy: string): string | undefined {
  return v2Enabled ? undefined : requiredCopy;
}

export function DelegationPoliciesSlice(props: {
  model: string;
  guidanceEnabled: boolean;
  syncCodexDefaults: boolean;
  saving: boolean;
  onSave: (patch: DelegationPatch) => void;
  ultraMode: UltraModeState;
  ultraSaving: boolean;
  onUltraModeSave: (patch: UltraModePatch) => void;
  compact: boolean;
}) {
  const t = useT();
  const ultraOn = ultraModeHintActive(props.ultraMode.hintText);
  const syncTitle = props.compact ? t("sub.capabilities.sync") : t("dash.syncCodexSubagentDefaults");
  const guidanceTitle = props.compact ? t("sub.capabilities.guidance") : t("dash.multiAgentGuidance");

  return (
    <>
      <SwitchRow
        title={syncTitle}
        hint={props.compact ? undefined : t("dash.syncCodexSubagentDefaultsHint")}
        pressed={props.syncCodexDefaults}
        disabled={props.saving || props.model.length === 0}
        ariaLabel={t("dash.syncCodexSubagentDefaults")}
        onToggle={() => props.onSave({ syncCodexSubagentDefaults: !props.syncCodexDefaults })}
      />
      <SwitchRow
        title={guidanceTitle}
        hint={props.compact ? undefined : t("dash.multiAgentGuidanceHint")}
        pressed={props.guidanceEnabled}
        disabled={props.saving}
        ariaLabel={t("dash.multiAgentGuidance")}
        onToggle={() => props.onSave({ multiAgentGuidanceEnabled: !props.guidanceEnabled })}
      />
      <SwitchRow
        title={t("sub.ultraMode")}
        hint={props.compact ? undefined : t("sub.ultraModeHint")}
        pressed={ultraOn}
        disabled={props.saving || props.ultraSaving || (!ultraOn && !props.ultraMode.multiAgentV2Enabled)}
        ariaLabel={t("sub.ultraMode")}
        onToggle={() => props.onUltraModeSave({ multiAgentModeHintText: ultraOn ? null : ULTRA_MODE_PRESET })}
        footnote={resolveUltraFootnote(props.ultraMode.multiAgentV2Enabled, t("sub.ultraModeV2Required"))}
      />
    </>
  );
}

export function UltraModeEditor(props: {
  initialHint: string;
  disabled: boolean;
  onSave: (patch: UltraModePatch) => void;
  preset: string;
  compact?: boolean;
  labels: { text: string; preset: string; save: string };
}) {
  const [draft, setDraft] = useState(props.initialHint);
  const empty = draft.trim().length === 0;
  return (
    <>
      <textarea
        className="input swi-ultra-mode-textarea"
        value={draft}
        onChange={(event) => setDraft(event.target.value)}
        disabled={props.disabled}
        rows={props.compact ? 2 : 4}
        aria-label={props.labels.text}
      />
      <button type="button" className="btn btn-ghost btn-sm" onClick={() => setDraft(props.preset)} disabled={props.disabled}>
        {props.labels.preset}
      </button>
      <button
        type="button"
        className="btn btn-primary btn-sm"
        onClick={() => {
          if (empty) return;
          props.onSave({ multiAgentModeHintText: draft });
        }}
        disabled={props.disabled || empty}
      >
        {props.labels.save}
      </button>
    </>
  );
}
