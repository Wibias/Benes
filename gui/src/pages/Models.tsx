import { useCallback, useEffect, useMemo, useRef, useState } from "react";

import { setClientResourceData } from "../client-resource";
import { useDataSurface } from "../data-surface";
import { readJsonOrThrow } from "../fetch-json";
import { useT } from "../i18n/shared";
import { fetchCodexAppServerState, type AppServerStateOutcome } from "../listener-commands";
import { modelVisible, shouldApplyLoadGeneration, type ModelDiscoveryState, type ProviderModelMap } from "../model-visibility";
import { postModelDiscoverySync } from "../model-discovery-sync";
import { applyProviderModelContextWindows, buildProviderModelGroups, type ModelContextWindowPatch } from "../models-groups";
import { formatProviderDisplayName } from "../provider-icons";
import { readSessionListCache, writeSessionListCache } from "../session-list-cache";
import { ToastNotice } from "../ui";
import { useCodexRestart } from "../use-codex-restart";
import {
  catalogFooterCounts,
  filterCatalogGroups,
  gpt56Family,
  isAdvertisedContextSelection,
  providerContextBulkWindows,
  type CatalogVisibilityFilter,
} from "./models-catalog-filter";
import { ModelsCatalogToolbar, ModelsCollapseControls } from "./models-catalog-controls";
import { ModelsCustomModelModal } from "./models-catalog-modals";
import { ModelsCatalogPanel } from "./models-catalog-panel";
import { ModelsCatalogSettings } from "./models-catalog-settings";
import {
  modelsCatalogMutationStable,
  shouldApplyModelsCatalogLoad,
} from "./models-change-sync";
import {
  fetchModelsCatalogSnapshot,
  planCatalogApplication,
  type ModelsCatalogSnapshot,
} from "./models-page-orchestration";
import { ModelsPageShell } from "./models-page-shell";
import { ModelsProviderGroup } from "./models-provider-group";
import {
  CUSTOM_OPTION,
  NATIVE_GPT56_DEFAULT_WINDOW,
  PAGE,
  readCollapsedProviders,
  REASONING_EFFORT_LEVELS,
  writeCollapsedProviders,
  type ModelRow,
} from "./models-shared";
import {
  useModelsChangeWrites,
  type ModelsCatalogMutationPatch,
} from "./use-models-change-writes";

const DEFAULT_CONTEXT_CAP = 350_000;
const EMPTY_DISCOVERY: ModelDiscoveryState = { newModelPolicy: "on", providers: {} };
const EMPTY_SELECTION: ProviderModelMap = {};

type ModelsProps = {
  apiBase: string;
  restartEpoch?: number;
};

type CustomEditorState = {
  open: boolean;
  mode: "add" | "edit";
  provider: string;
  customId: string;
  modelId: string;
  displayName: string;
  contextWindow: string;
  customContext: boolean;
  modalities: string[];
  reasoning: boolean;
  reasoningEfforts: string[];
  saving: boolean;
  error: string;
};

function blankCustomEditor(): CustomEditorState {
  return {
    open: false,
    mode: "add",
    provider: "",
    customId: "",
    modelId: "",
    displayName: "",
    contextWindow: "",
    customContext: false,
    modalities: ["text"],
    reasoning: false,
    reasoningEfforts: [],
    saving: false,
    error: "",
  };
}

function feedbackTone(success: boolean): "ok" | "err" {
  return success ? "ok" : "err";
}

function useModelsAppServerControl(apiBase: string, restartEpoch: number) {
  const [state, setState] = useState<AppServerStateOutcome["state"]>(null);
  const mounted = useRef(true);

  useEffect(() => {
    mounted.current = true;
    return () => {
      mounted.current = false;
    };
  }, []);

  const refresh = useCallback((signal?: AbortSignal) => {
    void fetchCodexAppServerState(apiBase, { signal }).then(result => {
      if (signal?.aborted || !mounted.current) return;
      setState(result.state);
    });
  }, [apiBase]);

  const restartControl = useCodexRestart(apiBase, { onSettled: () => refresh() });

  useEffect(() => {
    const request = new AbortController();
    refresh(request.signal);
    return () => request.abort();
  }, [refresh, restartEpoch]);

  return {
    state,
    restarting: restartControl.restarting,
    restart: restartControl.restart,
  };
}

function positiveInteger(raw: string): number | null {
  const value = Number(raw.replace(/[_,\s]/g, ""));
  return Number.isSafeInteger(value) && value > 0 ? value : null;
}

export default function Models({ apiBase, restartEpoch = 0 }: ModelsProps) {
  const t = useT();
  const appServer = useModelsAppServerControl(apiBase, restartEpoch);
  const cacheKey = `benes.models.catalog.v2:${apiBase}`;
  const cached = useMemo(
    () => readSessionListCache<ModelsCatalogSnapshot>(cacheKey),
    [cacheKey],
  );

  const [catalogModels, setCatalogModels] = useState<ModelRow[]>(() => cached?.models ?? []);
  const [catalogProviders, setCatalogProviders] = useState(() => cached?.providers ?? []);
  const [disabledNames, setDisabledNames] = useState<Set<string>>(
    () => new Set(cached?.disabled ?? []),
  );
  const [selectionMap, setSelectionMap] = useState<ProviderModelMap | null>(
    () => cached?.selectedModels ?? null,
  );
  const [discoveryState, setDiscoveryState] = useState<ModelDiscoveryState>(
    () => cached?.discovery ?? EMPTY_DISCOVERY,
  );
  const [contextCaps, setContextCaps] = useState<Record<string, number>>(
    () => cached?.contextCaps ?? {},
  );
  const [contextCapValue, setContextCapValue] = useState(
    () => cached?.contextCapValue ?? DEFAULT_CONTEXT_CAP,
  );

  const [catalogQuery, setCatalogQuery] = useState("");
  const [visibilityFilter, setVisibilityFilter] = useState<CatalogVisibilityFilter>("all");
  const [selectedProvider, setSelectedProvider] = useState<string | null>(null);
  const selectedProviderRef = useRef<string | null>(null);
  const [rowLimits, setRowLimits] = useState<Record<string, number>>({});
  const [showCustomCap, setShowCustomCap] = useState(false);
  const [customCapInput, setCustomCapInput] = useState("");

  const [storedCollapse] = useState(() => readCollapsedProviders());
  const [collapsedProviders, setCollapsedProviders] = useState<Set<string>>(
    () => storedCollapse ?? new Set(),
  );
  const needsDefaultCollapseRef = useRef(storedCollapse === null);

  const [status, setStatus] = useState("");
  const [ok, setOk] = useState(false);
  const [feedbackGeneration, setFeedbackGeneration] = useState(0);
  const publishFeedback = useCallback((success: boolean, message: string) => {
    setOk(success);
    setStatus(message);
    setFeedbackGeneration(value => value + 1);
  }, []);

  useEffect(() => {
    if (!status) return;
    const timer = setTimeout(() => setStatus(""), ok ? 6_000 : 8_000);
    return () => clearTimeout(timer);
  }, [feedbackGeneration, ok, status]);

  const [busy, setBusy] = useState(false);
  const busyRef = useRef(false);
  const loadGenerationRef = useRef(0);
  const loadPendingRef = useRef(false);
  const catalogMutationEpochRef = useRef(0);
  const catalogMutationPendingRef = useRef(0);
  const catalogSnapshotRef = useRef<ModelsCatalogSnapshot | null>(cached ?? null);
  const pendingContextWritesRef = useRef<ModelContextWindowPatch[]>([]);
  const disabledNamesRef = useRef<string[]>([...(cached?.disabled ?? [])]);
  const selectionMapRef = useRef<ProviderModelMap>(cached?.selectedModels ?? EMPTY_SELECTION);

  const [customEditor, setCustomEditor] = useState<CustomEditorState>(blankCustomEditor);
  const customReasoningSeededRef = useRef(false);

  const chooseProvider = useCallback((provider: string | null) => {
    selectedProviderRef.current = provider;
    setSelectedProvider(provider);
  }, []);

  const fetchCatalog = useCallback(
    (signal: AbortSignal) => fetchModelsCatalogSnapshot(apiBase, signal),
    [apiBase],
  );

  const applyCatalog = useCallback((next: ModelsCatalogSnapshot) => {
    catalogSnapshotRef.current = next;
    writeSessionListCache(cacheKey, next);

    const plan = planCatalogApplication({
      models: next.models,
      providers: next.providers,
      selectedProvider: selectedProviderRef.current,
      pendingContextWrites: pendingContextWritesRef.current,
    });

    pendingContextWritesRef.current = plan.pendingContextWrites;
    if (plan.selectedProvider !== selectedProviderRef.current) {
      chooseProvider(plan.selectedProvider);
    }

    disabledNamesRef.current = next.disabled;
    selectionMapRef.current = next.selectedModels;
    setCatalogModels(next.models);
    setCatalogProviders(plan.providers);
    setDisabledNames(new Set(next.disabled));
    setSelectionMap(next.selectedModels);
    setDiscoveryState(next.discovery);
    setContextCaps(next.contextCaps);
    setContextCapValue(next.contextCapValue);

    for (const patch of plan.staleContextWrites) {
      void fetch(`${apiBase}/api/providers?name=${encodeURIComponent(patch.provider)}`, {
        method: "PATCH",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ modelContextWindows: patch.windows }),
      });
    }
  }, [apiBase, cacheKey, chooseProvider]);

  const catalogResource = useDataSurface<ModelsCatalogSnapshot>(
    cacheKey,
    [apiBase],
    async signal => {
      const epochAtStart = catalogMutationEpochRef.current;
      const snapshot = await fetchCatalog(signal);
      if (signal.aborted) throw new Error("models request aborted");

      const stable = modelsCatalogMutationStable(
        epochAtStart,
        catalogMutationEpochRef.current,
        catalogMutationPendingRef.current,
      );
      if (!stable) return catalogSnapshotRef.current ?? snapshot;

      applyCatalog(snapshot);
      return snapshot;
    },
    {
      isEmpty: () => false,
      pollMs: 10_000,
      initialData: cached ?? undefined,
      enabled: true,
      deadlineMs: 60_000,
    },
  );
  const catalogState = catalogResource.state;

  const publishCatalogMutation = useCallback((patch: ModelsCatalogMutationPatch) => {
    catalogMutationPendingRef.current += 1;
    catalogMutationEpochRef.current += 1;
    const current = catalogSnapshotRef.current;
    if (!current) return;

    const next = { ...current, ...patch };
    catalogSnapshotRef.current = next;
    writeSessionListCache(cacheKey, next);
    setClientResourceData(cacheKey, next);
  }, [cacheKey]);

  const settleCatalogMutation = useCallback(() => {
    if (catalogMutationPendingRef.current > 0) {
      catalogMutationPendingRef.current -= 1;
    }
    catalogMutationEpochRef.current += 1;
  }, []);

  const load = useCallback(async (force = false): Promise<boolean> => {
    if (loadPendingRef.current && !force) return false;
    loadPendingRef.current = true;

    const epochAtStart = catalogMutationEpochRef.current;
    const generation = loadGenerationRef.current + 1;
    loadGenerationRef.current = generation;

    try {
      const next = await fetchCatalog(new AbortController().signal);
      const accepted = shouldApplyModelsCatalogLoad(
        generation,
        loadGenerationRef.current,
        epochAtStart,
        catalogMutationEpochRef.current,
        catalogMutationPendingRef.current,
      );
      if (!accepted) return false;

      applyCatalog(next);
      setClientResourceData(cacheKey, next);
      return true;
    } catch {
      return false;
    } finally {
      if (shouldApplyLoadGeneration(generation, loadGenerationRef.current)) {
        loadPendingRef.current = false;
      }
    }
  }, [applyCatalog, cacheKey, fetchCatalog]);

  const {
    applyVisibility,
    applyNewModelPolicy,
    putCap,
    cancelPendingSync,
  } = useModelsChangeWrites({
    apiBase,
    t,
    load,
    setBusy,
    busyRef,
    setDisabled: setDisabledNames,
    setSelectedModels: setSelectionMap,
    discovery: discoveryState,
    setDiscovery: setDiscoveryState,
    setContextCapValue,
    setContextCaps,
    setStatus,
    setOk,
    disabledRef: disabledNamesRef,
    selectedModelsRef: selectionMapRef,
    publishCatalogMutation,
    settleCatalogMutation,
  });

  const providerGroups = useMemo(
    () => buildProviderModelGroups(catalogModels, catalogProviders),
    [catalogModels, catalogProviders],
  );

  useEffect(() => {
    if (!needsDefaultCollapseRef.current || providerGroups.length === 0) return;
    needsDefaultCollapseRef.current = false;
    const everyProvider = new Set(providerGroups.map(group => group.provider));
    // eslint-disable-next-line react-hooks/set-state-in-effect, react/react-compiler
    setCollapsedProviders(everyProvider);
    writeCollapsedProviders(everyProvider);
  }, [providerGroups]);

  const selectedModelMap = selectionMap ?? EMPTY_SELECTION;
  const isVisible = useCallback((provider: string, model: ModelRow) => modelVisible(
    selectedModelMap,
    provider,
    model.id,
    model.native === true,
    disabledNames.has(model.namespaced),
  ), [disabledNames, selectedModelMap]);

  const visibleGroups = useMemo(() => filterCatalogGroups(providerGroups, {
    provider: selectedProvider,
    query: catalogQuery,
    visibility: visibilityFilter,
    providerLabel: provider => formatProviderDisplayName(provider, t),
    isVisible,
  }), [catalogQuery, isVisible, providerGroups, selectedProvider, t, visibilityFilter]);

  const footer = catalogFooterCounts(visibleGroups, isVisible);

  const arrivalNotes = useMemo(() => {
    const notes: Record<string, string> = {};
    for (const [provider, summary] of Object.entries(discoveryState.providers)) {
      const count = summary.arrivals.filter(arrival => arrival.state === "auto-disabled").length;
      if (count > 0) notes[provider] = t("models.newArrivalsCount", { n: count });
    }
    return notes;
  }, [discoveryState, t]);

  const syncProviderModels = useCallback(async (provider: string) => {
    if (busyRef.current) return;
    busyRef.current = true;
    setBusy(true);
    cancelPendingSync();

    try {
      const result = await postModelDiscoverySync(apiBase, provider);
      await load(true);
      if (!result.ok) {
        publishFeedback(
          false,
          result.applicable === false
            ? t("models.emptyDiscoveryDisabled")
            : (result.error || t("prov.networkError")),
        );
        return;
      }
      publishFeedback(true, t("prov.health.models"));
    } catch {
      publishFeedback(false, t("prov.networkError"));
    } finally {
      busyRef.current = false;
      setBusy(false);
    }
  }, [apiBase, cancelPendingSync, load, publishFeedback, t]);

  const refreshCatalog = useCallback(async () => {
    cancelPendingSync();
    const refreshed = await load(true);
    publishFeedback(
      refreshed,
      t(refreshed ? "models.catalogRefreshed" : "models.catalogRefreshFailed"),
    );
  }, [cancelPendingSync, load, publishFeedback, t]);

  const toggleProviderCollapse = (provider: string) => {
    setCollapsedProviders(current => {
      const next = new Set(current);
      if (next.has(provider)) next.delete(provider);
      else next.add(provider);
      writeCollapsedProviders(next);
      return next;
    });
  };

  const setEveryProviderCollapsed = (collapse: boolean) => {
    const next = collapse
      ? new Set(providerGroups.map(group => group.provider))
      : new Set<string>();
    setCollapsedProviders(next);
    writeCollapsedProviders(next);
  };

  const allCapped = useMemo(() => {
    const routed = providerGroups.filter(group => !group.native);
    if (routed.length === 0) return false;
    if (routed.some(group => contextCaps[group.provider] !== contextCapValue)) return false;
    return Object.values(contextCaps).every(value => value === contextCapValue);
  }, [contextCapValue, contextCaps, providerGroups]);

  const setGlobalCap = (value: number) => {
    if (!Number.isSafeInteger(value) || value <= 0) return;
    void putCap(allCapped ? { value, setAll: true } : { value });
  };

  const selectGlobalCap = (raw: string) => {
    if (raw === CUSTOM_OPTION) {
      setShowCustomCap(true);
      setCustomCapInput(String(contextCapValue));
      return;
    }
    setShowCustomCap(false);
    const value = Number(raw);
    if (Number.isSafeInteger(value) && value > 0 && value !== contextCapValue) {
      setGlobalCap(value);
    }
  };

  const applyCustomCap = () => {
    const value = positiveInteger(customCapInput);
    if (value === null) {
      publishFeedback(false, t("models.capSaveFailed"));
      return;
    }
    setShowCustomCap(false);
    setGlobalCap(value);
  };

  const saveModelContexts = async (
    provider: string,
    windows: Record<string, number | null>,
  ) => {
    const pending: ModelContextWindowPatch = { provider, windows };
    pendingContextWritesRef.current = [...pendingContextWritesRef.current, pending];
    setCatalogProviders(current => applyProviderModelContextWindows(current, provider, windows));

    try {
      const response = await fetch(
        `${apiBase}/api/providers?name=${encodeURIComponent(provider)}`,
        {
          method: "PATCH",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ modelContextWindows: windows }),
        },
      );
      await readJsonOrThrow(response, t("models.contextSaveFailed"));
    } catch (error) {
      pendingContextWritesRef.current = pendingContextWritesRef.current.filter(
        candidate => candidate !== pending,
      );
      publishFeedback(
        false,
        error instanceof Error ? error.message : t("models.contextSaveFailed"),
      );
      await load(true);
      return;
    }

    publishFeedback(true, t("models.contextSaved"));
  };

  const selectModelContext = (provider: string, modelId: string, raw: string) => {
    const group = providerGroups.find(candidate => candidate.provider === provider);
    const current = group?.modelContextWindows?.[modelId];
    const model = group?.rows.find(candidate => candidate.id === modelId);
    const advertised = typeof model?.contextWindow === "number" && model.contextWindow > 0
      ? model.contextWindow
      : 0;
    const nativeFloor = group?.nativeProviderGroup && gpt56Family(modelId)
      ? NATIVE_GPT56_DEFAULT_WINDOW
      : 0;
    const maximum = Math.max(advertised, nativeFloor);

    if (isAdvertisedContextSelection(raw, maximum)) {
      if (current != null) void saveModelContexts(provider, { [modelId]: null });
      return;
    }

    const value = Number(raw);
    if (!Number.isSafeInteger(value) || value <= 0 || value === current) return;
    void saveModelContexts(provider, { [modelId]: value });
  };

  const selectProviderContext = (provider: string, raw: string) => {
    const group = providerGroups.find(candidate => candidate.provider === provider);
    if (!group) return;
    const patch = providerContextBulkWindows(raw, group.rows.map(row => row.id));
    if (patch) void saveModelContexts(provider, patch);
  };

  const patchCustomEditor = (patch: Partial<CustomEditorState>) => {
    setCustomEditor(current => ({ ...current, ...patch }));
  };

  const beginCustomAdd = (provider: string) => {
    customReasoningSeededRef.current = false;
    setCustomEditor({
      ...blankCustomEditor(),
      open: true,
      provider,
    });
  };

  const beginCustomEdit = (model: ModelRow) => {
    const hasReasoningOverride = Array.isArray(model.reasoningEfforts);
    customReasoningSeededRef.current = hasReasoningOverride;
    setCustomEditor({
      open: true,
      mode: "edit",
      provider: model.provider,
      customId: model.customId ?? "",
      modelId: model.id,
      displayName: model.displayName ?? "",
      contextWindow: model.contextWindow ? String(model.contextWindow) : "",
      customContext: false,
      modalities: model.inputModalities ?? ["text"],
      reasoning: hasReasoningOverride,
      reasoningEfforts: model.reasoningEfforts ?? [],
      saving: false,
      error: "",
    });
  };

  const finishCustomSave = async (
    request: () => Promise<Response>,
    successMessage: string,
  ) => {
    patchCustomEditor({ saving: true, error: "" });
    let response: Response;
    try {
      response = await request();
    } catch {
      patchCustomEditor({ saving: false, error: t("models.networkError") });
      return;
    }

    try {
      await readJsonOrThrow(response, t("models.customSaveFailed"));
    } catch (error) {
      patchCustomEditor({
        saving: false,
        error: error instanceof Error ? error.message : t("models.customSaveFailed"),
      });
      return;
    }

    patchCustomEditor({ open: false, saving: false });
    publishFeedback(true, successMessage);
    await load(true);
  };

  const addCustomModel = async (
    provider: string,
    modelId: string,
    displayName?: string,
    contextWindow?: number,
    inputModalities?: string[],
    reasoningEfforts?: string[],
  ) => finishCustomSave(
    () => fetch(`${apiBase}/api/custom-models`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        provider,
        modelId,
        displayName,
        contextWindow,
        inputModalities,
        reasoningEfforts,
      }),
    }),
    t("models.customAdded"),
  );

  const updateCustomModel = async (id: string, patch: Record<string, unknown>) => finishCustomSave(
    () => fetch(`${apiBase}/api/custom-models/${encodeURIComponent(id)}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(patch),
    }),
    t("models.customUpdated"),
  );

  const deleteCustomModel = async (id: string) => {
    try {
      const response = await fetch(
        `${apiBase}/api/custom-models/${encodeURIComponent(id)}`,
        { method: "DELETE" },
      );
      if (!response.ok) {
        publishFeedback(false, t("models.customSaveFailed"));
        return;
      }
      publishFeedback(true, t("models.customDeleted"));
      await load(true);
    } catch {
      publishFeedback(false, t("models.networkError"));
    }
  };

  const confirmCustomDelete = (model: ModelRow) => {
    if (!model.customId) return;
    const confirmed = window.confirm(
      t("models.customDeleteConfirm", { name: model.displayName ?? model.id }),
    );
    if (confirmed) void deleteCustomModel(model.customId);
  };

  const toggleCustomReasoning = (checked: boolean) => {
    patchCustomEditor({ reasoning: checked });
    if (!checked || customReasoningSeededRef.current) return;

    customReasoningSeededRef.current = true;
    const advertised = catalogModels.find(model => (
      model.provider === customEditor.provider && model.id === customEditor.modelId
    ))?.reasoningEfforts;
    patchCustomEditor({
      reasoningEfforts: Array.isArray(advertised)
        ? advertised
        : [...REASONING_EFFORT_LEVELS],
    });
  };

  const submitCustomEditor = () => {
    const modelId = customEditor.modelId.trim();
    const displayName = customEditor.displayName.trim();
    const contextWindow = positiveInteger(customEditor.contextWindow) ?? undefined;

    if (customEditor.mode === "add") {
      void addCustomModel(
        customEditor.provider,
        modelId,
        displayName || undefined,
        contextWindow,
        customEditor.modalities.length > 0 ? customEditor.modalities : undefined,
        customEditor.reasoning ? customEditor.reasoningEfforts : undefined,
      );
      return;
    }

    void updateCustomModel(customEditor.customId, {
      modelId,
      displayName,
      contextWindow: contextWindow ?? null,
      inputModalities: customEditor.modalities,
      reasoningEfforts: customEditor.reasoning ? customEditor.reasoningEfforts : null,
    });
  };

  const catalog = catalogState.data ?? cached;
  const catalogCold = catalogState.showSkeleton && !catalog;
  const catalogColdFailure = catalogState.kind === "failed-cold"
    ? (catalogState.error instanceof Error ? catalogState.error.message : t("models.loadFail"))
    : null;

  const providerList = (
    <>
      {visibleGroups.map(group => (
        <ModelsProviderGroup
          key={group.provider}
          group={group}
          t={t}
          busy={busy}
          collapsed={collapsedProviders}
          selectedModelMap={selectedModelMap}
          disabled={disabledNames}
          contextCaps={contextCaps}
          contextCapValue={contextCapValue}
          limit={rowLimits}
          arrivalNote={arrivalNotes[group.provider]}
          onToggleCollapse={toggleProviderCollapse}
          onAddCustom={beginCustomAdd}
          onEditCustom={beginCustomEdit}
          onDeleteCustom={confirmCustomDelete}
          onApplyVisibility={applyVisibility}
          onSelectProviderContext={selectProviderContext}
          onSelectModelContext={selectModelContext}
          onShowMore={(provider, shown) => {
            setRowLimits(current => ({ ...current, [provider]: shown + PAGE }));
          }}
          onSyncModels={syncProviderModels}
        />
      ))}
    </>
  );

  const toolbar = (
    <ModelsCatalogToolbar
      t={t}
      busy={busy}
      refreshing={catalogState.refreshing}
      query={catalogQuery}
      provider={selectedProvider}
      visibility={visibilityFilter}
      providers={providerGroups.map(group => group.provider)}
      listenForShortcut
      onQueryChange={setCatalogQuery}
      onProviderChange={chooseProvider}
      onVisibilityChange={setVisibilityFilter}
      onRefresh={() => { void refreshCatalog(); }}
    />
  );

  const settings = (
    <ModelsCatalogSettings
      t={t}
      busy={busy}
      newModelPolicy={discoveryState.newModelPolicy === "off" ? "off" : "on"}
      contextCapValue={contextCapValue}
      showCustom={showCustomCap}
      customCap={customCapInput}
      allCapped={allCapped}
      onNewModelPolicy={policy => { void applyNewModelPolicy(policy); }}
      onSelectCap={selectGlobalCap}
      onCustomCapChange={setCustomCapInput}
      onApplyCustomCap={applyCustomCap}
      onSetAll={() => { void putCap({ setAll: !allCapped }); }}
    />
  );

  const modal = (
    <ModelsCustomModelModal
      open={customEditor.open}
      mode={customEditor.mode}
      provider={customEditor.provider}
      t={t}
      error={customEditor.error}
      saving={customEditor.saving}
      modelId={customEditor.modelId}
      displayName={customEditor.displayName}
      contextWindow={customEditor.contextWindow}
      showCustomCtx={customEditor.customContext}
      modalities={customEditor.modalities}
      reasoning={customEditor.reasoning}
      reasoningEfforts={customEditor.reasoningEfforts}
      onClose={() => patchCustomEditor({ open: false })}
      onModelIdChange={modelId => patchCustomEditor({ modelId })}
      onDisplayNameChange={displayName => patchCustomEditor({ displayName })}
      onContextSelect={value => {
        if (value === CUSTOM_OPTION) {
          patchCustomEditor({ customContext: true });
        } else {
          patchCustomEditor({ customContext: false, contextWindow: value });
        }
      }}
      onContextWindowChange={contextWindow => patchCustomEditor({ contextWindow })}
      onToggleModality={(modality, checked) => {
        patchCustomEditor({
          modalities: checked
            ? [...customEditor.modalities, modality]
            : customEditor.modalities.filter(item => item !== modality),
        });
      }}
      onToggleReasoning={toggleCustomReasoning}
      onToggleEffort={(effort, checked) => {
        patchCustomEditor({
          reasoningEfforts: checked
            ? [...customEditor.reasoningEfforts, effort]
            : customEditor.reasoningEfforts.filter(item => item !== effort),
        });
      }}
      onSubmit={submitCustomEditor}
    />
  );

  const catalogPanel = (
    <ModelsCatalogPanel
      t={t}
      showError={catalogState.showError}
      refreshing={catalogState.refreshing}
      empty={visibleGroups.length === 0}
      footer={footer}
      toolbar={toolbar}
      settings={settings}
      collapseControls={(
        <ModelsCollapseControls
          t={t}
          busy={busy}
          onCollapseAll={() => setEveryProviderCollapsed(true)}
          onExpandAll={() => setEveryProviderCollapsed(false)}
        />
      )}
      providerList={providerList}
      modals={modal}
    />
  );

  return (
    <>
      {status && (
        <ToastNotice
          tone={feedbackTone(ok)}
          onDismiss={() => setStatus("")}
          dismissLabel={t("common.close")}
        >
          {status}
        </ToastNotice>
      )}
      <ModelsPageShell
        t={t}
        appServerState={appServer.state}
        codexController={{ restarting: appServer.restarting, restart: appServer.restart }}
        catalogCold={catalogCold}
        catalogColdFailure={catalogColdFailure}
        catalogPanel={catalogPanel}
        onRetryCatalog={() => catalogResource.refresh()}
      />
    </>
  );
}
