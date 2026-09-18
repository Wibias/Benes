/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useEffect, useRef, useState, type ReactNode } from "react";
import { formatProviderDisplayName } from "../provider-icons";
import type { TFn, TKey } from "../i18n/shared";
import { Notice, Select } from "../ui";
import { CUSTOM_OPTION, REASONING_EFFORT_LEVELS } from "./models-shared";
import { ModelsModalShell } from "./models-modal-shell";

function useAutofocus<T extends HTMLElement>(active: boolean) {
  const ref = useRef<T>(null);
  useEffect(() => {
    if (!active) return;
    window.requestAnimationFrame(() => ref.current?.focus());
  }, [active]);
  return ref;
}

function ModalHeading({
  title,
  closeLabel,
  onClose,
  disabled = false,
}: {
  title: string;
  closeLabel: string;
  onClose: () => void;
  disabled?: boolean;
}) {
  return (
    <div className="modal-head">
      <h3>{title}</h3>
      <button
        type="button"
        className="btn btn-ghost btn-sm"
        aria-label={closeLabel}
        onClick={onClose}
        disabled={disabled}
      >
        &times;
      </button>
    </div>
  );
}

function ModalButtons({ children }: { children: ReactNode }) {
  return <div className="modal-actions">{children}</div>;
}

export function ModelsV2HelpModal({
  open,
  t,
  onClose,
}: {
  open: boolean;
  t: TFn;
  onClose: () => void;
}) {
  const title = t("models.v2Label");
  return (
    <ModelsModalShell open={open} label={title} onClose={onClose}>
      <ModalHeading title={title} closeLabel={t("common.close")} onClose={onClose} />
      <p className="modal-desc leading-relaxed" style={{ whiteSpace: "pre-line" }}>
        {t("models.v2Help")}
      </p>
      <div className="models-help-link">
        <a
          className="text-control"
          href="https://github.com/Wibias/Benes/guides/sub-agent-surface/"
          target="_blank"
          rel="noreferrer"
          style={{ color: "var(--accent)" }}
        >
          {t("models.v2DocsLink")}
        </a>
      </div>
      <ModalButtons>
        <button type="button" className="btn btn-primary" onClick={onClose}>
          {t("common.ok")}
        </button>
      </ModalButtons>
    </ModelsModalShell>
  );
}

function ContextModelFields({
  t,
  saving,
  modelIds,
  modelId,
  modelDrafts,
  onSelectModel,
  onModelDraftChange,
}: {
  t: TFn;
  saving: boolean;
  modelIds: string[];
  modelId: string;
  modelDrafts: Record<string, string>;
  onSelectModel: (modelId: string) => void;
  onModelDraftChange: (value: string) => void;
}) {
  if (modelIds.length === 0) return null;
  const modelLabel = t("models.contextModel");
  return (
    <>
      <div className="text-label models-field">
        {modelLabel}
        <Select
          value={modelId}
          options={modelIds.map(id => ({ value: id, label: id }))}
          onChange={onSelectModel}
          disabled={saving}
          label={modelLabel}
        />
      </div>
      <label className="text-label models-field">
        {t("models.contextModelOverride")}
        <input
          className="input"
          inputMode="numeric"
          value={modelDrafts[modelId] ?? ""}
          onChange={event => onModelDraftChange(event.target.value)}
          disabled={saving}
          placeholder={t("models.contextAutomatic")}
        />
      </label>
    </>
  );
}

export function ModelsContextSettingsModal({
  provider,
  t,
  error,
  saving,
  defaultDraft,
  modelIds,
  modelId,
  modelDrafts,
  onClose,
  onDefaultChange,
  onSelectModel,
  onModelDraftChange,
  onSave,
}: {
  provider: string | null;
  t: TFn;
  error: string;
  saving: boolean;
  defaultDraft: string;
  modelIds: string[];
  modelId: string;
  modelDrafts: Record<string, string>;
  onClose: () => void;
  onDefaultChange: (value: string) => void;
  onSelectModel: (modelId: string) => void;
  onModelDraftChange: (value: string) => void;
  onSave: () => void;
}) {
  const [retainedProvider, setRetainedProvider] = useState(provider);
  if (provider !== null && provider !== retainedProvider) {
    setRetainedProvider(provider);
  }
  const activeProvider = provider ?? retainedProvider;
  const defaultInput = useAutofocus<HTMLInputElement>(provider !== null);
  if (!activeProvider) return null;

  const title = t("models.contextSettingsTitle", {
    provider: formatProviderDisplayName(activeProvider, t),
  });

  return (
    <ModelsModalShell
      open={provider !== null}
      label={t("models.contextSettings")}
      onClose={onClose}
      closeDisabled={saving}
    >
      <ModalHeading
        title={title}
        closeLabel={t("common.close")}
        onClose={onClose}
        disabled={saving}
      />
      {error ? <Notice tone="err">{error}</Notice> : null}
      <p className="modal-desc leading-relaxed">{t("models.contextHint")}</p>
      <div className="models-context-fields">
        <label className="text-label models-field">
          {t("models.contextDefault")}
          <input
            ref={defaultInput}
            className="input"
            inputMode="numeric"
            value={defaultDraft}
            onChange={event => onDefaultChange(event.target.value)}
            disabled={saving}
            placeholder={t("models.contextAutomatic")}
          />
        </label>
        <ContextModelFields
          t={t}
          saving={saving}
          modelIds={modelIds}
          modelId={modelId}
          modelDrafts={modelDrafts}
          onSelectModel={onSelectModel}
          onModelDraftChange={onModelDraftChange}
        />
      </div>
      <ModalButtons>
        <button type="button" className="btn btn-ghost" onClick={onClose} disabled={saving}>
          {t("common.cancel")}
        </button>
        <button type="button" className="btn btn-primary" onClick={onSave} disabled={saving}>
          {saving ? t("models.customSaving") : t("models.customApply")}
        </button>
      </ModalButtons>
    </ModelsModalShell>
  );
}

const CUSTOM_CONTEXT_OPTIONS: Array<{ value: string; label: string }> = [
  { value: "", label: "—" },
  { value: "100000", label: "100k" },
  { value: "128000", label: "128k" },
  { value: "200000", label: "200k" },
  { value: "256000", label: "256k" },
  { value: "352000", label: "352k" },
  { value: "500000", label: "500k" },
  { value: "1000000", label: "1M" },
];

function ContextWindowEditor({
  t,
  saving,
  contextWindow,
  showCustom,
  onSelect,
  onChange,
}: {
  t: TFn;
  saving: boolean;
  contextWindow: string;
  showCustom: boolean;
  onSelect: (value: string) => void;
  onChange: (value: string) => void;
}) {
  const label = t("models.customFieldContext");
  const options = [...CUSTOM_CONTEXT_OPTIONS, { value: CUSTOM_OPTION, label: t("models.custom") }];
  return (
    <label className="text-label models-field">
      {label}
      <div className="row models-field-row">
        <Select
          value={showCustom ? CUSTOM_OPTION : contextWindow}
          options={options}
          onChange={onSelect}
          disabled={saving}
          label={label}
        />
        {showCustom ? (
          <input
            className="input"
            style={{ width: 120 }}
            inputMode="numeric"
            value={contextWindow}
            onChange={event => onChange(event.target.value)}
            disabled={saving}
            placeholder={t("models.customPlaceholder")}
            aria-label={label}
          />
        ) : null}
      </div>
    </label>
  );
}

function ToggleChoice({
  checked,
  disabled,
  label,
  onChange,
}: {
  checked: boolean;
  disabled: boolean;
  label: string;
  onChange: (checked: boolean) => void;
}) {
  return (
    <label className="row models-modality-option">
      <input
        type="checkbox"
        checked={checked}
        onChange={event => onChange(event.target.checked)}
        disabled={disabled}
      />
      <span className="text-control">{label}</span>
    </label>
  );
}

function ModelsCustomModalFields({
  t,
  saving,
  modelId,
  displayName,
  contextWindow,
  showCustomCtx,
  modalities,
  reasoning,
  reasoningEfforts,
  onModelIdChange,
  onDisplayNameChange,
  onContextSelect,
  onContextWindowChange,
  onToggleModality,
  onToggleReasoning,
  onToggleEffort,
  open,
}: {
  t: TFn;
  saving: boolean;
  modelId: string;
  displayName: string;
  contextWindow: string;
  showCustomCtx: boolean;
  modalities: string[];
  reasoning: boolean;
  reasoningEfforts: string[];
  onModelIdChange: (value: string) => void;
  onDisplayNameChange: (value: string) => void;
  onContextSelect: (value: string) => void;
  onContextWindowChange: (value: string) => void;
  onToggleModality: (modality: string, checked: boolean) => void;
  onToggleReasoning: (checked: boolean) => void;
  onToggleEffort: (effort: string, checked: boolean) => void;
  open: boolean;
}) {
  const modelIdInput = useAutofocus<HTMLInputElement>(open);
  return (
    <div className="models-field-stack">
      <label className="text-label models-field">
        {t("models.customFieldModelId")}
        <input
          ref={modelIdInput}
          className="input"
          value={modelId}
          onChange={event => onModelIdChange(event.target.value)}
          disabled={saving}
          placeholder={t("models.customFieldModelIdPlaceholder")}
        />
      </label>
      <label className="text-label models-field">
        {t("models.customFieldDisplayName")}
        <input
          className="input"
          value={displayName}
          onChange={event => onDisplayNameChange(event.target.value)}
          disabled={saving}
          placeholder={t("models.customFieldDisplayNamePlaceholder")}
        />
      </label>
      <ContextWindowEditor
        t={t}
        saving={saving}
        contextWindow={contextWindow}
        showCustom={showCustomCtx}
        onSelect={onContextSelect}
        onChange={onContextWindowChange}
      />
      <div className="text-label models-field">
        {t("models.customFieldModalities")}
        <div className="row models-field-row">
          {(["text", "image", "audio"] as const).map(modality => (
            <ToggleChoice
              key={modality}
              checked={modalities.includes(modality)}
              disabled={saving}
              label={modality}
              onChange={checked => onToggleModality(modality, checked)}
            />
          ))}
        </div>
      </div>
      <div className="text-label models-field">
        {t("models.customFieldReasoning")}
        <div className="row models-field-row">
          <ToggleChoice
            checked={reasoning}
            disabled={saving}
            label={t("models.customFieldReasoningOverride")}
            onChange={onToggleReasoning}
          />
        </div>
        {reasoning ? (
          <div className="row models-field-row" style={{ flexWrap: "wrap" }}>
            {REASONING_EFFORT_LEVELS.map(effort => (
              <ToggleChoice
                key={effort}
                checked={reasoningEfforts.includes(effort)}
                disabled={saving}
                label={t(`models.reasoningEffort.${effort}` as TKey)}
                onChange={checked => onToggleEffort(effort, checked)}
              />
            ))}
          </div>
        ) : null}
      </div>
    </div>
  );
}

export function ModelsCustomModelModal({
  open,
  mode,
  provider,
  t,
  error,
  saving,
  modelId,
  displayName,
  contextWindow,
  showCustomCtx,
  modalities,
  reasoning,
  reasoningEfforts,
  onClose,
  onModelIdChange,
  onDisplayNameChange,
  onContextSelect,
  onContextWindowChange,
  onToggleModality,
  onToggleReasoning,
  onToggleEffort,
  onSubmit,
}: {
  open: boolean;
  mode: "add" | "edit";
  provider: string;
  t: TFn;
  error: string;
  saving: boolean;
  modelId: string;
  displayName: string;
  contextWindow: string;
  showCustomCtx: boolean;
  modalities: string[];
  reasoning: boolean;
  reasoningEfforts: string[];
  onClose: () => void;
  onModelIdChange: (value: string) => void;
  onDisplayNameChange: (value: string) => void;
  onContextSelect: (value: string) => void;
  onContextWindowChange: (value: string) => void;
  onToggleModality: (modality: string, checked: boolean) => void;
  onToggleReasoning: (checked: boolean) => void;
  onToggleEffort: (effort: string, checked: boolean) => void;
  onSubmit: () => void;
}) {
  const providerName = formatProviderDisplayName(provider, t);
  const title = t(mode === "add" ? "models.customAddTitle" : "models.customEditTitle", { provider: providerName });
  const submitLabel = saving
    ? t("models.customSaving")
    : t(mode === "add" ? "models.customAddBtn" : "models.customEditBtn");
  const submitDisabled = saving || modelId.trim().length === 0;

  return (
    <ModelsModalShell
      open={open}
      label={t("models.customAdd")}
      onClose={onClose}
      closeDisabled={saving}
    >
      <ModalHeading
        title={title}
        closeLabel={t("common.close")}
        onClose={onClose}
        disabled={saving}
      />
      {error ? <Notice tone="err">{error}</Notice> : null}
      <ModelsCustomModalFields
        open={open}
        t={t}
        saving={saving}
        modelId={modelId}
        displayName={displayName}
        contextWindow={contextWindow}
        showCustomCtx={showCustomCtx}
        modalities={modalities}
        reasoning={reasoning}
        reasoningEfforts={reasoningEfforts}
        onModelIdChange={onModelIdChange}
        onDisplayNameChange={onDisplayNameChange}
        onContextSelect={onContextSelect}
        onContextWindowChange={onContextWindowChange}
        onToggleModality={onToggleModality}
        onToggleReasoning={onToggleReasoning}
        onToggleEffort={onToggleEffort}
      />
      <ModalButtons>
        <button type="button" className="btn btn-ghost" onClick={onClose} disabled={saving}>
          {t("common.cancel")}
        </button>
        <button type="button" className="btn btn-primary" disabled={submitDisabled} onClick={onSubmit}>
          {submitLabel}
        </button>
      </ModalButtons>
    </ModelsModalShell>
  );
}
