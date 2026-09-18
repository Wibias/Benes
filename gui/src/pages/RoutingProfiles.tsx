/** Benes dashboard source. The Routing profiles workspace.
 *
 * The state owner for the Profiles surfaces: it reads the editor's inputs, keeps the open
 * profile and draft, writes profiles, and runs the dry run. What one read means — which sources
 * may answer with nothing, what a settled profile does to an open draft, when a dry run stops
 * describing what is on screen — is `./routing-profiles-load`'s, so this file only sequences it.
 *
 * The rendered surfaces are `./routing-profiles-sections`'s: this file hands them a view and
 * never lays anything out itself.
 */
import { useCallback, useEffect, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { readJsonIfOk } from "../fetch-json";
import { useI18n } from "../i18n/shared";
import {
  routingProfileDraftFromDto,
  routingProfilePutBody,
} from "../routing-profile/profile-codec";
import {
  newDraftCandidate,
  newRoutingProfileDraft,
  type RoutingProfileDraft,
  type RoutingProfileDto,
} from "../routing-profile/profile-model";
import {
  routingProfileResponseError,
  routingProfileResponseSucceeded,
} from "../routing-profile/profile-response";
import {
  firstModelForProvider,
  modelOptionsForProvider,
  routingDryRunEvidence,
  selectedProfileAfterLoad,
} from "./routing-profile-decode";
import type { DryRunResult } from "./routing-profiles-format";
import {
  EMPTY_ROUTING_DATA,
  EMPTY_ROUTING_EDITOR,
  dryRunSurvivesReload,
  editorAfterLoad,
  fetchRoutingProfileSources,
  routingProfilesData,
  type RoutingEditorState,
  type RoutingProfilesData,
  type RoutingProfileSources,
} from "./routing-profiles-load";
import { RoutingProfilesPage, type RoutingProfilesView } from "./routing-profiles-sections";

/** How long a success notice stays up. */
const NOTICE_MS = 5000;

/** The dry-run form, and the probe it last ran. */
interface RoutingDryRunState {
  readonly context: string;
  readonly tools: boolean;
  readonly image: boolean;
  readonly structured: boolean;
  readonly running: boolean;
  readonly result: DryRunResult | null;
  readonly error: string;
}

const EMPTY_DRY_RUN: RoutingDryRunState = {
  context: "",
  tools: false,
  image: false,
  structured: false,
  running: false,
  result: null,
  error: "",
};

export default function RoutingProfiles({
  apiBase,
  active = true,
  surface = "profiles",
  refreshNonce = 0,
  onCountChange,
  onOpenEvaluation,
  onOpenAnalytics,
  onOpenOverview,
}: {
  apiBase: string;
  /**
   * False while this panel is mounted but hidden behind another surface. Defaults true so a
   * direct render (tests) behaves like a visible panel.
   */
  active?: boolean;
  surface?: "profiles" | "evaluation" | "analytics";
  refreshNonce?: number;
  /** Reports the profile count up to the tab strip. */
  onCountChange?: (count: number) => void;
  onOpenEvaluation?: () => void;
  onOpenAnalytics?: () => void;
  onOpenOverview?: () => void;
}) {
  const { locale, t } = useI18n();
  const unavailable = t("routing.unavailable");
  const [data, setData] = useState<RoutingProfilesData>(EMPTY_ROUTING_DATA);
  const [editor, setEditor] = useState<RoutingEditorState>(EMPTY_ROUTING_EDITOR);
  const [dryRun, setDryRun] = useState<RoutingDryRunState>(EMPTY_DRY_RUN);
  const [notice, setNotice] = useState<{ message: string; ok: boolean } | null>(null);
  const [loadError, setLoadError] = useState("");
  const [saving, setSaving] = useState(false);
  const [query, setQuery] = useState("");
  /*
   * The editor as the async reads see it. `load` settles long after the render that started it,
   * so it reads the draft the reader is typing into now rather than the one captured with the
   * request — which is why every transition below goes through `applyEditor`.
   */
  const editorRef = useRef<RoutingEditorState>(EMPTY_ROUTING_EDITOR);
  const loadRef = useRef<AbortController | null>(null);
  const liveRef = useRef(true);
  const generationRef = useRef(0);
  const dryRunGenerationRef = useRef(0);

  const applyEditor = useCallback((next: RoutingEditorState) => {
    editorRef.current = next;
    setEditor(next);
  }, []);

  useEffect(() => {
    if (notice === null || !notice.ok) return undefined;
    const timer = window.setTimeout(() => setNotice(null), NOTICE_MS);
    return () => window.clearTimeout(timer);
  }, [notice]);

  const clearDryRun = useCallback(() => {
    dryRunGenerationRef.current += 1;
    setDryRun(current => ({ ...current, running: false, result: null, error: "" }));
  }, []);

  /*
   * Stop loading, and move the generation past whatever is in flight.
   *
   * Aborting is not enough on its own: a save or a delete can settle after the panel is hidden
   * and then start a fresh load the deactivation has already run past. The generation is what
   * makes that late load a no-op.
   */
  const cancelActiveLoad = useCallback(() => {
    liveRef.current = false;
    loadRef.current?.abort();
    generationRef.current += 1;
  }, []);

  /**
   * Apply one settled read: the inputs it produced, and the editor they leave behind.
   *
   * Which profile the read settled on, and whether it replaces an open draft, are
   * `./routing-profiles-load`'s decisions; this only hands them the state they read.
   */
  const settle = useCallback((sources: RoutingProfileSources, settledOnId?: string) => {
    const nextData = routingProfilesData(sources);
    const before = editorRef.current.selected;
    const refreshed = selectedProfileAfterLoad(nextData.profiles, before?.id ?? null, settledOnId);
    const replacesEditor = settledOnId !== undefined;
    setData(nextData);
    applyEditor(editorAfterLoad({
      editor: editorRef.current,
      models: nextData.models,
      settledOn: refreshed,
      replacesEditor,
    }));
    if (replacesEditor && !dryRunSurvivesReload(before, refreshed)) clearDryRun();
  }, [applyEditor, clearDryRun]);

  const load = useCallback(async (settledOnId?: string) => {
    if (!liveRef.current) return;
    loadRef.current?.abort();
    const controller = new AbortController();
    loadRef.current = controller;
    const generation = ++generationRef.current;
    setLoadError("");
    try {
      const sources = await fetchRoutingProfileSources(
        apiBase,
        controller.signal,
        settledOnId ?? editorRef.current.selected?.id ?? null,
      );
      if (generation === generationRef.current) settle(sources, settledOnId);
    } catch (error) {
      // A superseded or deactivated read is not a failure worth showing.
      if (generation === generationRef.current && !controller.signal.aborted) {
        setLoadError(error instanceof Error ? error.message : String(error));
      }
    } finally {
      // Clear only if this request still owns the ref; a newer load may have replaced it.
      if (loadRef.current === controller) loadRef.current = null;
    }
  }, [apiBase, settle]);

  useEffect(() => {
    if (!active) {
      cancelActiveLoad();
      return undefined;
    }
    liveRef.current = true;
    const timer = window.setTimeout(() => { void load(); }, 0);
    return () => {
      window.clearTimeout(timer);
      cancelActiveLoad();
    };
  }, [active, cancelActiveLoad, load, refreshNonce]);

  useEffect(() => {
    onCountChange?.(data.profiles.length);
  }, [onCountChange, data.profiles.length]);

  /** Open one profile, or clear the editor when there is none to open. */
  const selectProfile = useCallback((profile: RoutingProfileDto | null) => {
    applyEditor({
      selected: profile,
      draft: profile === null ? null : routingProfileDraftFromDto(profile),
      editing: false,
    });
    setNotice(null);
    clearDryRun();
    if (profile !== null) void load(profile.id);
  }, [applyEditor, clearDryRun, load]);

  const startCreate = useCallback(() => {
    const provider = data.providerNames[0] ?? "";
    applyEditor({
      selected: null,
      draft: newRoutingProfileDraft(provider, firstModelForProvider(data.models, provider)),
      editing: true,
    });
    setNotice(null);
    clearDryRun();
  }, [applyEditor, clearDryRun, data.models, data.providerNames]);

  const cancelEdit = useCallback(() => {
    const current = editorRef.current;
    setNotice(null);
    if (current.selected !== null) {
      applyEditor({ ...current, editing: false, draft: routingProfileDraftFromDto(current.selected) });
      return;
    }
    selectProfile(data.profiles[0] ?? null);
  }, [applyEditor, data.profiles, selectProfile]);

  /** Change the open draft in place; without one there is nothing to change. */
  const editDraft = useCallback((change: (draft: RoutingProfileDraft) => RoutingProfileDraft) => {
    const current = editorRef.current;
    if (current.draft === null) return;
    applyEditor({ ...current, draft: change(current.draft) });
  }, [applyEditor]);

  const setDraft = useCallback<Dispatch<SetStateAction<RoutingProfileDraft | null>>>((update) => {
    const current = editorRef.current;
    applyEditor({ ...current, draft: typeof update === "function" ? update(current.draft) : update });
  }, [applyEditor]);

  const setEditing = useCallback((value: boolean) => {
    applyEditor({ ...editorRef.current, editing: value });
  }, [applyEditor]);

  const updateCandidate = useCallback((
    index: number,
    field: "provider" | "model",
    value: string,
  ) => {
    editDraft(draft => ({
      ...draft,
      candidates: draft.candidates.map((candidate, at) => {
        if (at !== index) return candidate;
        if (field === "provider") {
          return { ...candidate, provider: value, model: firstModelForProvider(data.models, value) };
        }
        return value.trim() === "" ? candidate : { ...candidate, model: value };
      }),
    }));
  }, [data.models, editDraft]);

  const addCandidate = useCallback(() => {
    const provider = data.providerNames[0] ?? "";
    editDraft(draft => ({
      ...draft,
      candidates: [
        ...draft.candidates,
        newDraftCandidate(provider, firstModelForProvider(data.models, provider)),
      ],
    }));
  }, [data.models, data.providerNames, editDraft]);

  const removeCandidate = useCallback((index: number) => {
    editDraft(draft => ({
      ...draft,
      candidates: draft.candidates.filter((_, at) => at !== index),
    }));
  }, [editDraft]);

  const moveCandidate = useCallback((from: number, to: number) => {
    editDraft(draft => {
      if (from === to) return draft;
      if (from < 0 || to < 0 || from >= draft.candidates.length || to >= draft.candidates.length) return draft;
      const candidates = [...draft.candidates];
      const [row] = candidates.splice(from, 1);
      candidates.splice(to, 0, row!);
      return { ...draft, candidates };
    });
  }, [editDraft]);

  /**
   * One profile write.
   *
   * A refusal is read for its own message — the listener explains a rejected profile better than
   * this page can — and a write that landed reloads the list, so the editor shows what was stored
   * rather than what was typed.
   */
  const writeProfile = useCallback(async (
    body: ReturnType<typeof routingProfilePutBody>,
  ): Promise<boolean> => {
    setSaving(true);
    setNotice(null);
    try {
      const response = await fetch(`${apiBase}/api/routing-profiles`, {
        method: "PUT",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(body),
      });
      const payload = await readJsonIfOk<unknown>(response);
      const refusal = response.ok ? payload : await response.json().catch(() => null) as unknown;
      if (!response.ok || !routingProfileResponseSucceeded(payload)) {
        setNotice({ message: routingProfileResponseError(refusal) ?? t("routing.loadFailed"), ok: false });
        return false;
      }
      await load(body.id);
      setNotice({ message: t("common.ok"), ok: true });
      return true;
    } catch (error) {
      setNotice({ message: error instanceof Error ? error.message : t("routing.loadFailed"), ok: false });
      return false;
    } finally {
      setSaving(false);
    }
  }, [apiBase, load, t]);

  const saveProfile = useCallback(() => {
    const current = editorRef.current;
    if (current.draft === null || saving) return;
    void writeProfile(routingProfilePutBody(
      current.draft,
      current.selected === null ? "create" : "update",
      current.selected?.revision,
    ));
  }, [saving, writeProfile]);

  const saveProfileIcon = useCallback(async (iconId: string): Promise<boolean> => {
    const selected = editorRef.current.selected;
    if (selected === null || saving) return false;
    const draft = routingProfileDraftFromDto(selected);
    draft.icon = iconId.trim() || "file-document";
    return writeProfile(routingProfilePutBody(draft, "update", selected.revision));
  }, [saving, writeProfile]);

  const removeProfile = useCallback(() => {
    const selected = editorRef.current.selected;
    if (selected === null || saving) return;
    if (!window.confirm(t("routing.removeConfirm", { id: selected.id }))) return;
    void (async () => {
      setSaving(true);
      setNotice(null);
      try {
        const response = await fetch(
          `${apiBase}/api/routing-profiles?id=${encodeURIComponent(selected.id)}`,
          { method: "DELETE" },
        );
        const payload = await readJsonIfOk<unknown>(response);
        const refusal = response.ok ? payload : await response.json().catch(() => null) as unknown;
        if (!response.ok || !routingProfileResponseSucceeded(payload)) {
          setNotice({ message: routingProfileResponseError(refusal) ?? t("routing.loadFailed"), ok: false });
          return;
        }
        applyEditor({ selected: null, draft: null, editing: false });
        await load();
        setNotice({ message: t("common.ok"), ok: true });
      } catch (error) {
        setNotice({ message: error instanceof Error ? error.message : t("routing.loadFailed"), ok: false });
      } finally {
        setSaving(false);
      }
    })();
  }, [apiBase, applyEditor, load, saving, t]);

  /**
   * Prove a profile against declared evidence.
   *
   * Only what was declared is sent (see `routingDryRunEvidence`), and only the newest probe may
   * report: a superseded one would otherwise overwrite the result of the probe that followed it.
   */
  const runDryRun = useCallback(() => {
    const selected = editorRef.current.selected;
    if (selected === null) return;
    const generation = ++dryRunGenerationRef.current;
    setDryRun(current => ({ ...current, running: true, result: null, error: "" }));
    void (async () => {
      try {
        const response = await fetch(`${apiBase}/api/routing-profiles/dry-run`, {
          method: "POST",
          headers: { "content-type": "application/json" },
          body: JSON.stringify({
            profile: selected.id,
            evidence: routingDryRunEvidence({
              context: dryRun.context,
              tools: dryRun.tools,
              image: dryRun.image,
              structured: dryRun.structured,
            }),
          }),
        });
        if (generation !== dryRunGenerationRef.current) return;
        if (!response.ok) {
          const refusal = await response.json().catch(() => null) as unknown;
          if (generation !== dryRunGenerationRef.current) return;
          setDryRun(current => ({
            ...current,
            error: routingProfileResponseError(refusal)
              ?? t("routing.dryRunError", { status: response.status }),
          }));
          return;
        }
        const result = await response.json() as DryRunResult;
        if (generation !== dryRunGenerationRef.current) return;
        setDryRun(current => ({ ...current, result }));
      } catch (error) {
        if (generation !== dryRunGenerationRef.current) return;
        setDryRun(current => ({
          ...current,
          error: error instanceof Error ? error.message : String(error),
        }));
      } finally {
        if (generation === dryRunGenerationRef.current) {
          setDryRun(current => ({ ...current, running: false }));
        }
      }
    })();
  }, [apiBase, dryRun, t]);

  const view: RoutingProfilesView = {
    t,
    locale,
    unavailable,
    loadError,
    status: notice,
    clearStatus: () => setNotice(null),
    profiles: data.profiles,
    selected: editor.selected,
    draft: editor.draft,
    saving,
    editing: editor.editing,
    query,
    surface,
    context: dryRun.context,
    tools: dryRun.tools,
    image: dryRun.image,
    structured: dryRun.structured,
    dryRunResult: dryRun.result,
    dryRunError: dryRun.error,
    running: dryRun.running,
    catalogSuites: data.catalogSuites,
    catalogError: data.catalogError,
    analytics: data.analytics,
    providerNames: data.providerNames,
    selectedModelOptions: editor.draft?.candidates.map(
      candidate => modelOptionsForProvider(data.models, candidate.provider),
    ) ?? [],
    startCreate,
    onRetry: () => { void load(); },
    selectProfile,
    setQuery,
    setEditing,
    setDraft,
    updateCandidate,
    addCandidate,
    removeCandidate,
    moveCandidate,
    onSave: saveProfile,
    onSaveIcon: saveProfileIcon,
    onCancel: cancelEdit,
    onRemove: removeProfile,
    setContext: value => setDryRun(current => ({ ...current, context: value })),
    setTools: value => setDryRun(current => ({ ...current, tools: value })),
    setImage: value => setDryRun(current => ({ ...current, image: value })),
    setStructured: value => setDryRun(current => ({ ...current, structured: value })),
    clearDryRun,
    runDryRun,
    onOpenEvaluation: () => { onOpenEvaluation?.(); },
    onOpenAnalytics: () => { onOpenAnalytics?.(); },
    onOpenOverview: () => { onOpenOverview?.(); },
  };

  return <RoutingProfilesPage view={view} />;
}
