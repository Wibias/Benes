/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useReducer, useRef } from "react";
import { readJsonOrThrow } from "../fetch-json";
import { startVisibilityPoll } from "../visibility-poll";
import { createBoundedFetch } from "../bounded-fetch";
import { useT, type TKey } from "../i18n/shared";
import type { NoticeTone } from "../ui";
import {
  accountPickerInitialLoadFailed,
  accountPickerReducer,
  decodeAccountPickerEnabled,
  decodeAccountPickerSave,
  initialAccountPickerState,
  type AccountPickerEvent,
  type AccountPickerFeedbackKind,
} from "../lib/codex-account-picker-policy";
import {
  CodexAccountPickerControls,
  CodexAccountPickerCopy,
  CodexAccountPickerFeedback,
} from "./codex-account-picker-sections";

/** The setting lives on the shared settings document. */
const SETTINGS_PATH = "/api/settings";
const JSON_HEADERS = { "content-type": "application/json" } as const;

/** Card chrome, kept in one place. */
const CARD_CLASS = "card card-row codex-account-picker-card";

/** How long a settings read may take before the card gives up on that attempt. */
const READ_TIMEOUT_MS = 15_000;

/** How often the setting is re-read while the tab is visible. */
const SETTINGS_POLL_MS = 30_000;

/** The body a settings write answers with. */
interface SettingsWriteBody {
  ok?: unknown;
  codexAccountPickerEnabled?: unknown;
  catalogRefreshPending?: unknown;
}

/** Copy this card resolves. */
const UPDATED_KEY: TKey = "codexAuth.accountPickerUpdated";
const REFRESH_PENDING_KEY: TKey = "codexAuth.catalogRefreshPending";
const UPDATE_FAILED_KEY: TKey = "codexAuth.accountPickerUpdateFailed";

export interface CodexAccountPickerSettingProps {
  apiBase: string;
}

function settingsUrl(apiBase: string): string {
  return `${apiBase}${SETTINGS_PATH}`;
}

function settingsWrite(enabled: boolean): RequestInit {
  return {
    method: "PUT",
    headers: JSON_HEADERS,
    body: JSON.stringify({ codexAccountPickerEnabled: enabled }),
  };
}

/** A save notice is stated as intent here and translated where it is rendered. */
function feedbackKey(kind: AccountPickerFeedbackKind): TKey {
  if (kind === "updated") return UPDATED_KEY;
  return kind === "refresh-pending" ? REFRESH_PENDING_KEY : UPDATE_FAILED_KEY;
}

function feedbackTone(kind: AccountPickerFeedbackKind): NoticeTone {
  if (kind === "updated") return "ok";
  return kind === "refresh-pending" ? "warn" : "err";
}

/**
 * Opt-in control for account-qualified Codex model-picker entries.
 *
 * The card owns its transport — one bounded GET, one PUT, and the visibility poll — while
 * every decision about what those answers mean lives in the picker policy. The state is
 * mirrored into a ref because the async completions have to read the newest revision
 * synchronously: a read that lands while a save is out must be judged against the revision
 * the save claimed, not against whatever React last painted.
 */
export default function CodexAccountPickerSetting({ apiBase }: CodexAccountPickerSettingProps) {
  const translate = useT();
  const [snapshot, dispatch] = useReducer(accountPickerReducer, undefined, initialAccountPickerState);
  const stateRef = useRef(snapshot);

  const send = useCallback((event: AccountPickerEvent) => {
    stateRef.current = accountPickerReducer(stateRef.current, event);
    dispatch(event);
  }, []);

  /** One settings read. A save in flight owns the value, so a read would only race it. */
  const readSettings = useCallback(async () => {
    const opening = stateRef.current;
    if (opening.saving) return;
    const generation = opening.generation + 1;
    send({ kind: "read-began", generation });
    const bounded = createBoundedFetch(READ_TIMEOUT_MS);
    try {
      const response = await fetch(settingsUrl(apiBase), { signal: bounded.signal });
      if (!response.ok) throw new Error("settings read failed");
      const enabled = decodeAccountPickerEnabled(await response.json());
      if (enabled === null) throw new Error("settings read shape");
      send({ kind: "read-arrived", generation, enabled });
    } catch {
      send({ kind: "read-failed", generation });
    } finally {
      bounded.clear();
    }
  }, [apiBase, send]);

  /** One settings write, optimistically painted by the `save-began` transition. */
  const writeSettings = useCallback(async () => {
    const opening = stateRef.current;
    if (opening.saving || !opening.hydrated) return;
    const requested = !opening.enabled;
    send({ kind: "save-began", generation: opening.generation + 1, enabled: requested });
    try {
      const response = await fetch(settingsUrl(apiBase), settingsWrite(requested));
      const body = await readJsonOrThrow<SettingsWriteBody>(response) ?? {};
      const decoded = decodeAccountPickerSave(body);
      if (!decoded.ok) throw new Error("settings write unconfirmed");
      send({
        kind: "save-arrived",
        enabled: decoded.enabled,
        catalogRefreshPending: decoded.catalogRefreshPending,
      });
    } catch {
      send({ kind: "save-failed" });
    }
  }, [apiBase, send]);

  useEffect(() => {
    // Deferred a tick so the first read cannot land inside the mount commit.
    const first = window.setTimeout(() => { void readSettings(); }, 0);
    const stopPolling = startVisibilityPoll(() => { void readSettings(); }, SETTINGS_POLL_MS);
    return () => {
      window.clearTimeout(first);
      stopPolling();
    };
  }, [readSettings]);

  const feedback = snapshot.feedback === null
    ? null
    : { tone: feedbackTone(snapshot.feedback), message: translate(feedbackKey(snapshot.feedback)) };
  const awaitingFirstRead = !snapshot.hydrated
    && !accountPickerInitialLoadFailed(snapshot.loadError, snapshot.hydrated);

  return (
    <div className={CARD_CLASS} aria-busy={snapshot.saving || awaitingFirstRead || undefined}>
      <CodexAccountPickerCopy
        loadError={snapshot.loadError}
        hydrated={snapshot.hydrated}
        enabled={snapshot.enabled}
      />
      <CodexAccountPickerControls
        loadError={snapshot.loadError}
        hydrated={snapshot.hydrated}
        enabled={snapshot.enabled}
        saving={snapshot.saving}
        onRetry={() => { void readSettings(); }}
        onToggle={() => { void writeSettings(); }}
      />
      {feedback && <CodexAccountPickerFeedback feedback={feedback} />}
    </div>
  );
}
