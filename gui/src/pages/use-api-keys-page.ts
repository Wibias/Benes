/**
 * Benes dashboard source. The API page's state: two boards, one dialog, and the
 * surfaces the workspace renders.
 *
 * The page reads two things the listener owns — the key board and the model
 * catalogue — through the shared data-surface owner, so the three states that
 * look alike stay apart: a first load is a skeleton, a failed refresh beside
 * last-good rows is a banner plus those rows, and a cold failure is the gate.
 *
 * Everything the workspace needs is assembled into the surfaces the workspace
 * declares (`ApiKeysReadyModel`), plus the create dialog's own fields. The
 * surfaces are built in one place so a panel cannot be handed a field that
 * belongs to another panel.
 *
 * Nothing here restates a rule an owner already holds: the key board's cached
 * seed comes from `./api-keys-decode`, and the catalogue's search and labels
 * come from `api-access/model-catalog-view`.
 */
import { useCallback, useEffect, useMemo, useState } from "react";
import { LOCALES, useI18n, type TFn } from "../i18n/shared.ts";
import { PROVIDER_ERROR_TOAST_MS } from "../lib/provider-notice-policy.ts";
import { formatProviderDisplayName } from "../provider-icons.ts";
import {
  apiProtocolLabel,
  apiSourceLabel,
  filterCatalogRows,
  type ExternalModelRow,
} from "../api-access/model-catalog-view.ts";
import { type GatewayInboundProtocol, type ModelProbeResults } from "../api-access/model-tests.ts";
import { readSessionListCacheEntry } from "../session-list-cache.ts";
import { useDataSurface, type DataSurfaceState } from "../data-surface.ts";
import { readKeysBoardSeed, type CachedKeysShape } from "./api-keys-decode.ts";
import {
  deleteApiKey,
  loadKeysPayload,
  loadModelsCatalog,
  runCopyKey,
  runCopyModelId,
  runCreateKey,
  runTestModel,
} from "./api-keys-actions.ts";
import type { ApiKeysReadyModel } from "./api-keys-page-body.tsx";

/** Session-cache identities. Versioned, because a cached board is a view model. */
const KEYS_CACHE_PREFIX = "benes.apikeys.list.v2";
const MODELS_CACHE_PREFIX = "benes.apikeys.models.v1";

const EMPTY_MODELS: ExternalModelRow[] = [];
const MUTATION_TIMEOUT_MS = 15_000;

/**
 * The Keys board's presentation, as the page reads it.
 *
 * `listFailed` is only a banner while rows are on screen: with no board at all
 * the same failure is the cold gate's, and saying it twice would report one
 * failure as two.
 */
function keysPresentation(state: DataSurfaceState<CachedKeysShape>, board: CachedKeysShape | null, t: TFn) {
  return {
    listFailed: Boolean(state.showError && board),
    refreshing: state.refreshing,
    showSkeleton: Boolean(state.showSkeleton && !board),
    failedCold: state.kind === "failed-cold" && !board,
    errorMessage: state.error instanceof Error ? state.error.message : t("api.keysLoadFailed"),
  };
}

/**
 * The catalogue's presentation.
 *
 * A refresh that failed reports progress next to the rows it kept rather than
 * replacing them with a failure, which is what makes a stale catalogue readable
 * instead of blank.
 */
function modelsPresentation(state: DataSurfaceState<ExternalModelRow[]>, cached: ExternalModelRow[] | null) {
  const hasData = state.data !== undefined || cached !== null;
  return {
    loading: Boolean(state.showSkeleton && state.data === undefined && cached === null),
    refreshing: Boolean(state.refreshing && state.showError && hasData),
    hasData,
  };
}

/**
 * The two boards the page reads.
 *
 * Each is seeded from its own session cache — the key board through the decoder
 * that knows what one is, the catalogue through the store directly — so a
 * revisit paints the last known board instead of a skeleton while the live read
 * is in flight.
 */
function useApiKeysResources(apiBase: string, active: boolean, t: TFn) {
  const keysCacheKey = `${KEYS_CACHE_PREFIX}:${apiBase}`;
  const modelsCacheKey = `${MODELS_CACHE_PREFIX}:${apiBase}`;
  const keysSeed = readKeysBoardSeed(keysCacheKey);
  const cachedModelsEntry = readSessionListCacheEntry<ExternalModelRow[]>(modelsCacheKey);
  const cachedModels = cachedModelsEntry?.data ?? null;
  const fetchKeys = useCallback(
    (signal: AbortSignal) => loadKeysPayload(apiBase, signal, keysCacheKey, t),
    [apiBase, keysCacheKey, t],
  );
  const fetchModels = useCallback(
    (signal: AbortSignal) => loadModelsCatalog(apiBase, signal, modelsCacheKey, t),
    [apiBase, modelsCacheKey, t],
  );
  const keysResource = useDataSurface<CachedKeysShape>(`api-keys:${apiBase}`, [apiBase], fetchKeys, {
    isEmpty: board => board.keys.length === 0,
    initialData: keysSeed?.board,
    initialDataCachedAt: keysSeed?.cachedAt ?? null,
    staleAfterMs: 60_000,
    enabled: active,
  });
  const modelsResource = useDataSurface<ExternalModelRow[]>(`api-models:${apiBase}`, [apiBase], fetchModels, {
    isEmpty: models => models.length === 0,
    initialData: cachedModels ?? undefined,
    initialDataCachedAt: cachedModelsEntry?.cachedAt ?? null,
    staleAfterMs: 60_000,
    enabled: active,
  });
  return { keysSeed, cachedModels, keysResource, modelsResource };
}

/** The create-key dialog's fields, which travel beside the workspace surfaces. */
export interface ApiKeyDialogFields {
  newName: string;
  creating: boolean;
  newKey: string | null;
  copied: boolean;
  onNewNameChange: (value: string) => void;
  onCreate: () => void;
  onCopyKey: () => void;
}

/** What the page hands its frame and its workspace. */
export type ApiKeysReadyFields = ApiKeysReadyModel & ApiKeyDialogFields;

/**
 * The API page's state.
 *
 * `ready` is `null` until a board exists, which is what keeps the page from
 * rendering a workspace over data nothing has read. A probe result is keyed by
 * model and wire in `api-access/model-tests`, and a probe only runs while the
 * page still holds the freshly created key — a stored key is never used, because
 * the dashboard never receives one.
 */
export function useApiKeysPage(apiBase: string, active: boolean) {
  const { t, locale } = useI18n();
  const localeTag = LOCALES.find(entry => entry.code === locale)?.htmlLang;
  const { keysSeed, cachedModels, keysResource, modelsResource } = useApiKeysResources(apiBase, active, t);
  const [actionError, setActionError] = useState<string | null>(null);
  const [modelQuery, setModelQuery] = useState("");
  const [copiedModelId, setCopiedModelId] = useState<string | null>(null);
  const [modelTests, setModelProbeResults] = useState<ModelProbeResults>({});
  const [newName, setNewName] = useState("");
  const [creating, setCreating] = useState(false);
  const [newKey, setNewKey] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [testNotice, setTestNotice] = useState<string | null>(null);

  useEffect(() => {
    if (!testNotice) return undefined;
    const timer = window.setTimeout(() => setTestNotice(null), PROVIDER_ERROR_TOAST_MS);
    return () => window.clearTimeout(timer);
  }, [testNotice]);

  const board = keysResource.state.data ?? keysSeed?.board ?? null;
  const models = modelsResource.state.data ?? cachedModels ?? EMPTY_MODELS;
  const filteredModels = useMemo(() => filterCatalogRows(models, modelQuery), [modelQuery, models]);
  const keysBits = keysPresentation(keysResource.state, board, t);
  const modelBits = modelsPresentation(modelsResource.state, cachedModels);

  const onCreate = () => {
    void runCreateKey({
      creating,
      apiBase,
      name: newName,
      t,
      setCreating,
      setActionError,
      setNewKey,
      setNewName,
      refresh: keysResource.refresh,
    });
  };

  const onCopyKey = () => {
    void runCopyKey({ newKey, t, setActionError, setCopied });
  };

  const ready: ApiKeysReadyFields | null = board === null ? null : {
    keys: {
      entries: board.keys,
      listFailed: keysBits.listFailed,
      attributionSince: board.attributionSince,
      historyTruncated: board.historyTruncated === true,
      localeTag,
      onDelete: (id: string) => deleteApiKey(apiBase, id, MUTATION_TIMEOUT_MS).then(removed => {
        if (removed) keysResource.refresh();
        return removed;
      }),
    },
    clients: { apiBase, hasKeys: board.keys.length > 0 },
    endpoints: {
      endpoints: board.endpoints,
      authMatrix: board.authMatrix,
      claudeEnabled: board.claudeCodeEnabled,
    },
    models: {
      state: {
        models: filteredModels,
        totalCount: models.length,
        query: modelQuery,
        loading: modelBits.loading,
        refreshing: modelBits.refreshing,
        loadFailed: modelsResource.state.showError,
        hasData: modelBits.hasData,
        copiedModelId,
        probes: modelTests,
        probesAvailable: newKey !== null,
        sourceLabel: (model: ExternalModelRow) => apiSourceLabel(model, t, formatProviderDisplayName),
        protocolLabel: (protocol: GatewayInboundProtocol) => apiProtocolLabel(protocol, t),
      },
      actions: {
        search: setModelQuery,
        copyId: (modelId: string) => { void runCopyModelId({ modelId, setCopiedModelId }); },
        test: (model: ExternalModelRow, protocol: GatewayInboundProtocol) => {
          void runTestModel({
            model,
            protocol,
            apiKey: newKey,
            endpoints: board.endpoints,
            t,
            setModelTests: setModelProbeResults,
            onFailure: detail => {
              setTestNotice(t("api.testFail.detail", {
                protocol: apiProtocolLabel(protocol, t),
                reason: detail,
              }));
            },
          });
        },
        retry: () => modelsResource.refresh({ forceLoading: true }),
      },
    },
    newName,
    creating,
    newKey,
    copied,
    onNewNameChange: setNewName,
    onCreate,
    onCopyKey,
  };

  return {
    actionError,
    onClearActionError: () => setActionError(null),
    testNotice,
    onDismissTestNotice: () => setTestNotice(null),
    keysError: keysBits.listFailed,
    busy: keysBits.refreshing || modelsResource.state.refreshing,
    showSkeleton: keysBits.showSkeleton,
    failedCold: keysBits.failedCold,
    errorMessage: keysBits.errorMessage,
    onRetryKeys: keysResource.refresh,
    ready,
  };
}
