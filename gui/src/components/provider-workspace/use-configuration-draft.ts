import { useState } from "react";
import { pacingFromItem } from "../../provider-workspace/config-pacing";
import {
  configurationDraftFields,
  configurationPacingContext,
} from "../../provider-workspace/config-draft";
import type { WorkspaceItem, WorkspaceProvider } from "../../provider-workspace/catalog";
import type { ConfigPacingRule } from "../../provider-workspace/config-pacing";

type OverrideDraft = {
  overrideId: string;
  overrideRpm: string;
  overrideDelay: string;
  addingOverride: boolean;
};

const EMPTY_OVERRIDE: OverrideDraft = {
  overrideId: "",
  overrideRpm: "",
  overrideDelay: "",
  addingOverride: false,
};

export function useConfigurationDraft(item: WorkspaceItem, apiLane?: WorkspaceProvider) {
  const context = configurationPacingContext(item, apiLane);
  const initial = configurationDraftFields(item, pacingFromItem(context.pacingSource), context.discoverySupported);
  const [syncedItem, setSyncedItem] = useState(item);
  const [adapter, setAdapter] = useState(initial.adapter);
  const [baseUrl, setBaseUrl] = useState(initial.baseUrl);
  const [defaultModel, setDefaultModel] = useState(initial.defaultModel);
  const [note, setNote] = useState(initial.note);
  const [allowPrivateNetwork, setAllowPrivateNetwork] = useState(initial.allowPrivateNetwork);
  const [liveModels, setLiveModels] = useState(initial.liveModels);
  const [pacingEnabled, setPacingEnabled] = useState(initial.pacingEnabled);
  const [pacingRpm, setPacingRpm] = useState(initial.pacingRpm);
  const [pacingDelay, setPacingDelay] = useState(initial.pacingDelay);
  const [pacingModels, setPacingModels] = useState<Record<string, ConfigPacingRule>>(() => initial.pacingModels);
  const [override, setOverride] = useState(EMPTY_OVERRIDE);
  const [editing, setEditing] = useState<"general" | "connections" | "discovery" | "pacing" | null>(null);
  const [error, setError] = useState<string | null>(null);

  if (item !== syncedItem) {
    const switched = item.name !== syncedItem.name;
    setSyncedItem(item);
    // Identity-sync reads pacing from the visible item, not the openai-apikey overlay.
    const next = configurationDraftFields(item, pacingFromItem(item), context.discoverySupported);
    setAdapter(next.adapter);
    setBaseUrl(next.baseUrl);
    setDefaultModel(next.defaultModel);
    setNote(next.note);
    setAllowPrivateNetwork(next.allowPrivateNetwork);
    setLiveModels(next.liveModels);
    setPacingEnabled(next.pacingEnabled);
    setPacingRpm(next.pacingRpm);
    setPacingDelay(next.pacingDelay);
    setPacingModels(next.pacingModels);
    if (switched) setOverride(EMPTY_OVERRIDE);
  }

  const resetDrafts = () => {
    const next = configurationDraftFields(item, pacingFromItem(context.pacingSource), context.discoverySupported);
    setAdapter(next.adapter);
    setBaseUrl(next.baseUrl);
    setDefaultModel(next.defaultModel);
    setNote(next.note);
    setAllowPrivateNetwork(next.allowPrivateNetwork);
    setLiveModels(next.liveModels);
    setPacingEnabled(next.pacingEnabled);
    setPacingRpm(next.pacingRpm);
    setPacingDelay(next.pacingDelay);
    setPacingModels(next.pacingModels);
    setOverride(EMPTY_OVERRIDE);
    setEditing(null);
    setError(null);
  };

  return {
    ...context,
    adapter,
    setAdapter,
    baseUrl,
    setBaseUrl,
    defaultModel,
    setDefaultModel,
    note,
    setNote,
    allowPrivateNetwork,
    setAllowPrivateNetwork,
    liveModels,
    setLiveModels,
    pacingEnabled,
    setPacingEnabled,
    pacingRpm,
    setPacingRpm,
    pacingDelay,
    setPacingDelay,
    pacingModels,
    setPacingModels,
    overrideId: override.overrideId,
    overrideRpm: override.overrideRpm,
    overrideDelay: override.overrideDelay,
    addingOverride: override.addingOverride,
    setOverrideId: (value: string) => setOverride(current => ({ ...current, overrideId: value })),
    setOverrideRpm: (value: string) => setOverride(current => ({ ...current, overrideRpm: value })),
    setOverrideDelay: (value: string) => setOverride(current => ({ ...current, overrideDelay: value })),
    setAddingOverride: (value: boolean) => setOverride(current => ({ ...current, addingOverride: value })),
    clearOverrideDraft: () => {
      setOverride(EMPTY_OVERRIDE);
      setError(null);
    },
    editing,
    setEditing,
    error,
    setError,
    resetDrafts,
  };
}
