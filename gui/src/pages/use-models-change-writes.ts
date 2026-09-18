/** Models catalogue mutation orchestration. */
import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  type Dispatch,
  type SetStateAction,
} from "react";
import type { TFn } from "../i18n/shared";
import { readJsonOrThrow } from "../fetch-json";
import {
  nextDisabledModels,
  nextSelectedAllowlist,
  putModelDiscoveryPolicy,
  putModelVisibility,
  type ModelDiscoveryState,
  type ModelVisibilityScope,
  type ModelVisibilityTarget,
  type ProviderModelMap,
} from "../model-visibility";
import {
  createTrailingDebounce,
  MODELS_CHANGE_SYNC_DEBOUNCE_MS,
} from "./models-change-sync";
import type { ProviderContextCapsResponse } from "./models-shared";

export type ModelsCatalogMutationPatch = {
  disabled?: string[];
  selectedModels?: ProviderModelMap;
  discovery?: ModelDiscoveryState;
  contextCaps?: Record<string, number>;
  contextCapValue?: number;
};

type MutableValue<T> = { current: T };
type SaveOutcome = "saved" | "rejected" | "network-error";

type ModelsChangeWritesOptions = {
  apiBase: string;
  t: TFn;
  load: (force?: boolean) => Promise<boolean>;
  setBusy: (busy: boolean) => void;
  busyRef: MutableValue<boolean>;
  setDisabled: (next: Set<string>) => void;
  setSelectedModels: (next: ProviderModelMap) => void;
  discovery: ModelDiscoveryState;
  setDiscovery: Dispatch<SetStateAction<ModelDiscoveryState>>;
  setContextCapValue: (value: number) => void;
  setContextCaps: (caps: Record<string, number>) => void;
  setStatus: (status: string) => void;
  setOk: (ok: boolean) => void;
  disabledRef: MutableValue<string[]>;
  selectedModelsRef: MutableValue<ProviderModelMap>;
  publishCatalogMutation: (patch: ModelsCatalogMutationPatch) => void;
  settleCatalogMutation: () => void;
};

async function saveOutcome(request: () => Promise<Response>): Promise<SaveOutcome> {
  try {
    return (await request()).ok ? "saved" : "rejected";
  } catch {
    return "network-error";
  }
}

function setBusyState(
  value: boolean,
  setBusy: (busy: boolean) => void,
  busyRef: MutableValue<boolean>,
): void {
  setBusy(value);
  busyRef.current = value;
}

function useCatalogSync(load: (force?: boolean) => Promise<boolean>) {
  const debounce = useMemo(
    () => createTrailingDebounce(MODELS_CHANGE_SYNC_DEBOUNCE_MS),
    [],
  );

  useEffect(() => () => debounce.cancel(), [debounce]);

  const cancel = useCallback(() => {
    debounce.cancel();
  }, [debounce]);

  const schedule = useCallback(() => {
    debounce.schedule(() => {
      void load(true);
    });
  }, [debounce, load]);

  return { cancel, schedule };
}

function useSerialQueue() {
  const tail = useRef<Promise<void> | null>(null);

  return useCallback((task: () => Promise<void>): Promise<void> => {
    const previous = tail.current ?? Promise.resolve();
    const next = previous.catch(() => undefined).then(task);
    tail.current = next;
    return next;
  }, []);
}

export function useModelsChangeWrites({
  apiBase,
  t,
  load,
  setBusy,
  busyRef,
  setDisabled,
  setSelectedModels,
  discovery,
  setDiscovery,
  setContextCapValue,
  setContextCaps,
  setStatus,
  setOk,
  disabledRef,
  selectedModelsRef,
  publishCatalogMutation,
  settleCatalogMutation,
}: ModelsChangeWritesOptions) {
  const catalogSync = useCatalogSync(load);
  const enqueueVisibility = useSerialQueue();

  const applyVisibility = useCallback((
    scope: ModelVisibilityScope,
    provider: string,
    targets: ModelVisibilityTarget[],
    enabled: boolean,
  ) => {
    const previousDisabled = disabledRef.current;
    const previousSelected = selectedModelsRef.current;
    const optimisticDisabled = nextDisabledModels(
      previousDisabled,
      provider,
      targets,
      enabled,
    );
    const selectedPatch = nextSelectedAllowlist(
      previousSelected,
      scope,
      provider,
      targets,
      enabled,
    );

    let optimisticSelected = previousSelected;
    disabledRef.current = optimisticDisabled;
    setDisabled(new Set(optimisticDisabled));

    if (selectedPatch !== undefined) {
      optimisticSelected = { ...previousSelected, [provider]: selectedPatch };
      selectedModelsRef.current = optimisticSelected;
      setSelectedModels(optimisticSelected);
    }

    publishCatalogMutation({
      disabled: optimisticDisabled,
      selectedModels: optimisticSelected,
    });

    return enqueueVisibility(async () => {
      setStatus("");
      const outcome = await saveOutcome(() => putModelVisibility(apiBase, {
        scope,
        provider,
        targets,
        enabled,
        disabled: previousDisabled,
        selected: previousSelected,
      }));

      settleCatalogMutation();
      if (outcome !== "saved") {
        catalogSync.cancel();
        await load(true);
        setOk(false);
        setStatus(t(outcome === "rejected" ? "models.saveFailed" : "models.networkError"));
        return;
      }

      setOk(true);
      setStatus(t("models.applied"));
      catalogSync.schedule();
    });
  }, [
    apiBase,
    catalogSync,
    disabledRef,
    enqueueVisibility,
    load,
    publishCatalogMutation,
    selectedModelsRef,
    setDisabled,
    setOk,
    setSelectedModels,
    setStatus,
    settleCatalogMutation,
    t,
  ]);

  const applyNewModelPolicy = useCallback(async (policy: "on" | "off") => {
    const optimisticDiscovery = { ...discovery, newModelPolicy: policy };
    setDiscovery(optimisticDiscovery);
    publishCatalogMutation({ discovery: optimisticDiscovery });
    setBusyState(true, setBusy, busyRef);
    setStatus("");

    const outcome = await saveOutcome(() => putModelDiscoveryPolicy(apiBase, policy));
    settleCatalogMutation();

    if (outcome !== "saved") {
      catalogSync.cancel();
      await load(true);
      setOk(false);
      setStatus(t(outcome === "rejected" ? "models.saveFailed" : "models.networkError"));
    } else {
      setOk(true);
      setStatus(t("models.newPolicyApplied"));
      catalogSync.schedule();
    }

    setBusyState(false, setBusy, busyRef);
  }, [
    apiBase,
    busyRef,
    catalogSync,
    discovery,
    load,
    publishCatalogMutation,
    setBusy,
    setDiscovery,
    setOk,
    setStatus,
    settleCatalogMutation,
    t,
  ]);

  const putCap = useCallback(async (body: Record<string, unknown>) => {
    setBusyState(true, setBusy, busyRef);
    setStatus("");

    try {
      const response = await fetch(`${apiBase}/api/provider-context-caps`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(body),
      });

      try {
        const data = await readJsonOrThrow<ProviderContextCapsResponse>(
          response,
          t("models.capSaveFailed"),
        );
        const nextCaps = data?.caps ?? {};
        const patch: ModelsCatalogMutationPatch = { contextCaps: nextCaps };

        if (typeof data?.value === "number" && Number.isFinite(data.value) && data.value > 0) {
          patch.contextCapValue = data.value;
          setContextCapValue(data.value);
        }

        setContextCaps(nextCaps);
        publishCatalogMutation(patch);
        settleCatalogMutation();
        setOk(true);
        setStatus(t("models.capApplied"));
        catalogSync.schedule();
      } catch (error) {
        catalogSync.cancel();
        setOk(false);
        setStatus(error instanceof Error ? error.message : t("models.capSaveFailed"));
        await load(true);
      }
    } catch {
      catalogSync.cancel();
      setOk(false);
      setStatus(t("models.networkError"));
    } finally {
      setBusyState(false, setBusy, busyRef);
    }
  }, [
    apiBase,
    busyRef,
    catalogSync,
    load,
    publishCatalogMutation,
    setBusy,
    setContextCapValue,
    setContextCaps,
    setOk,
    setStatus,
    settleCatalogMutation,
    t,
  ]);

  return {
    applyVisibility,
    applyNewModelPolicy,
    putCap,
    cancelPendingSync: catalogSync.cancel,
  };
}
