/** Benes dashboard client for the Go proxy (`internal/server`). */
import { IconChevron, IconPlus } from "../icons";
import type { TFn } from "../i18n/shared";
import { type ModelVisibilityTarget, type ModelVisibilityScope, type ProviderModelMap } from "../model-visibility";
import type { ProviderModelGroup } from "../models-groups";
import { formatProviderDisplayName } from "../provider-icons";
import { ProviderMark } from "../components/ProviderMark";
import { Switch, Select } from "../ui";
import { EmptyProviderHint } from "./models-provider-hints";
import { NewArrivalNote } from "./models-new-policy-control";
import { modelsProviderCardState } from "./models-provider-group-data";
import {
  catalogContextSelectOptions,
  CONTEXT_MIXED_VALUE,
  contextMaxOptionLabel,
  contextSelectValue,
  gpt56Family,
  modelAdvertisedMax,
  modelContextChoices,
  modelContextDisplayValue,
  providerContextChoices,
  providerContextSummary,
} from "./models-catalog-filter";
import {
  discoveryFailureLabel,
  NATIVE_GPT56_DEFAULT_WINDOW,
  type ModelRow,
} from "./models-shared";

export type ModelsProviderGroupProps = {
  group: ProviderModelGroup<ModelRow>;
  t: TFn;
  busy: boolean;
  collapsed: ReadonlySet<string>;
  selectedModelMap: ProviderModelMap;
  disabled: ReadonlySet<string>;
  contextCaps: Record<string, number>;
  contextCapValue: number;
  limit: Record<string, number>;
  arrivalNote?: string;
  onToggleCollapse: (provider: string) => void;
  onAddCustom: (provider: string) => void;
  onEditCustom: (model: ModelRow) => void;
  onDeleteCustom: (model: ModelRow) => void;
  onApplyVisibility: (
    scope: ModelVisibilityScope,
    provider: string,
    targets: ModelVisibilityTarget[],
    enabled: boolean,
  ) => Promise<void>;
  onSelectProviderContext: (provider: string, raw: string) => void;
  onSelectModelContext: (provider: string, modelId: string, raw: string) => void;
  onShowMore: (provider: string, shown: number) => void;
  onSyncModels?: (provider: string) => void;
};

function nativeAdvertisedFloor(nativeProviderGroup: boolean, modelId: string): number | undefined {
  if (!nativeProviderGroup || !gpt56Family(modelId)) return undefined;
  return NATIVE_GPT56_DEFAULT_WINDOW;
}

function ModelsProviderContextSelect({
  provider,
  summary,
  presets,
  current,
  advertisedMax,
  override,
  busy,
  t,
  onSelect,
}: {
  provider: string;
  summary: ReturnType<typeof providerContextSummary>;
  presets: readonly number[];
  current?: number;
  advertisedMax?: number;
  override?: number | null;
  busy: boolean;
  t: TFn;
  onSelect: (provider: string, raw: string) => void;
}) {
  if (summary.kind === "empty") return null;
  const mixed = summary.kind === "mixed";
  const maxTokens = typeof advertisedMax === "number" && advertisedMax > 0 ? advertisedMax : 0;
  const value = mixed
    ? CONTEXT_MIXED_VALUE
    : contextSelectValue({
      display: summary.tokens,
      advertisedMax: maxTokens,
      override,
    });
  return (
    <Select
      value={value}
      options={catalogContextSelectOptions({
        presets,
        current,
        mixed,
        mixedLabel: t("models.contextMixed"),
        maxLabel: contextMaxOptionLabel(maxTokens, t("models.contextMax")),
      })}
      onChange={value => onSelect(provider, value)}
      disabled={busy}
      label={t("models.col.context")}
    />
  );
}

function ModelsProviderModelRow({
  model,
  provider,
  off,
  t,
  busy,
  contextValue,
  advertisedMax,
  onApplyVisibility,
  onEditCustom,
  onDeleteCustom,
  onSelectModelContext,
}: {
  model: ModelRow;
  provider: string;
  off: boolean;
  t: TFn;
  busy: boolean;
  contextValue: number;
  advertisedMax: number;
  onApplyVisibility: ModelsProviderGroupProps["onApplyVisibility"];
  onEditCustom: ModelsProviderGroupProps["onEditCustom"];
  onDeleteCustom: ModelsProviderGroupProps["onDeleteCustom"];
  onSelectModelContext: ModelsProviderGroupProps["onSelectModelContext"];
}) {
  const presets = modelContextChoices({
    advertisedMax,
    standard: model.standardContextWindow,
  });
  return (
    <tr className="models-catalog-row-model">
      <td>
        <div className="models-catalog-identity models-catalog-identity--model">
          <code className={`models-catalog-model-id${off ? " is-off" : ""}`}>
            {model.id}
          </code>
          {model.custom && (
            <span className="models-chip muted mono text-caption">{t("models.customBadge")}</span>
          )}
        </div>
      </td>
      <td>
        <Select
          value={contextSelectValue({
            display: contextValue,
            advertisedMax,
            override: undefined,
          })}
          options={catalogContextSelectOptions({
            presets,
            current: contextValue === advertisedMax ? undefined : contextValue,
            mixedLabel: t("models.contextMixed"),
            maxLabel: contextMaxOptionLabel(advertisedMax, t("models.contextMax")),
          })}
          onChange={value => onSelectModelContext(provider, model.id, value)}
          disabled={busy}
          label={t("models.col.context")}
        />
      </td>
      <td>
        <div className="models-catalog-visibility">
          {model.custom && (
            <span className="models-catalog-row-actions">
              <button type="button" className="btn btn-ghost btn-sm text-caption" onClick={() => onEditCustom(model)}>
                {t("models.customEdit")}
              </button>
              <button
                type="button"
                className="btn btn-ghost btn-sm text-caption"
                style={{ color: "var(--red)" }}
                onClick={() => onDeleteCustom(model)}
              >
                {t("models.customDelete")}
              </button>
            </span>
          )}
          <Switch
            on={!off}
            onClick={() => void onApplyVisibility("models", provider, [{ id: model.id, native: model.native === true }], off)}
            disabled={busy}
            label={model.native ? model.id : model.namespaced}
          />
        </div>
      </td>
    </tr>
  );
}

function groupContextSelectState(
  rows: ModelRow[],
  summary: ReturnType<typeof providerContextSummary>,
  advertisedMaxes: number[],
  overrides: Record<string, number> | undefined,
) {
  const sharedMax = advertisedMaxes.length > 0 && advertisedMaxes.every(max => max === advertisedMaxes[0])
    ? advertisedMaxes[0]
    : undefined;
  return {
    presets: providerContextChoices({
      advertisedMaxes,
      standards: rows.map(model => model.standardContextWindow),
    }),
    current: summary.kind === "value" && summary.tokens !== (advertisedMaxes[0] ?? 0)
      ? summary.tokens
      : undefined,
    advertisedMax: summary.kind === "value" ? sharedMax : undefined,
    override: summary.kind === "value" ? overrides?.[rows[0]?.id ?? ""] : undefined,
  };
}

export function ModelsProviderGroup(props: ModelsProviderGroupProps) {
  const card = modelsProviderCardState(props.group, {
    collapsed: props.collapsed,
    selectedModelMap: props.selectedModelMap,
    disabled: props.disabled,
    contextCaps: props.contextCaps,
    contextCapValue: props.contextCapValue,
    search: {},
    limit: props.limit,
  });
  const { provider, rows } = card;
  const providerLabel = formatProviderDisplayName(provider, props.t);
  const contextValues = rows.map(model => modelContextDisplayValue({
    override: props.group.modelContextWindows?.[model.id],
    advertised: model.contextWindow,
    providerCap: card.capDisplayValue,
  }));
  const summary = providerContextSummary(contextValues);
  const advertisedMaxes = rows.map(model => modelAdvertisedMax({
    advertised: model.contextWindow,
    providerCap: card.capDisplayValue,
    nativeFloor: nativeAdvertisedFloor(card.nativeProviderGroup, model.id),
  }));
  const groupContext = groupContextSelectState(
    rows,
    summary,
    advertisedMaxes,
    props.group.modelContextWindows,
  );
  const bulkToggle = (enable: boolean) => {
    if (!card.hasRows) return;
    void props.onApplyVisibility(
      "provider",
      provider,
      rows.map(model => ({ id: model.id, native: model.native === true })),
      enable,
    );
  };
  return (
    <>
      <tr className="models-catalog-row-group">
        <td>
          <div className="models-catalog-identity">
            <button
              type="button"
              className="models-catalog-collapse"
              onClick={() => props.onToggleCollapse(provider)}
              aria-expanded={!card.isCollapsed}
              aria-label={providerLabel}
            >
              <IconChevron
                width={14}
                height={14}
                aria-hidden="true"
                style={{ transform: card.isCollapsed ? "none" : "rotate(90deg)", transition: "transform .12s" }}
              />
            </button>
            <ProviderMark name={provider} className="models-catalog-provider-icon" />
            <span className="models-catalog-provider-name">{providerLabel}</span>
            {card.discoveryFailure && (
              <span
                className="badge badge-amber"
                role="status"
                title={discoveryFailureLabel(props.t, card.discoveryFailure)}
              >
                {props.t("models.discoveryFailedBadge")}
              </span>
            )}
            <span className="models-catalog-visible-count">
              {props.t("models.active", { active: card.activeCount, total: rows.length })}
            </span>
            <NewArrivalNote text={props.arrivalNote} />
            <button
              type="button"
              className="models-catalog-add"
              onClick={() => props.onAddCustom(provider)}
              aria-label={props.t("models.customAdd")}
              title={props.t("models.customAdd")}
              aria-haspopup="dialog"
            >
              <IconPlus width={12} height={12} aria-hidden="true" />
            </button>
          </div>
        </td>
        <td>
          <ModelsProviderContextSelect
            provider={provider}
            summary={summary}
            presets={groupContext.presets}
            current={groupContext.current}
            advertisedMax={groupContext.advertisedMax}
            override={groupContext.override}
            busy={props.busy}
            t={props.t}
            onSelect={props.onSelectProviderContext}
          />
        </td>
        <td>
          {card.isCollapsed ? (
            <div className="models-catalog-group-actions">
              <button
                type="button"
                className="models-catalog-collapse models-catalog-collapse--end"
                onClick={() => props.onToggleCollapse(provider)}
                aria-expanded={false}
                tabIndex={-1}
                aria-hidden="true"
              >
                <IconChevron width={14} height={14} aria-hidden="true" />
              </button>
            </div>
          ) : (
            <div className="models-catalog-group-actions">
              <button
                type="button"
                className="models-catalog-bulk"
                disabled={props.busy || card.allOn}
                onClick={() => bulkToggle(true)}
              >
                {props.t("models.enableAll")}
              </button>
              <span className="models-catalog-bulk-sep" aria-hidden="true">|</span>
              <button
                type="button"
                className="models-catalog-bulk"
                disabled={props.busy || card.allOff}
                onClick={() => bulkToggle(false)}
              >
                {props.t("models.disableAll")}
              </button>
            </div>
          )}
        </td>
      </tr>
      {!card.isCollapsed && (
        <ModelsProviderBody card={card} props={props} />
      )}
    </>
  );
}

function ModelsProviderBody({
  card,
  props,
}: {
  card: ReturnType<typeof modelsProviderCardState>;
  props: ModelsProviderGroupProps;
}) {
  const { provider, rows, liveModels, discovery } = card;
  return (
    <>
      {rows.length === 0 && (
        <tr className="models-catalog-empty-row">
          <td colSpan={3}>
            <EmptyProviderHint
              liveModels={liveModels}
              discovery={discovery}
              showFailureBadge={false}
              onSync={props.onSyncModels && liveModels ? () => props.onSyncModels?.(provider) : undefined}
              syncing={props.busy}
            />
          </td>
        </tr>
      )}
      {card.visible.map(model => (
        <ModelsProviderModelRow
          key={model.namespaced}
          model={model}
          provider={provider}
          off={!card.isVisible(model)}
          t={props.t}
          busy={props.busy}
          contextValue={modelContextDisplayValue({
            override: props.group.modelContextWindows?.[model.id],
            advertised: model.contextWindow,
            providerCap: card.capDisplayValue,
          })}
          advertisedMax={modelAdvertisedMax({
            advertised: model.contextWindow,
            providerCap: card.capDisplayValue,
            nativeFloor: nativeAdvertisedFloor(card.nativeProviderGroup, model.id),
          })}
          onApplyVisibility={props.onApplyVisibility}
          onEditCustom={props.onEditCustom}
          onDeleteCustom={props.onDeleteCustom}
          onSelectModelContext={props.onSelectModelContext}
        />
      ))}
      {card.remaining > 0 && (
        <tr>
          <td colSpan={3}>
            <button
              type="button"
              onClick={() => props.onShowMore(provider, card.shown)}
              className="btn btn-ghost btn-sm models-show-more"
            >
              {props.t("models.showMore", { n: card.remaining })}
            </button>
          </td>
        </tr>
      )}
    </>
  );
}
