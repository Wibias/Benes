/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useReducer } from "react";

/*
 * The raw-config sheet behind the Providers page.
 *
 * The sheet is one reducer-owned editor state: a draft, the baseline a discard returns to,
 * and the three presentation flags that used to be independent `useState` cells. Persisting
 * the draft is a separate concern with its own typed result, so the hook decides what the
 * user sees and how the editor moves, and the transport decides nothing.
 */

/** One configured provider as the JSON sheet shows it. */
export interface ConfigProvider {
  adapter: string;
  baseUrl: string;
  hasApiKey?: boolean;
  hasHeaders?: boolean;
  defaultModel?: string;
  models?: string[];
  liveModels?: boolean;
  reasoningWireFormat?: "gateway-object";
  authMode?: string;
  keyOptional?: boolean;
  disabled?: boolean;
  note?: string;
  codexAccountMode?: "direct" | "pool";
}

export interface Config {
  port: number;
  defaultProvider: string;
  providers: Record<string, ConfigProvider>;
}

/** The notice sink the Providers page hands down. */
type EditorNotice = (message: string, ok?: boolean) => void;

/** Re-read the live config. */
type ConfigRefresh = () => Promise<void>;

/** Re-read provider quotas; `true` forces a live read. */
type QuotaRefresh = (refresh?: boolean) => Promise<void>;

/** Copy for the keys this sheet renders. */
type EditorCopy = (key: string, values?: Record<string, string>) => string;

export type JsonConfigEditorDeps = {
  apiBase: string;
  config: Config | null;
  notify: EditorNotice;
  fetchConfig: ConfigRefresh;
  fetchProviderQuotas: QuotaRefresh;
  onSaved: () => void;
  t: EditorCopy;
};

/** What a save attempt did: persisted, unparseable, or refused by the listener. */
export type ConfigPersistResult =
  | { outcome: "saved"; config: unknown }
  | { outcome: "invalid-json" }
  | { outcome: "refused"; error?: string };

/** A refusal body, when the listener sent one worth showing. */
async function readRefusal(response: Response): Promise<string | undefined> {
  try {
    const body = await response.json() as { error?: unknown };
    return typeof body.error === "string" ? body.error : undefined;
  } catch {
    return undefined;
  }
}

/** The parsed draft, or a marker saying the text is not JSON at all. */
function parseDraftConfig(draft: string): { ok: true; config: unknown } | { ok: false } {
  try {
    return { ok: true, config: JSON.parse(draft) };
  } catch {
    return { ok: false };
  }
}

/**
 * Persist the raw config sheet: parse the draft, PUT it, and decode a refusal. The result is
 * typed, so the caller decides what the user is told; a transport failure is left to throw.
 */
export async function persistConfigDraft(apiBase: string, draft: string): Promise<ConfigPersistResult> {
  const parsed = parseDraftConfig(draft);
  if (!parsed.ok) {
    return { outcome: "invalid-json" };
  }
  const put: RequestInit = {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(parsed.config),
  };
  const response = await fetch(`${apiBase}/api/config`, put);
  if (response.ok) return { outcome: "saved", config: parsed.config };
  const error = await readRefusal(response);
  return error === undefined ? { outcome: "refused" } : { outcome: "refused", error };
}

/** The message a failed save shows, from the result the persistence layer returned. */
export function saveFailureMessage(
  failure: Exclude<ConfigPersistResult, { outcome: "saved" }>,
  t: EditorCopy,
): string {
  if (failure.outcome === "invalid-json") return t("prov.invalidJson");
  return failure.error && failure.error.length > 0 ? failure.error : t("prov.saveFailed");
}

/** The editor's whole state, so a transition can never land with cells out of step. */
type EditorState = {
  /** The sheet is on screen. */
  open: boolean;
  /** The unsaved-changes confirmation is on screen. */
  confirmingLeave: boolean;
  /** A save request is in flight. */
  saving: boolean;
  /** The editable text. */
  draft: string;
  /** The text a discard returns to. */
  baseline: string;
};

type EditorAction =
  | { type: "config-synchronized"; serialized: string }
  | { type: "open" }
  | { type: "edit"; draft: string }
  | { type: "request-close" }
  | { type: "leave-confirmation"; visible: boolean }
  | { type: "discard"; serialized: string }
  | { type: "restore" }
  | { type: "save-started" }
  | { type: "save-succeeded"; serialized: string }
  | { type: "save-failed" };

const EMPTY_EDITOR: EditorState = {
  open: false,
  confirmingLeave: false,
  saving: false,
  draft: "",
  baseline: "",
};

function hideEditor(state: EditorState): EditorState {
  return { ...state, open: false, confirmingLeave: false };
}

/** True while the sheet is open on edits that differ from the text it was opened with. */
export function jsonDraftIsDirty(open: boolean, draft: string, baseline: string): boolean {
  return open && draft !== baseline;
}

function reduceEditor(state: EditorState, action: EditorAction): EditorState {
  switch (action.type) {
    case "config-synchronized":
      // A closed sheet follows the live config; an open sheet owns the user's text.
      return state.open ? state : { ...state, draft: action.serialized };
    case "open":
      // The sheet opens on the text it already holds, so nothing is re-serialized here.
      return { ...state, open: true, confirmingLeave: false, baseline: state.draft };
    case "edit":
      return { ...state, draft: action.draft };
    case "request-close":
      return jsonDraftIsDirty(state.open, state.draft, state.baseline)
        ? { ...state, confirmingLeave: true }
        : hideEditor(state);
    case "leave-confirmation":
      return { ...state, confirmingLeave: action.visible };
    case "discard":
      return hideEditor({ ...state, draft: action.serialized, baseline: action.serialized });
    case "restore":
      return { ...state, draft: state.baseline };
    case "save-started":
      return { ...state, saving: true };
    case "save-succeeded":
      // The saved bytes become the new baseline, so a later discard returns to them.
      return hideEditor({ ...state, saving: false, baseline: action.serialized });
    case "save-failed":
      return { ...state, saving: false };
  }
}

/** The config as the sheet's text. */
function serializedConfig(config: Config): string {
  return JSON.stringify(config, null, 2);
}

export function useJsonConfigEditor(input: JsonConfigEditorDeps) {
  const {
    apiBase,
    config,
    notify,
    fetchConfig: reloadConfig,
    fetchProviderQuotas: reloadQuotas,
    onSaved,
    t,
  } = input;
  const [editor, dispatch] = useReducer(reduceEditor, EMPTY_EDITOR);
  const { draft, open, saving, confirmingLeave, baseline } = editor;

  useEffect(() => {
    if (!config) return;
    dispatch({ type: "config-synchronized", serialized: serializedConfig(config) });
  }, [config]);

  const saveConfig = useCallback(async (): Promise<boolean> => {
    dispatch({ type: "save-started" });
    try {
      const result = await persistConfigDraft(apiBase, draft);
      if (result.outcome !== "saved") {
        notify(saveFailureMessage(result, t), false);
        dispatch({ type: "save-failed" });
        return false;
      }
      notify(t("prov.saved"), true);
      dispatch({ type: "save-succeeded", serialized: JSON.stringify(result.config, null, 2) });
      reloadConfig();
      reloadQuotas(true);
      onSaved();
      return true;
    } catch {
      // A transport failure has always shown the invalid-JSON copy here; keep that.
      notify(t("prov.invalidJson"), false);
      dispatch({ type: "save-failed" });
      return false;
    }
  }, [apiBase, draft, reloadConfig, reloadQuotas, notify, onSaved, t]);

  const openJsonEditor = useCallback(() => {
    dispatch({ type: "open" });
  }, []);

  const discardJsonEditor = useCallback(() => {
    dispatch({ type: "discard", serialized: config ? serializedConfig(config) : baseline });
  }, [config, baseline]);

  const requestCloseJsonEditor = useCallback(() => {
    dispatch({ type: "request-close" });
  }, []);

  const restoreJsonEditor = useCallback(() => {
    dispatch({ type: "restore" });
  }, []);

  const setDraft = useCallback((next: string) => {
    dispatch({ type: "edit", draft: next });
  }, []);

  const setJsonLeaveOpen = useCallback((visible: boolean) => {
    dispatch({ type: "leave-confirmation", visible });
  }, []);

  return {
    draft,
    setDraft,
    jsonEditorOpen: open,
    jsonSaving: saving,
    jsonLeaveOpen: confirmingLeave,
    setJsonLeaveOpen,
    saveConfig,
    openJsonEditor,
    discardJsonEditor,
    requestCloseJsonEditor,
    restoreJsonEditor,
    jsonIsDirty: jsonDraftIsDirty(open, draft, baseline),
  };
}
