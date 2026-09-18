/**
 * ProviderConfiguration — divider sections matching the Providers
 * Configuration mock: General, Discovery, Request pacing, model overrides.
 */
import { useEffect, useMemo, useRef, useState } from "react";
import { useT } from "../../i18n/shared";
import { navigateHash } from "../../hash-routing";
import { isCatalogProviderId } from "../../provider-icons";
import {
  configDiscoveryPatch,
  configGeneralPatch,
  configOnOff,
  configOverrideRule,
  configPacingMissingRule,
  configPacingPayload,
  pacingFromItem,
  type ConfigPacingRule,
} from "../../provider-workspace/config-pacing";
import {
  configurationModelOptions,
  configurationOverrideDraft,
} from "../../provider-workspace/config-draft";
import type { ProviderUpdatePatch } from "../../provider-workspace/provider-mutations";
import type { WorkspaceItem, WorkspaceProvider } from "../../provider-workspace/catalog";
import {
  ConfigConnectionsSection,
  ConfigDiscoverySection,
  ConfigEditLink,
  ConfigGeneralSection,
  ConfigOverridesSection,
  ConfigPacingSection,
  ConfigCodexSection,
} from "./config-sections";
import { useConfigurationDraft } from "./use-configuration-draft";

type Section = "general" | "connections" | "discovery" | "pacing";
type PacingRuntime = { currentQueue: number; untilNextSlotMs: number; lastModel?: string };

export default function ProviderConfiguration({
  item, availableModels = [], onUpdateProvider, onDirtyChange, onRegisterSave, onNotice, apiLane, apiBase,
}: {
  item: WorkspaceItem;
  availableModels?: string[];
  onUpdateProvider?: (name: string, patch: ProviderUpdatePatch) => Promise<{ ok: boolean; error?: string }>;
  onDirtyChange?: (dirty: boolean) => void;
  onRegisterSave?: (fn: (() => Promise<boolean>) | null) => void;
  onNotice?: (message: string, ok?: boolean) => void;
  apiLane?: WorkspaceProvider;
  apiBase?: string;
}) {
  const t = useT();
  const [saving, setSaving] = useState(false);
  const [pacingRuntime, setPacingRuntime] = useState<PacingRuntime | null>(null);
  const draft = useConfigurationDraft(item, apiLane);
  const {
    openaiLogical, pacingSource, pacingName, discoverySupported, savedLiveModels,
    adapter, setAdapter, baseUrl, setBaseUrl, defaultModel, setDefaultModel, note, setNote,
    allowPrivateNetwork, setAllowPrivateNetwork, liveModels, setLiveModels,
    pacingEnabled, setPacingEnabled, pacingRpm, setPacingRpm, pacingDelay, setPacingDelay,
    pacingModels, setPacingModels,
    overrideId, overrideRpm, overrideDelay, addingOverride,
    setOverrideId, setOverrideRpm, setOverrideDelay, setAddingOverride, clearOverrideDraft,
    editing, setEditing, error, setError, resetDrafts,
  } = draft;
  const savedPacing = pacingFromItem(pacingSource);
  const preset = isCatalogProviderId(item.name);
  const modelOptions = useMemo(
    () => configurationModelOptions(availableModels, item.defaultModel, defaultModel),
    [availableModels, defaultModel, item.defaultModel],
  );
  const canEdit = Boolean(onUpdateProvider);
  const overrideDraft = configurationOverrideDraft(overrideId, overrideRpm, overrideDelay);
  const dirty = editing !== null || addingOverride || overrideDraft;

  const fail = (message: string) => {
    setError(null);
    if (onNotice) onNotice(message, false);
    else setError(message);
  };

  useEffect(() => {
    onDirtyChange?.(dirty);
    return () => onDirtyChange?.(false);
  }, [dirty, onDirtyChange]);

  useEffect(() => {
    if (!apiBase || !openaiLogical) return;
    let cancelled = false;
    const load = async () => {
      try {
        const response = await fetch(`${apiBase}/api/provider-request-pacing?name=${encodeURIComponent(pacingName)}`);
        if (!response.ok) throw new Error(String(response.status));
        const data = await response.json() as { runtime?: Record<string, PacingRuntime> };
        if (!cancelled) setPacingRuntime(data.runtime?.[pacingName] ?? null);
      } catch {
        if (!cancelled) setPacingRuntime(null);
      }
    };
    void load();
    const interval = window.setInterval(() => { void load(); }, 5_000);
    return () => {
      cancelled = true;
      window.clearInterval(interval);
    };
  }, [apiBase, openaiLogical, pacingName]);

  const patch = async (body: ProviderUpdatePatch, name = item.name): Promise<boolean> => {
    if (!onUpdateProvider) return false;
    setSaving(true);
    setError(null);
    try {
      const res = await onUpdateProvider(name, body);
      if (res.ok) {
        setEditing(null);
        setOverrideId("");
        setOverrideRpm("");
        setOverrideDelay("");
        setAddingOverride(false);
        return true;
      }
      fail(res.error || t("prov.saveFailed"));
      return false;
    } finally {
      setSaving(false);
    }
  };

  const pacingPayload = (models: Record<string, ConfigPacingRule> = pacingModels) => configPacingPayload({
    enabled: pacingEnabled,
    rpm: pacingRpm,
    delay: pacingDelay,
    models,
  });

  const saveGeneral = () => patch(configGeneralPatch({
    openaiLogical,
    adapter,
    itemAdapter: item.adapter,
    baseUrl,
    itemBaseUrl: item.baseUrl,
    defaultModel,
    note,
  }));
  const saveDiscovery = () => patch(configDiscoveryPatch({
    allowPrivateNetwork,
    discoverySupported,
    liveModels,
    savedLiveModels,
  }));
  const savePacing = async (): Promise<boolean> => {
    const body = pacingPayload();
    if (configPacingMissingRule(body)) {
      fail(t("pws.pacingRuleRequired"));
      return false;
    }
    return patch({ requestPacing: body }, pacingName);
  };

  const addOverride = async (): Promise<boolean> => {
    const id = overrideId.trim();
    const rule = configOverrideRule(overrideRpm, overrideDelay);
    if (!id || !rule) {
      fail(t("pws.pacingRuleRequired"));
      return false;
    }
    const next = { ...pacingModels, [id]: rule };
    setPacingModels(next);
    return patch({ requestPacing: pacingPayload(next) }, pacingName);
  };

  const saveOpen = async (): Promise<boolean> => {
    if (editing === "general") return saveGeneral();
    if (editing === "discovery") return saveDiscovery();
    if (editing === "pacing") return savePacing();
    if (addingOverride || overrideDraft) return addOverride();
    return true;
  };

  const saveRef = useRef(saveOpen);
  useEffect(() => {
    saveRef.current = saveOpen;
  });
  useEffect(() => {
    if (!onRegisterSave) return;
    onRegisterSave(() => saveRef.current());
    return () => onRegisterSave(null);
  }, [onRegisterSave]);

  const removeOverride = (model: string) => {
    const next = { ...pacingModels };
    delete next[model];
    setPacingModels(next);
    void patch({ requestPacing: pacingPayload(next) }, pacingName);
  };

  const editLink = (section: Section) => (
    <ConfigEditLink
      section={section}
      editing={editing}
      saving={saving}
      canEdit={canEdit}
      onEdit={next => {
        if (editing === next) void saveOpen();
        else { setError(null); setEditing(next); }
      }}
      onCancel={resetDrafts}
    />
  );

  const privateState = configOnOff(t, item.allowPrivateNetwork === true);
  const discoveryState = configOnOff(t, savedLiveModels);
  const pacingState = configOnOff(t, savedPacing.enabled);
  const overrideEntries = Object.entries(savedPacing.models);

  return (
    <div className="providers-config">
      {!onNotice && error && <p className="providers-config-error">{error}</p>}
      <ConfigGeneralSection
        itemName={item.name}
        adapter={adapter}
        baseUrl={baseUrl}
        defaultModel={defaultModel}
        note={note}
        itemAdapter={item.adapter}
        itemBaseUrl={item.baseUrl}
        itemDefaultModel={item.defaultModel}
        itemNote={item.note}
        openaiLogical={openaiLogical}
        preset={preset}
        editing={editing === "general"}
        modelOptions={modelOptions}
        editLink={editLink("general")}
        onAdapter={setAdapter}
        onBaseUrl={setBaseUrl}
        onDefaultModel={setDefaultModel}
        onNote={setNote}
      />
      {openaiLogical && <ConfigConnectionsSection apiLaneBaseUrl={apiLane?.baseUrl} />}
      <ConfigDiscoverySection
        openaiLogical={openaiLogical}
        editing={editing === "discovery"}
        privateState={privateState}
        discoveryState={discoveryState}
        allowPrivateNetwork={allowPrivateNetwork}
        liveModels={liveModels}
        discoverySupported={discoverySupported}
        editLink={editLink("discovery")}
        onAllowPrivateNetwork={setAllowPrivateNetwork}
        onLiveModels={setLiveModels}
      />
      <ConfigPacingSection
        openaiLogical={openaiLogical}
        editing={editing === "pacing"}
        pacingState={pacingState}
        savedPacing={savedPacing}
        pacingEnabled={pacingEnabled}
        pacingRpm={pacingRpm}
        pacingDelay={pacingDelay}
        pacingRuntime={pacingRuntime}
        editLink={editLink("pacing")}
        onEnabled={setPacingEnabled}
        onRpm={setPacingRpm}
        onDelay={setPacingDelay}
      />
      <ConfigOverridesSection
        itemName={item.name}
        openaiLogical={openaiLogical}
        canEdit={canEdit}
        saving={saving}
        addingOverride={addingOverride}
        overrideId={overrideId}
        overrideRpm={overrideRpm}
        overrideDelay={overrideDelay}
        overrideEntries={overrideEntries}
        availableModels={availableModels}
        onStartAdd={() => { setError(null); setAddingOverride(true); }}
        onAdd={() => { void addOverride(); }}
        onOverrideId={setOverrideId}
        onOverrideRpm={setOverrideRpm}
        onOverrideDelay={setOverrideDelay}
        onCancelAdd={clearOverrideDraft}
        onRemove={removeOverride}
      />
      {openaiLogical && apiBase && <ConfigCodexSection apiBase={apiBase} />}
      <div className="providers-config-footer">
        <button type="button" className="providers-link" onClick={() => navigateHash("models")}>{t("prov.config.viewModels")}</button>
        <button type="button" className="providers-link" onClick={() => navigateHash("models/routing")}>{t("prov.downstream.viewRouting")}</button>
        <button type="button" className="providers-link" onClick={() => navigateHash("logs")}>{t("prov.config.viewLogs")}</button>
      </div>
    </div>
  );
}
