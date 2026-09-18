import type { ReactNode } from "react";
import { useT } from "../../i18n/shared";
import { configDash, configOnOff, type ConfigPacingRule, type ConfigPacingView } from "../../provider-workspace/config-pacing";
import CodexAccountPickerSetting from "../CodexAccountPickerSetting";
import DefaultModeRequestUserInputSetting from "../DefaultModeRequestUserInputSetting";

type Section = "general" | "connections" | "discovery" | "pacing";
type PacingRuntime = { currentQueue: number; untilNextSlotMs: number; lastModel?: string };

export function ConfigFact({
  label, value, on, field, children,
}: {
  label: string;
  value?: string;
  on?: boolean;
  field?: boolean;
  children?: ReactNode;
}) {
  return (
    <div className={field ? "providers-config-field" : undefined}>
      <dt>{label}</dt>
      <dd className={on === true ? "providers-config-on" : on === false ? "providers-config-off" : undefined}>
        {children ?? value}
      </dd>
    </div>
  );
}

export function ConfigEditLink({
  section,
  editing,
  saving,
  canEdit,
  onEdit,
  onCancel,
}: {
  section: Section;
  editing: Section | null;
  saving: boolean;
  canEdit: boolean;
  onEdit: (section: Section) => void;
  onCancel: () => void;
}) {
  const t = useT();
  if (!canEdit) return null;
  const open = editing === section;
  return (
    <span className={`providers-config-edit-actions${open ? " is-editing" : ""}`}>
      <button
        type="button"
        className="providers-link providers-link--plain"
        disabled={open && saving}
        onClick={() => onEdit(section)}
      >
        {open ? (saving ? t("pws.saving") : t("pws.saveSettings")) : t("prov.config.edit")}
      </button>
      <button
        type="button"
        className="providers-link providers-link--plain providers-config-edit-cancel"
        disabled={!open || saving}
        tabIndex={open ? undefined : -1}
        onClick={onCancel}
      >
        {t("common.cancel")}
      </button>
    </span>
  );
}

export function ConfigGeneralSection({
  itemName,
  adapter,
  baseUrl,
  defaultModel,
  note,
  itemAdapter,
  itemBaseUrl,
  itemDefaultModel,
  itemNote,
  openaiLogical,
  preset,
  editing,
  modelOptions,
  editLink,
  onAdapter,
  onBaseUrl,
  onDefaultModel,
  onNote,
}: {
  itemName: string;
  adapter: string;
  baseUrl: string;
  defaultModel: string;
  note: string;
  itemAdapter: string;
  itemBaseUrl: string;
  itemDefaultModel?: string;
  itemNote?: string;
  openaiLogical: boolean;
  preset: boolean;
  editing: boolean;
  modelOptions: string[];
  editLink: ReactNode;
  onAdapter: (value: string) => void;
  onBaseUrl: (value: string) => void;
  onDefaultModel: (value: string) => void;
  onNote: (value: string) => void;
}) {
  const t = useT();
  return (
    <section className="providers-block">
      <div className="providers-block-head">
        <h4>{t("prov.config.general")}</h4>
        {editLink}
      </div>
      <dl className="providers-facts providers-facts--2">
        <ConfigFact field label={t("pws.providerId")} value={itemName}>
          {editing ? <input className="input" value={itemName} readOnly disabled /> : undefined}
        </ConfigFact>
        {!openaiLogical && (
          <ConfigFact field label={t("modal.adapter")} value={configDash(itemAdapter)}>
            {editing ? (
              <input className="input" value={adapter} readOnly={preset} disabled={preset} onChange={e => onAdapter(e.target.value)} />
            ) : undefined}
          </ConfigFact>
        )}
        {!openaiLogical && (
          <ConfigFact field label={t("modal.baseUrl")} value={configDash(itemBaseUrl)}>
            {editing ? (
              <input className="input" value={baseUrl} readOnly={preset} disabled={preset} onChange={e => onBaseUrl(e.target.value)} />
            ) : undefined}
          </ConfigFact>
        )}
        <ConfigFact field label={t("pws.cell.defaultModel")} value={configDash(itemDefaultModel)}>
          {editing ? (
            modelOptions.length > 0 ? (
              <select className="input" value={defaultModel} onChange={e => onDefaultModel(e.target.value)}>
                <option value="">{t("pws.defaultModelNone")}</option>
                {modelOptions.map(id => <option key={id} value={id}>{id}</option>)}
              </select>
            ) : (
              <input className="input" value={defaultModel} onChange={e => onDefaultModel(e.target.value)} />
            )
          ) : undefined}
        </ConfigFact>
        <ConfigFact field label={t("prov.config.note")} value={configDash(itemNote)}>
          {editing ? (
            <input className="input" value={note} onChange={e => onNote(e.target.value)} />
          ) : undefined}
        </ConfigFact>
      </dl>
    </section>
  );
}

export function ConfigConnectionsSection({ apiLaneBaseUrl }: { apiLaneBaseUrl?: string }) {
  const t = useT();
  return (
    <section className="providers-block">
      <div className="providers-block-head">
        <h4>{t("prov.config.connections")}</h4>
      </div>
      <dl className="providers-facts providers-facts--2 providers-facts--stack">
        <ConfigFact label={t("prov.config.chatgptAccess")} value={t("prov.config.chatgptManaged")} />
        <ConfigFact label={t("prov.config.openaiResponses")}>
          <code>{apiLaneBaseUrl || "https://api.openai.com/v1"}</code>
        </ConfigFact>
      </dl>
    </section>
  );
}

export function ConfigDiscoverySection({
  openaiLogical,
  editing,
  privateState,
  discoveryState,
  allowPrivateNetwork,
  liveModels,
  discoverySupported,
  editLink,
  onAllowPrivateNetwork,
  onLiveModels,
}: {
  openaiLogical: boolean;
  editing: boolean;
  privateState: { label: string; on: boolean };
  discoveryState: { label: string; on: boolean };
  allowPrivateNetwork: boolean;
  liveModels: boolean;
  discoverySupported: boolean;
  editLink: ReactNode;
  onAllowPrivateNetwork: (value: boolean) => void;
  onLiveModels: (value: boolean) => void;
}) {
  const t = useT();
  return (
    <section className="providers-block">
      <div className="providers-block-head">
        <h4>{t("prov.config.discovery")}</h4>
        {editLink}
      </div>
      <dl className={openaiLogical ? "providers-facts" : "providers-facts providers-facts--2"}>
        {!openaiLogical && (
          <ConfigFact
            label={t("pws.allowPrivateNetwork")}
            value={privateState.label}
            on={editing ? configOnOff(t, allowPrivateNetwork).on : privateState.on}
          >
            {editing ? (
              <label className="providers-config-inline-check">
                {configOnOff(t, allowPrivateNetwork).label}
                <input type="checkbox" checked={allowPrivateNetwork} onChange={e => onAllowPrivateNetwork(e.target.checked)} />
              </label>
            ) : undefined}
          </ConfigFact>
        )}
        <ConfigFact
          label={t("prov.config.discoverModels")}
          value={discoveryState.label}
          on={editing ? configOnOff(t, liveModels).on : discoveryState.on}
        >
          {editing ? (
            <label className="providers-config-inline-check">
              {configOnOff(t, liveModels).label}
              <input type="checkbox" checked={liveModels} disabled={!discoverySupported} onChange={e => onLiveModels(e.target.checked)} />
            </label>
          ) : undefined}
        </ConfigFact>
      </dl>
    </section>
  );
}

export function ConfigPacingSection({
  openaiLogical,
  editing,
  pacingState,
  savedPacing,
  pacingEnabled,
  pacingRpm,
  pacingDelay,
  pacingRuntime,
  editLink,
  onEnabled,
  onRpm,
  onDelay,
}: {
  openaiLogical: boolean;
  editing: boolean;
  pacingState: { label: string; on: boolean };
  savedPacing: ConfigPacingView;
  pacingEnabled: boolean;
  pacingRpm: string;
  pacingDelay: string;
  pacingRuntime: PacingRuntime | null;
  editLink: ReactNode;
  onEnabled: (value: boolean) => void;
  onRpm: (value: string) => void;
  onDelay: (value: string) => void;
}) {
  const t = useT();
  return (
    <section className="providers-block">
      <div className="providers-block-head">
        <h4>{openaiLogical ? t("prov.config.apiPacing") : t("pws.pacingTitle")}</h4>
        {editLink}
      </div>
      <dl className="providers-facts providers-facts--pacing">
        <ConfigFact
          label={openaiLogical ? t("prov.config.apiPacing") : t("pws.pacingTitle")}
          value={pacingState.label}
          on={editing ? configOnOff(t, pacingEnabled).on : pacingState.on}
        >
          {editing ? (
            <label className="providers-config-inline-check">
              {configOnOff(t, pacingEnabled).label}
              <input type="checkbox" checked={pacingEnabled} onChange={e => onEnabled(e.target.checked)} />
            </label>
          ) : undefined}
        </ConfigFact>
        {openaiLogical && (
          <>
            <ConfigFact label={t("prov.config.currentQueue")} value={pacingRuntime ? String(pacingRuntime.currentQueue) : "—"} />
            <ConfigFact label={t("prov.config.untilNextSlot")} value={pacingRuntime ? `${pacingRuntime.untilNextSlotMs} ms` : "—"} />
            <ConfigFact label={t("prov.config.lastModel")} value={configDash(pacingRuntime?.lastModel)} />
          </>
        )}
        <ConfigFact field label={t("pws.pacingRpm")} value={configDash(savedPacing.rpm)}>
          {editing ? (
            <input className="input" type="number" min="0" value={pacingRpm} onChange={e => onRpm(e.target.value)} />
          ) : undefined}
        </ConfigFact>
        <ConfigFact field label={t("pws.pacingDelay")} value={configDash(savedPacing.minMs)}>
          {editing ? (
            <input className="input" type="number" min="1" value={pacingDelay} onChange={e => onDelay(e.target.value)} />
          ) : undefined}
        </ConfigFact>
      </dl>
    </section>
  );
}

export function ConfigOverridesSection({
  itemName,
  openaiLogical,
  canEdit,
  saving,
  addingOverride,
  overrideId,
  overrideRpm,
  overrideDelay,
  overrideEntries,
  availableModels,
  onStartAdd,
  onAdd,
  onOverrideId,
  onOverrideRpm,
  onOverrideDelay,
  onCancelAdd,
  onRemove,
}: {
  itemName: string;
  openaiLogical: boolean;
  canEdit: boolean;
  saving: boolean;
  addingOverride: boolean;
  overrideId: string;
  overrideRpm: string;
  overrideDelay: string;
  overrideEntries: [string, ConfigPacingRule][];
  availableModels: string[];
  onStartAdd: () => void;
  onAdd: () => void;
  onOverrideId: (value: string) => void;
  onOverrideRpm: (value: string) => void;
  onOverrideDelay: (value: string) => void;
  onCancelAdd: () => void;
  onRemove: (model: string) => void;
}) {
  const t = useT();
  return (
    <section className="providers-block providers-config-overrides">
      <div className="providers-block-head">
        <h4>{openaiLogical ? t("prov.config.apiModelPacing") : t("prov.config.modelPacing")}</h4>
      </div>
      <div className="providers-config-overrides-grid">
        {canEdit && (
          <div className="providers-config-overrides-add-link">
            <button
              type="button"
              className="providers-link providers-link--plain"
              disabled={saving}
              onClick={() => {
                if (!addingOverride) {
                  onStartAdd();
                  return;
                }
                onAdd();
              }}
            >
              {t("prov.config.addOverride")}
            </button>
          </div>
        )}
        <div className="providers-config-overrides-head">
          <span id={`config-ov-model-${itemName}`}>{t("pws.pacingModel")}</span>
          <span id={`config-ov-rpm-${itemName}`}>{t("pws.pacingRpm")}</span>
          <span id={`config-ov-delay-${itemName}`}>{t("pws.pacingDelay")}</span>
          <span id={`config-ov-actions-${itemName}`}>{t("prov.config.actions")}</span>
        </div>
        {canEdit && addingOverride && (
          <div className="providers-config-overrides-add">
            <span>
              <input
                className="input"
                list={`config-pacing-models-${itemName}`}
                value={overrideId}
                aria-labelledby={`config-ov-model-${itemName}`}
                onChange={e => onOverrideId(e.target.value)}
              />
              <datalist id={`config-pacing-models-${itemName}`}>{availableModels.map(id => <option key={id} value={id} />)}</datalist>
            </span>
            <input
              className="input"
              type="number"
              min="0"
              value={overrideRpm}
              aria-labelledby={`config-ov-rpm-${itemName}`}
              onChange={e => onOverrideRpm(e.target.value)}
            />
            <input
              className="input"
              type="number"
              min="1"
              value={overrideDelay}
              aria-labelledby={`config-ov-delay-${itemName}`}
              onChange={e => onOverrideDelay(e.target.value)}
            />
            <div className="providers-config-override-actions" aria-labelledby={`config-ov-actions-${itemName}`}>
              <button type="button" className="providers-link providers-link--plain" disabled={saving} onClick={onCancelAdd}>{t("common.cancel")}</button>
            </div>
          </div>
        )}
        <ul className="providers-config-overrides-rows">
          {overrideEntries.length === 0 && !addingOverride && (
            <li className="providers-config-overrides-empty">
              <span>—</span>
              <span>—</span>
              <span>—</span>
              <span />
            </li>
          )}
          {overrideEntries.map(([model, rule]) => (
            <li key={model}>
              <span>{model}</span>
              <span>{rule.requestsPerMinute ?? "—"}</span>
              <span>{rule.minIntervalMs ?? "—"}</span>
              <span>
                {canEdit && (
                  <button type="button" className="providers-link providers-link--plain" disabled={saving} onClick={() => onRemove(model)}>
                    {t("prov.config.delete")}
                  </button>
                )}
              </span>
            </li>
          ))}
        </ul>
      </div>
    </section>
  );
}

/**
 * Codex-client settings on the OpenAI provider's Configuration tab.
 *
 * These two rows used to sit behind the standalone Codex Auth page's "Advanced settings"
 * disclosure. Configuration is the provider surface that owns Codex-level settings, so the
 * disclosure is gone and the rows live here with the rest of the provider's configuration.
 */
export function ConfigCodexSection({ apiBase }: { apiBase: string }) {
  const t = useT();
  return (
    <section className="providers-block">
      <div className="providers-block-head">
        <h4>{t("nav.codexAuth")}</h4>
      </div>
      <div className="providers-config-codex">
        <CodexAccountPickerSetting apiBase={apiBase} />
        <DefaultModeRequestUserInputSetting apiBase={apiBase} />
      </div>
    </section>
  );
}
