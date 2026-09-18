/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useReducer, useRef } from "react";
import { formatBytes } from "../format-bytes";
import { readJsonIfOk, readJsonOrThrow } from "../fetch-json";
import type { Locale, TFn, TKey } from "../i18n/shared";
import { readSessionListCache, writeSessionListCache } from "../session-list-cache";
import {
  cleanupPolicyBody,
  draftsFromPolicyResponse,
  runOutcomeView,
  shouldApplyLoadedPolicy,
  type CachedCleanupPolicy,
  type CleanupPolicy,
} from "./storage-cleanup-policy";
import { executeCleanupRun } from "./storage-cleanup-run";
import type { PolicyDrafts } from "./storage-policy-fields";

/** Local-storage key the last accepted policy is kept under. */
function policyCacheKey(apiBase: string): string {
  return `benes.storage.cleanup-policy.v1:${apiBase}`;
}

/** The response this session opens from, when the reader has one. */
function readCachedPolicy(apiBase: string): CachedCleanupPolicy | null {
  return readSessionListCache<CachedCleanupPolicy>(policyCacheKey(apiBase));
}

/** The drafts a response restores, or the values the form opens with when there is none. */
export function openingDrafts(cached: CachedCleanupPolicy | null): PolicyDrafts {
  return {
    thresholdGb: cached?.thresholdGb ?? "5",
    targetMode: cached?.targetMode ?? "percent",
    percent: cached?.percent ?? "25",
    reduceGb: cached?.reduceGb ?? "4",
  };
}

/** Everything the policy pane shows, in one value so no transition can half-apply. */
type PolicyState = {
  readonly policy: CleanupPolicy | null;
  readonly drafts: PolicyDrafts;
  readonly loading: boolean;
  readonly saving: boolean;
  readonly running: boolean;
  readonly status: string | null;
  readonly error: string | null;
};

type PolicyEvent =
  | { readonly kind: "load_started" }
  | { readonly kind: "load_settled" }
  | { readonly kind: "loaded"; readonly cached: CachedCleanupPolicy }
  | { readonly kind: "load_failed"; readonly copy: string }
  | { readonly kind: "drafted"; readonly patch: Partial<PolicyDrafts> }
  | { readonly kind: "save_started" }
  | { readonly kind: "save_failed"; readonly copy: string }
  | { readonly kind: "saved"; readonly cached: CachedCleanupPolicy; readonly copy: string }
  | { readonly kind: "run_started" }
  | { readonly kind: "run_settled"; readonly status?: string; readonly error?: string }
  | { readonly kind: "feedback_cleared" };

/**
 * Policy transitions.
 *
 * A settled load only ends the wait: a load that found nothing new leaves the values on screen
 * alone, which is what keeps a background poll from undoing an edit the reader is making.
 */
function reducePolicy(state: PolicyState, event: PolicyEvent): PolicyState {
  switch (event.kind) {
    case "load_started":
      return { ...state, loading: true, error: null };
    case "load_settled":
      return { ...state, loading: false };
    case "loaded":
      return {
        ...state,
        policy: event.cached.policy,
        drafts: openingDrafts(event.cached),
        loading: false,
        error: null,
      };
    case "load_failed":
      return { ...state, policy: null, loading: false, error: event.copy };
    case "drafted":
      return { ...state, drafts: { ...state.drafts, ...event.patch } };
    case "save_started":
      return { ...state, saving: true, status: null, error: null };
    case "save_failed":
      return { ...state, saving: false, error: event.copy };
    case "saved":
      return {
        ...state,
        policy: event.cached.policy,
        drafts: openingDrafts(event.cached),
        saving: false,
        status: event.copy,
        error: null,
      };
    case "run_started":
      return { ...state, running: true, status: null, error: null };
    case "run_settled":
      return { ...state, running: false, error: event.error ?? null, status: event.status ?? null };
    case "feedback_cleared":
      return { ...state, status: null, error: null };
  }
}

/** Copy for each way a run can end without reporting an outcome of its own. */
const RUN_FAILURE_COPY: Record<"save_failed" | "already_running" | "run_failed", TKey> = {
  save_failed: "storage.policy.saveFailed",
  already_running: "storage.policy.alreadyRunning",
  run_failed: "storage.policy.runFailed",
};

/** Wait between two polls of a running cleanup. */
function sleep(ms: number): Promise<void> {
  return new Promise<void>(resolve => {
    window.setTimeout(resolve, ms);
  });
}

/** Read the current policy off the listener. */
async function readPolicy(apiBase: string, signal?: AbortSignal): Promise<CleanupPolicy> {
  const response = await fetch(`${apiBase}/api/storage/cleanup-policy`, { signal });
  return await readJsonOrThrow<CleanupPolicy>(response) as CleanupPolicy;
}

/** Write one policy body, and hand back what the listener accepted. */
async function writePolicy(apiBase: string, body: CleanupPolicy): Promise<CleanupPolicy | null> {
  const response = await fetch(`${apiBase}/api/storage/cleanup-policy`, {
    method: "PUT",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
  const saved = await readJsonIfOk<{ policy?: CleanupPolicy }>(response);
  return saved?.policy ?? null;
}

/** A load may only act while it is still the newest one and nobody has aborted it. */
function loadIsCurrent(signal: AbortSignal | undefined, generation: number, newest: number): boolean {
  return !signal?.aborted && generation === newest;
}

/** An abort is the reader's own doing, not a failure worth reporting. */
function wasAborted(signal: AbortSignal, error: unknown): boolean {
  if (signal.aborted) return true;
  return error instanceof DOMException && error.name === "AbortError";
}

/**
 * The automatic-cleanup policy, and the edit session that moves it.
 *
 * Whether the drafts are dirty and whether a field holds focus are refs rather than state on
 * purpose: the loader reads them when a response lands, and dropping that response must not itself
 * cause a render.
 */
export function useCleanupPolicy(options: {
  apiBase: string;
  locale: Locale;
  t: TFn;
  onDone: () => void;
}) {
  const { apiBase, locale, t, onDone } = options;
  const cacheKey = policyCacheKey(apiBase);
  const cached = readCachedPolicy(apiBase);
  const hasCacheRef = useRef(Boolean(cached));
  const editSessionRef = useRef({ dirty: false, editing: false });
  const loadGenerationRef = useRef(0);
  const runAbortRef = useRef<AbortController | null>(null);
  const [state, dispatch] = useReducer(reducePolicy, {
    policy: cached?.policy ?? null,
    drafts: openingDrafts(cached),
    loading: !cached,
    saving: false,
    running: false,
    status: null,
    error: null,
  });

  /** Accept a response as the new baseline, and write it back to the session cache. */
  const accept = useCallback((json: CleanupPolicy) => {
    const next = draftsFromPolicyResponse(json);
    hasCacheRef.current = true;
    writeSessionListCache(cacheKey, next);
    editSessionRef.current.dirty = false;
    return next;
  }, [cacheKey]);

  const loadPolicy = useCallback(async (signal?: AbortSignal): Promise<void> => {
    const generation = ++loadGenerationRef.current;
    const session = editSessionRef.current;
    if (!hasCacheRef.current) dispatch({ kind: "load_started" });
    let loaded: CleanupPolicy | null;
    try {
      loaded = await readPolicy(apiBase, signal);
    } catch {
      loaded = null;
    }
    if (!loadIsCurrent(signal, generation, loadGenerationRef.current)) return;
    if (loaded !== null && shouldApplyLoadedPolicy({
      aborted: false,
      generation,
      currentGeneration: loadGenerationRef.current,
      dirty: session.dirty,
      editing: session.editing,
    })) {
      dispatch({ kind: "loaded", cached: accept(loaded) });
    } else if (loaded === null && !hasCacheRef.current) {
      dispatch({ kind: "load_failed", copy: t("storage.policy.loadFailed") });
    }
    dispatch({ kind: "load_settled" });
  }, [accept, apiBase, t]);

  useEffect(() => {
    const controller = new AbortController();
    const timeout = window.setTimeout(() => {
      void loadPolicy(controller.signal);
    }, 0);
    return () => {
      window.clearTimeout(timeout);
      loadGenerationRef.current += 1;
      controller.abort();
    };
  }, [loadPolicy]);

  useEffect(() => {
    return () => {
      runAbortRef.current?.abort();
      runAbortRef.current = null;
    };
  }, []);

  /** The drafts are no longer the baseline once the reader touches a field. */
  const markDirty = () => {
    editSessionRef.current.dirty = true;
  };

  /** A field takes and releases focus around its own commit. */
  const setEditing = (editing: boolean) => {
    editSessionRef.current.editing = editing;
  };

  const editDrafts = useCallback((patch: Partial<PolicyDrafts>) => {
    dispatch({ kind: "drafted", patch });
  }, []);

  const savePolicy = async (patch?: Partial<CleanupPolicy>): Promise<void> => {
    const base = cleanupPolicyBody(state.policy, state.drafts);
    if (!base) {
      dispatch({ kind: "save_failed", copy: t("storage.policy.invalid") });
      return;
    }
    dispatch({ kind: "save_started" });
    try {
      const saved = await writePolicy(apiBase, { ...base, ...patch });
      if (!saved) {
        dispatch({ kind: "save_failed", copy: t("storage.policy.saveFailed") });
        return;
      }
      dispatch({ kind: "saved", cached: accept(saved), copy: t("storage.policy.saved") });
    } catch {
      dispatch({ kind: "save_failed", copy: t("storage.policy.saveFailed") });
    }
  };

  /** Turn the finished run into the line or the refusal the pane reports. */
  const reportRun = (view: ReturnType<typeof runOutcomeView>): void => {
    if (view.kind === "status" || view.kind === "error") {
      dispatch({ kind: "run_settled", error: t(view.key) });
      return;
    }
    const report = view.mode === "permanent"
      ? t("storage.policy.donePermanent", {
        count: String(view.removed),
        size: formatBytes(view.freedBytes, locale),
      })
      : t("storage.policy.doneQuarantineCount", { count: String(view.removed) });
    dispatch({ kind: "run_settled", status: report });
    onDone();
  };

  const runNow = async (): Promise<void> => {
    runAbortRef.current?.abort();
    const controller = new AbortController();
    runAbortRef.current = controller;
    const { signal } = controller;
    const base = cleanupPolicyBody(state.policy, state.drafts);
    if (!base) {
      dispatch({ kind: "save_failed", copy: t("storage.policy.invalid") });
      return;
    }
    dispatch({ kind: "run_started" });
    try {
      const result = await executeCleanupRun({ apiBase, body: base, signal, sleep, onPolicy: accept });
      if (result.kind === "aborted") return;
      if (result.kind !== "outcome") {
        dispatch({ kind: "run_settled", error: t(RUN_FAILURE_COPY[result.kind]) });
        return;
      }
      reportRun(runOutcomeView(result.outcome));
    } catch (error) {
      if (wasAborted(signal, error)) return;
      dispatch({ kind: "run_settled", error: t("storage.policy.runFailed") });
    } finally {
      if (runAbortRef.current === controller) runAbortRef.current = null;
    }
  };

  const formatWhen = (ms: number | undefined): string => {
    if (ms === undefined) return t("storage.policy.never");
    return new Date(ms).toLocaleString(locale);
  };

  return {
    state,
    markDirty,
    setEditing,
    editDrafts,
    loadPolicy,
    savePolicy,
    runNow,
    formatWhen,
    clearFeedback: () => { dispatch({ kind: "feedback_cleared" }); },
  };
}