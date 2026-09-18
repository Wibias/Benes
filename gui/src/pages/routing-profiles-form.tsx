/** Create/edit routing profile form — layout matches the Profiles mock (structure/copy, not chrome colors). */
import type { Dispatch, PointerEvent, SetStateAction, ReactNode } from "react";
import { Notice, Select, Tooltip } from "../ui";
import type { TFn, TKey, Locale } from "../i18n/shared";
import { IconAlertCircle, IconGrip, IconTrash } from "../icons";
import { usePointerReorder } from "../components/subagents-workspace/use-pointer-reorder";
import { formatProviderDisplayName } from "../provider-icons";
import { routingCompatibilityLabels } from "../i18n/routing-compatibility";
import {
  type ModelOption,
  type OptionalBoolean,
  type RoutingProfileDraft,
  type RoutingProfileDto,
  type UnknownCostCapMode,
  type UnknownEvidenceMode,
} from "../routing-profile/profile-model";
import type { LabCatalogSuite } from "../lab/lab-catalog";
import {
  BOOLEAN_REQUIREMENTS,
  OPTIMIZE_KEYS,
  STRING_REQUIREMENTS,
  UNKNOWN_COST_CAP_OPTIONS,
  UNKNOWN_EVIDENCE_KEYS,
  UNKNOWN_EVIDENCE_OPTIONS,
  suiteSelected,
} from "./routing-profiles-format";
import { CATALOG_CONTEXT_PRESETS } from "./models-catalog-filter";
import { REASONING_EFFORT_LEVELS } from "./models-shared";
import { profileTitle } from "./routing-profile-summary";

export type RoutingProfileFormView = {
  t: TFn;
  locale: Locale;
  selected: RoutingProfileDto | null;
  draft: RoutingProfileDraft | null;
  saving: boolean;
  catalogSuites: LabCatalogSuite[];
  catalogError: boolean;
  providerNames: string[];
  selectedModelOptions: ModelOption[][];
  setDraft: Dispatch<SetStateAction<RoutingProfileDraft | null>>;
  updateCandidate: (index: number, field: "provider" | "model", value: string) => void;
  addCandidate: () => void;
  removeCandidate: (index: number) => void;
  moveCandidate: (from: number, to: number) => void;
  onSave: () => void;
  onCancel: () => void;
  onRemove: () => void;
};

const REQUIRE_BOOL_LABEL: Record<(typeof BOOLEAN_REQUIREMENTS)[number], string> = {
  tools: "routing.require.tools",
  imageInput: "routing.require.image",
  structuredOutput: "routing.require.structured",
  localOnly: "routing.require.localOnly",
  remoteAllowed: "routing.require.remoteAllowed",
  encryptedCodexTasks: "routing.require.encryptedCodexTasks",
};

const REQUIRE_STRING_LABEL: Record<(typeof STRING_REQUIREMENTS)[number], string> = {
  reasoningEffort: "routing.require.reasoningEffort",
  serviceTier: "routing.require.serviceTier",
};

const OPTIMIZE_LABEL: Record<(typeof OPTIMIZE_KEYS)[number], string> = {
  latency: "routing.optimize.latency",
  health: "routing.optimize.health",
  cost: "routing.optimize.cost",
  quota: "routing.optimize.quota",
};

const UNKNOWN_LABEL: Record<(typeof UNKNOWN_EVIDENCE_KEYS)[number], string> = {
  capability: "routing.form.evidence.capability",
  health: "routing.form.evidence.health",
  quota: "routing.form.evidence.quota",
  cost: "routing.form.evidence.cost",
};

function FormSection({
  title,
  hint,
  infoHint,
  action,
  children,
}: {
  title: string;
  hint?: string;
  infoHint?: string;
  action?: ReactNode;
  children: ReactNode;
}) {
  return (
    <section className="routing-form-section">
      <div className="routing-form-section-head">
        <div className="routing-form-section-title">
          <h4>{title}</h4>
          {infoHint ? (
            <Tooltip content={infoHint}>
              <span className="routing-form-field-info" aria-label={infoHint}>
                <IconAlertCircle width={14} height={14} aria-hidden="true" />
              </span>
            </Tooltip>
          ) : null}
        </div>
        {action ? <div className="routing-form-section-action">{action}</div> : null}
      </div>
      {hint ? <p className="muted routing-form-hint">{hint}</p> : null}
      {children}
    </section>
  );
}

function Field({
  label,
  hint,
  infoHint,
  children,
}: {
  label: string;
  hint?: string;
  infoHint?: string;
  children: ReactNode;
}) {
  return (
    <div className="routing-form-field">
      <span className={`routing-form-field-label${infoHint ? " routing-form-field-label--with-info" : ""}`}>
        {label}
        {infoHint ? (
          <Tooltip content={infoHint}>
            <span className="routing-form-field-info" aria-label={infoHint}>
              <IconAlertCircle width={14} height={14} aria-hidden="true" />
            </span>
          </Tooltip>
        ) : null}
      </span>
      {children}
      {hint ? <span className="muted routing-form-field-hint">{hint}</span> : null}
    </div>
  );
}

const SERVICE_TIER_VALUES = ["default", "priority", "flex"] as const;

function requireBoolOptions(t: TFn) {
  return [
    { value: "", label: t("routing.require.any") },
    { value: "true", label: t("routing.require.required") },
    { value: "false", label: t("routing.require.off") },
  ];
}

function requireContextOptions(t: TFn, current: string) {
  const options = [
    { value: "", label: t("routing.require.any") },
    ...CATALOG_CONTEXT_PRESETS.map((tokens) => ({
      value: String(tokens),
      label: `${tokens / 1000}k`,
    })),
  ];
  if (current.trim() !== "" && !options.some((option) => option.value === current)) {
    options.splice(1, 0, { value: current, label: current });
  }
  return options;
}

function requireReasoningOptions(t: TFn, current: string) {
  const options = [
    { value: "", label: t("routing.require.any") },
    ...REASONING_EFFORT_LEVELS.map((effort) => ({
      value: effort,
      label: t(`models.reasoningEffort.${effort}` as TKey),
    })),
  ];
  if (current.trim() !== "" && !options.some((option) => option.value === current)) {
    options.splice(1, 0, { value: current, label: current });
  }
  return options;
}

function unknownCostOptions(t: TFn) {
  return UNKNOWN_COST_CAP_OPTIONS.map((mode) => ({
    value: mode,
    label: t(`routing.unknownEvidence.${mode}` as const),
  }));
}

function unknownEvidenceModeOptions(t: TFn) {
  return UNKNOWN_EVIDENCE_OPTIONS.map((mode) => ({
    value: mode,
    label: t(`routing.unknownEvidence.${mode}` as const),
  }));
}

function candidateProviderOptions(t: TFn, providers: string[]) {
  return providers.map((provider) => ({
    value: provider,
    label: formatProviderDisplayName(provider, t),
  }));
}

function candidateModelOptions(models: ModelOption[], current: string) {
  const options = models.map((model) => ({ value: model.id, label: model.id }));
  if (current.trim() !== "" && !options.some((option) => option.value === current)) {
    options.unshift({ value: current, label: current });
  }
  return options;
}

function requireServiceTierOptions(t: TFn, current: string) {
  const options = [
    { value: "", label: t("routing.require.any") },
    ...SERVICE_TIER_VALUES.map((tier) => ({
      value: tier,
      label: t(`routing.require.serviceTier.${tier}` as TKey),
    })),
  ];
  if (current.trim() !== "" && !options.some((option) => option.value === current)) {
    options.splice(1, 0, { value: current, label: current });
  }
  return options;
}

function fractionToPercentInput(value: string): string {
  if (value.trim() === "") return "";
  const n = Number(value);
  if (!Number.isFinite(n)) return value;
  return String(Math.round(n * 1000) / 10);
}

function percentInputToFraction(value: string): string {
  if (value.trim() === "") return "";
  const n = Number(value);
  if (!Number.isFinite(n)) return value;
  return String(n / 100);
}

function optimizeTotalPercent(draft: RoutingProfileDraft): number {
  let total = 0;
  for (const key of OPTIMIZE_KEYS) {
    const n = Number(draft.optimize[key]);
    if (Number.isFinite(n)) total += n;
  }
  return Math.round(total * 1000) / 10;
}

function RoutingProfileIdentityFields({ view }: { view: RoutingProfileFormView }) {
  const { t, draft, selected, setDraft } = view;
  if (!draft) return null;
  const clientRoute = draft.id.trim() ? `policy/${draft.id.trim()}` : "";
  return (
    <FormSection title={t("routing.form.identity")}>
      <div className="routing-form-grid routing-form-grid--3">
        <Field label={t("routing.form.profileId")} hint={t("routing.form.profileIdHint")}>
          <input
            className="input"
            required
            disabled={selected !== null}
            value={draft.id}
            onChange={(event) =>
              setDraft((current) => (current ? { ...current, id: event.target.value } : current))
            }
          />
        </Field>
        <Field label={t("routing.form.clientRoute")} hint={t("routing.form.clientRouteHint")}>
          <input className="input" readOnly value={clientRoute} />
        </Field>
        <Field label={t("routing.form.alias")} hint={t("routing.form.aliasHint")}>
          <input
            className="input"
            value={draft.alias}
            placeholder={t("routing.form.aliasPlaceholder")}
            onChange={(event) =>
              setDraft((current) => (current ? { ...current, alias: event.target.value } : current))
            }
          />
        </Field>
      </div>
    </FormSection>
  );
}

function RoutingProfileCandidates({ view }: { view: RoutingProfileFormView }) {
  const {
    t,
    draft,
    providerNames,
    selectedModelOptions,
    updateCandidate,
    addCandidate,
    removeCandidate,
    moveCandidate,
  } = view;
  const candidateIds = draft?.candidates.map((candidate) => candidate.key) ?? [];
  const { draggingId, begin, bindList, bindGhost } = usePointerReorder({
    boundSelector: ".routing-candidate-list",
    itemAttr: "data-routing-candidate-id",
  });
  const onGripPointerDown = (candidateKey: string, event: PointerEvent<HTMLButtonElement>) => {
    const candidates = draft?.candidates ?? [];
    const candidate = candidates.find((row) => row.key === candidateKey);
    if (!candidate) return;
    const providerLabel = candidate.provider
      ? formatProviderDisplayName(candidate.provider, t)
      : t("routing.form.provider");
    const modelLabel = candidate.model || t("routing.form.model");
    begin({
      id: candidateKey,
      event,
      ids: candidateIds,
      onSelect: () => {},
      onCommit: (sourceId, targetId) => {
        const from = candidates.findIndex((row) => row.key === sourceId);
        const to = candidates.findIndex((row) => row.key === targetId);
        if (from < 0 || to < 0 || from === to) return;
        moveCandidate(from, to);
      },
      fillGhost: (ghost, hoverIndex) => {
        const indexEl = ghost.querySelector("[data-ghost-index]");
        const providerEl = ghost.querySelector("[data-ghost-provider]");
        const modelEl = ghost.querySelector("[data-ghost-model]");
        if (indexEl) indexEl.textContent = String(hoverIndex + 1);
        if (providerEl) providerEl.textContent = providerLabel;
        if (modelEl) modelEl.textContent = modelLabel;
      },
      paintItem: (row, id, order) => {
        const indexEl = row.querySelector(".routing-candidate-index");
        if (!(indexEl instanceof HTMLElement)) return;
        const next = order.indexOf(id);
        if (next >= 0) indexEl.textContent = String(next + 1);
      },
    });
  };
  if (!draft) return null;
  return (
    <FormSection
      title={t("routing.candidates")}
      infoHint={t("routing.form.candidatesOrderHint")}
      action={
        <button type="button" className="providers-link providers-link--plain routing-candidate-add" onClick={addCandidate}>
          {t("routing.form.addCandidate")}
        </button>
      }
    >
      <div
        className={`routing-candidate-list${draggingId ? " is-reordering" : ""}`}
        ref={bindList}
      >
        {draft.candidates.map((candidate, index) => {
          const candidateProviders = [...new Set([candidate.provider, ...providerNames])].filter(Boolean);
          const providerLabel = t("routing.form.provider");
          const modelLabel = t("routing.form.model");
          const providerOptions = candidateProviderOptions(t, candidateProviders);
          const modelOptions = candidateModelOptions(
            selectedModelOptions[index] ?? [],
            candidate.model,
          );
          const modelValue = modelOptions.some((option) => option.value === candidate.model)
            ? candidate.model
            : (modelOptions[0]?.value ?? "");
          return (
            <div
              key={candidate.key}
              className={`routing-candidate-row${draggingId === candidate.key ? " is-dragging" : ""}`}
              data-routing-candidate-id={candidate.key}
            >
              <span className="routing-candidate-index" aria-hidden="true">
                {index + 1}
              </span>
              <button
                type="button"
                className="routing-candidate-grip"
                aria-label={t("routing.form.candidateDrag")}
                title={t("routing.form.candidateDrag")}
                onPointerDown={(event) => onGripPointerDown(candidate.key, event)}
              >
                <IconGrip width={14} height={14} aria-hidden="true" />
              </button>
              <Select
                value={
                  providerOptions.some((option) => option.value === candidate.provider)
                    ? candidate.provider
                    : (providerOptions[0]?.value ?? "")
                }
                options={providerOptions}
                onChange={(value) => updateCandidate(index, "provider", value)}
                label={providerLabel}
                align="left"
                chevron="down"
                style={{ width: "100%", display: "block", minWidth: 0 }}
              />
              <Select
                value={modelValue}
                options={modelOptions}
                onChange={(value) => updateCandidate(index, "model", value)}
                label={modelLabel}
                align="left"
                chevron="down"
                style={{ width: "100%", display: "block", minWidth: 0 }}
              />
              <button
                type="button"
                className="btn btn-ghost btn-icon"
                disabled={draft.candidates.length === 1}
                onClick={() => removeCandidate(index)}
                aria-label={t("routing.removeCandidate", {
                  provider: candidate.provider,
                  model: candidate.model,
                })}
              >
                <IconTrash width={14} height={14} aria-hidden="true" />
              </button>
            </div>
          );
        })}
        <div
          ref={bindGhost}
          className="routing-candidate-ghost"
          hidden
          aria-hidden="true"
        >
          <span className="routing-candidate-index" data-ghost-index />
          <span className="routing-candidate-ghost-grip">
            <IconGrip width={14} height={14} aria-hidden="true" />
          </span>
          <span className="routing-candidate-ghost-field" data-ghost-provider />
          <span className="routing-candidate-ghost-field" data-ghost-model />
          <span className="routing-candidate-ghost-spacer" />
        </div>
      </div>
    </FormSection>
  );
}

function RoutingProfileRequire({ view }: { view: RoutingProfileFormView }) {
  const { t, draft, setDraft } = view;
  if (!draft) return null;
  const boolOptions = requireBoolOptions(t);
  return (
    <FormSection title={t("routing.section.requirements")}>
      <div className="routing-form-grid routing-form-grid--5">
        {BOOLEAN_REQUIREMENTS.slice(0, 3).map((key) => {
          const label = t(REQUIRE_BOOL_LABEL[key] as Parameters<TFn>[0]);
          return (
            <Field key={key} label={label}>
              <Select
                value={draft.require[key]}
                options={boolOptions}
                onChange={(value) =>
                  setDraft((current) =>
                    current
                      ? {
                          ...current,
                          require: { ...current.require, [key]: value as OptionalBoolean },
                        }
                      : current,
                  )
                }
                label={label}
                align="left"
                chevron="down"
                style={{ width: "100%", display: "block" }}
              />
            </Field>
          );
        })}
        <Field label={t("routing.minContext")}>
          <Select
            value={draft.require.minContextWindow}
            options={requireContextOptions(t, draft.require.minContextWindow)}
            onChange={(value) =>
              setDraft((current) =>
                current
                  ? {
                      ...current,
                      require: { ...current.require, minContextWindow: value },
                    }
                  : current,
              )
            }
            label={t("routing.minContext")}
            align="left"
            chevron="down"
            style={{ width: "100%", display: "block" }}
          />
        </Field>
        <div className="routing-form-field">
          <span className="routing-form-field-label">
            {t("routing.require.minQuotaHeadroom")}
          </span>
          <div className="routing-form-input-unit">
            <input
              className="input"
              type="number"
              min={0}
              max={100}
              step="any"
              value={fractionToPercentInput(draft.require.minQuotaHeadroom)}
              placeholder={t("routing.require.any")}
              aria-label={t("routing.require.minQuotaHeadroom")}
              onChange={(event) =>
                setDraft((current) =>
                  current
                    ? {
                        ...current,
                        require: {
                          ...current.require,
                          minQuotaHeadroom: percentInputToFraction(event.target.value),
                        },
                      }
                    : current,
                )
              }
            />
            <span className="routing-form-input-unit-mark" aria-hidden="true">
              %
            </span>
          </div>
        </div>
        {STRING_REQUIREMENTS.map((key) => {
          const label = t(REQUIRE_STRING_LABEL[key] as Parameters<TFn>[0]);
          const options =
            key === "reasoningEffort"
              ? requireReasoningOptions(t, draft.require.reasoningEffort)
              : requireServiceTierOptions(t, draft.require.serviceTier);
          return (
            <Field key={key} label={label}>
              <Select
                value={draft.require[key]}
                options={options}
                onChange={(value) =>
                  setDraft((current) =>
                    current
                      ? {
                          ...current,
                          require: { ...current.require, [key]: value },
                        }
                      : current,
                  )
                }
                label={label}
                align="left"
                chevron="down"
                style={{ width: "100%", display: "block" }}
              />
            </Field>
          );
        })}
        {BOOLEAN_REQUIREMENTS.slice(3).map((key) => {
          const label = t(REQUIRE_BOOL_LABEL[key] as Parameters<TFn>[0]);
          return (
            <Field key={key} label={label}>
              <Select
                value={draft.require[key]}
                options={boolOptions}
                onChange={(value) =>
                  setDraft((current) =>
                    current
                      ? {
                          ...current,
                          require: { ...current.require, [key]: value as OptionalBoolean },
                        }
                      : current,
                  )
                }
                label={label}
                align="left"
                chevron="down"
                style={{ width: "100%", display: "block" }}
              />
            </Field>
          );
        })}
      </div>
    </FormSection>
  );
}

function RoutingProfileOptimize({ view }: { view: RoutingProfileFormView }) {
  const { t, draft, setDraft } = view;
  if (!draft) return null;
  const total = optimizeTotalPercent(draft);
  return (
    <FormSection
      title={t("routing.section.optimisation")}
      action={
        <span className="muted routing-form-total">
          {t("routing.form.optimizeTotal", { n: String(total) })}
        </span>
      }
    >
      <div className="routing-form-grid routing-form-grid--4">
        {OPTIMIZE_KEYS.map((key) => (
          <Field key={key} label={t(OPTIMIZE_LABEL[key] as Parameters<TFn>[0])}>
            <div className="routing-form-input-unit">
              <input
                className="input"
                type="number"
                min={0}
                max={100}
                step="any"
                required
                value={fractionToPercentInput(draft.optimize[key])}
                aria-label={t(OPTIMIZE_LABEL[key] as Parameters<TFn>[0])}
                onChange={(event) =>
                  setDraft((current) =>
                    current
                      ? {
                          ...current,
                          optimize: {
                            ...current.optimize,
                            [key]: percentInputToFraction(event.target.value),
                          },
                        }
                      : current,
                  )
                }
              />
              <span className="routing-form-input-unit-mark" aria-hidden="true">
                %
              </span>
            </div>
          </Field>
        ))}
      </div>
    </FormSection>
  );
}

function RoutingProfileLimitsAndEvidence({ view }: { view: RoutingProfileFormView }) {
  const { t, draft, setDraft } = view;
  if (!draft) return null;
  return (
    <section className="routing-form-section">
      <div className="routing-form-split routing-form-split--limits-evidence">
        <div className="routing-form-section-title">
          <h4>{t("routing.section.limits")}</h4>
        </div>
        <div className="routing-form-section-title">
          <h4>{t("routing.unknownEvidence")}</h4>
          <Tooltip content={t("routing.form.unknownEvidenceHint")}>
            <span
              className="routing-form-field-info"
              aria-label={t("routing.form.unknownEvidenceHint")}
            >
              <IconAlertCircle width={14} height={14} aria-hidden="true" />
            </span>
          </Tooltip>
        </div>
        <div className="routing-form-grid routing-form-grid--2">
          <Field label={t("routing.form.maxEstimatedCost")}>
            <input
              className="input"
              type="number"
              min={0}
              step="any"
              value={draft.limits.maxEstimatedCostUsd}
              onChange={(event) =>
                setDraft((current) =>
                  current
                    ? {
                        ...current,
                        limits: { ...current.limits, maxEstimatedCostUsd: event.target.value },
                      }
                    : current,
                )
              }
            />
          </Field>
          <Field
            label={t("routing.form.unknownCost")}
            infoHint={t("routing.form.unknownCostHint")}
          >
            <Select
              value={draft.limits.onUnknownCost}
              options={unknownCostOptions(t)}
              onChange={(value) =>
                setDraft((current) =>
                  current
                    ? {
                        ...current,
                        limits: {
                          ...current.limits,
                          onUnknownCost: value as UnknownCostCapMode,
                        },
                      }
                    : current,
                )
              }
              label={t("routing.form.unknownCost")}
              align="left"
              chevron="down"
              style={{ width: "100%", display: "block" }}
            />
          </Field>
        </div>
        <div className="routing-form-grid routing-form-grid--2">
          {UNKNOWN_EVIDENCE_KEYS.map((key) => (
            <Field key={key} label={t(UNKNOWN_LABEL[key] as Parameters<TFn>[0])}>
              <Select
                value={draft.unknownEvidence[key]}
                options={unknownEvidenceModeOptions(t)}
                onChange={(value) =>
                  setDraft((current) =>
                    current
                      ? {
                          ...current,
                          unknownEvidence: {
                            ...current.unknownEvidence,
                            [key]: value as UnknownEvidenceMode,
                          },
                        }
                      : current,
                  )
                }
                label={t(UNKNOWN_LABEL[key] as Parameters<TFn>[0])}
                align="left"
                chevron="down"
                style={{ width: "100%", display: "block" }}
              />
            </Field>
          ))}
        </div>
      </div>
    </section>
  );
}

function RoutingProfileCompatibility({ view }: { view: RoutingProfileFormView }) {
  const { t, draft, setDraft, catalogSuites, catalogError, locale } = view;
  const compatibilityFieldLabels = routingCompatibilityLabels(locale);
  if (!draft) return null;
  return (
    <FormSection title={t("routing.form.advanced")}>
      <div className="routing-form-toggle-row">
        <strong className="routing-form-toggle-label">
          {t("routing.compatibility.title")}
          <Tooltip content={t("routing.form.compatibilityHint")}>
            <span
              className="routing-form-field-info"
              aria-label={t("routing.form.compatibilityHint")}
            >
              <IconAlertCircle width={14} height={14} aria-hidden="true" />
            </span>
          </Tooltip>
          <button
            type="button"
            className={`switch${draft.compatibility.enabled ? " on" : ""}`}
            aria-pressed={draft.compatibility.enabled}
            aria-label={t("routing.compatibility.enabled")}
            onClick={() =>
              setDraft((current) =>
                current
                  ? {
                      ...current,
                      compatibility: {
                        ...current.compatibility,
                        enabled: !current.compatibility.enabled,
                      },
                    }
                  : current,
              )
            }
          >
            <span className="knob" />
          </button>
        </strong>
      </div>
      {draft.compatibility.enabled ? (
        <div className="routing-form-grid routing-form-grid--4 routing-form-compat-fields">
          <div className="routing-form-field" style={{ gridColumn: "1 / -1" }}>
            <span className="routing-form-field-label">
              {t("routing.compatibility.requiredSuites")}
            </span>
            {catalogError ? (
              <Notice tone="warn">{t("routing.compatibility.catalogUnavailable")}</Notice>
            ) : null}
            {catalogSuites.length > 0 ? (
              <div className="routing-form-suite-list">
                {catalogSuites.map((suite) => (
                  <label key={suite.key} className="checkbox">
                    <input
                      type="checkbox"
                      checked={suiteSelected(draft.compatibility.requiredSuites, suite)}
                      onChange={(event) =>
                        setDraft((current) => {
                          if (!current) return current;
                          const selected = event.target.checked;
                          const requiredSuites = selected
                            ? [
                                ...current.compatibility.requiredSuites.filter(
                                  (row) =>
                                    !(
                                      row.suiteId === suite.suiteId
                                      && row.evidenceLayer === suite.evidenceLayer
                                    ),
                                ),
                                { suiteId: suite.suiteId, evidenceLayer: suite.evidenceLayer },
                              ]
                            : current.compatibility.requiredSuites.filter(
                                (row) =>
                                  !(
                                    row.suiteId === suite.suiteId
                                    && row.evidenceLayer === suite.evidenceLayer
                                  ),
                              );
                          return {
                            ...current,
                            compatibility: { ...current.compatibility, requiredSuites },
                          };
                        })
                      }
                    />
                    <span>
                      {suite.suiteId}{" "}
                      <span className="muted">
                        (
                        {t(
                          `routing.compatibility.layer.${suite.evidenceLayer}` as const,
                        )}
                        )
                      </span>
                    </span>
                  </label>
                ))}
              </div>
            ) : null}
          </div>
          <Field label={t("routing.compatibility.minStatus")}>
            <Select
              value={draft.compatibility.minStatus}
              options={[
                { value: "", label: t("routing.require.any") },
                { value: "PROBED", label: t("lab.verdict.PROBED") },
                { value: "VERIFIED", label: t("lab.verdict.VERIFIED") },
              ]}
              onChange={(value) =>
                setDraft((current) =>
                  current
                    ? {
                        ...current,
                        compatibility: {
                          ...current.compatibility,
                          minStatus: value as RoutingProfileDraft["compatibility"]["minStatus"],
                        },
                      }
                    : current,
                )
              }
              label={t("routing.compatibility.minStatus")}
              align="left"
              chevron="down"
              style={{ width: "100%", display: "block" }}
            />
          </Field>
          <Field label={compatibilityFieldLabels.maxEvidenceAgeMs}>
            <input
              className="input"
              type="number"
              min={0}
              value={draft.compatibility.maxEvidenceAgeMs}
              onChange={(event) =>
                setDraft((current) =>
                  current
                    ? {
                        ...current,
                        compatibility: {
                          ...current.compatibility,
                          maxEvidenceAgeMs: event.target.value,
                        },
                      }
                    : current,
                )
              }
            />
          </Field>
          <Field label={compatibilityFieldLabels.unknownEvidence}>
            <Select
              value={draft.compatibility.unknownEvidence}
              options={unknownEvidenceModeOptions(t)}
              onChange={(value) =>
                setDraft((current) =>
                  current
                    ? {
                        ...current,
                        compatibility: {
                          ...current.compatibility,
                          unknownEvidence: value as UnknownEvidenceMode,
                        },
                      }
                    : current,
                )
              }
              label={compatibilityFieldLabels.unknownEvidence}
              align="left"
              chevron="down"
              style={{ width: "100%", display: "block" }}
            />
          </Field>
          <Field label={compatibilityFieldLabels.degradedEvidence}>
            <Select
              value={draft.compatibility.degradedEvidence}
              options={unknownEvidenceModeOptions(t)}
              onChange={(value) =>
                setDraft((current) =>
                  current
                    ? {
                        ...current,
                        compatibility: {
                          ...current.compatibility,
                          degradedEvidence: value as UnknownEvidenceMode,
                        },
                      }
                    : current,
                )
              }
              label={compatibilityFieldLabels.degradedEvidence}
              align="left"
              chevron="down"
              style={{ width: "100%", display: "block" }}
            />
          </Field>
        </div>
      ) : null}
    </FormSection>
  );
}

export function RoutingProfileDraftForm({ view }: { view: RoutingProfileFormView }) {
  const { t, selected, draft, saving, onSave, onCancel, onRemove } = view;
  if (!draft) return null;
  return (
    <form
      className="routing-detail routing-detail-form"
      onSubmit={(event) => {
        event.preventDefault();
        void onSave();
      }}
    >
      <div className="routing-detail-head">
        <h3>{selected ? profileTitle(selected) : t("routing.form.createTitle")}</h3>
        <div className="routing-detail-actions">
          <button type="button" className="btn btn-ghost btn-sm" disabled={saving} onClick={onCancel}>
            {t("common.cancel")}
          </button>
          <button type="submit" className="btn btn-primary btn-sm" disabled={saving}>
            {saving
              ? t("common.saving")
              : selected
                ? t("common.save")
                : t("routing.createProfile")}
          </button>
          {selected ? (
            <button
              type="button"
              className="btn btn-ghost btn-sm"
              disabled={saving}
              onClick={() => void onRemove()}
            >
              {t("common.remove")}
            </button>
          ) : null}
        </div>
      </div>
      <RoutingProfileIdentityFields view={view} />
      <RoutingProfileCandidates view={view} />
      <RoutingProfileRequire view={view} />
      <RoutingProfileOptimize view={view} />
      <RoutingProfileLimitsAndEvidence view={view} />
      <RoutingProfileCompatibility view={view} />
    </form>
  );
}
