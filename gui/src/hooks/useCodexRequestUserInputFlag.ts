/**
 * Benes dashboard client for the Go proxy (`internal/server`).
 * Read/write state machine for Codex's own `default_mode_request_user_input` flag.
 *
 * The management endpoint flips the flag through the official `codex features` CLI, so a write
 * is not instant and a read that started before it can still carry the old file value. The hook
 * owns that ordering rather than leaving it to the caller:
 *
 * - a read is skipped outright while a write is in flight, and every read holds a ticket so a
 *   slow reply cannot overwrite a newer one;
 * - a write commits the local value first, reports what the listener confirmed, and puts the
 *   previous value back when the write failed.
 */

import { useCallback, useEffect, useRef, useState } from "react";
import { createBoundedFetch } from "../bounded-fetch";
import {
  featureFlagEnabled,
  featureFlagFailureMessage,
  featureFlagSavedMessageKey,
  featureFlagWriteAccepted,
  type FeatureFlagResponse,
} from "../codex-feature-flags";
import { readJsonOrThrow } from "../fetch-json";
import { useT } from "../i18n/shared";
import { startVisibilityPoll } from "../visibility-poll";

const FLAG_PATH = "/api/codex-auth/features/default-mode-request-user-input";

/** A read or write that outlives this deadline counts as failed. */
const REQUEST_TIMEOUT_MS = 15_000;

/** Hidden tabs hold no timer; a tab that comes back re-reads the file at once. */
const POLL_INTERVAL_MS = 30_000;

/** The file value is unknown until a read lands: `reading`, then `ready` or `unreadable`. */
type FlagPhase = "reading" | "ready" | "unreadable";

export interface RequestUserInputFeedback {
  tone: "ok" | "err";
  message: string;
}

export interface RequestUserInputFlagController {
  /** Last value the listener confirmed, or the requested value while a write is in flight. */
  enabled: boolean;
  /** The last read failed; the card says so and refuses writes until a read succeeds. */
  loadError: boolean;
  /** Waiting on the listener — the card is mid-turn. Drives `aria-busy`. */
  busy: boolean;
  /** The toggle must refuse input: mid-turn, unread, or unreadable. */
  locked: boolean;
  feedback: RequestUserInputFeedback | null;
  /** Re-read the flag from scratch (the retry control). */
  refresh(): void;
  /** Request the opposite of the current value. */
  toggle(): void;
}

function endpoint(apiBase: string): string {
  return `${apiBase}${FLAG_PATH}`;
}
/** The stored value; throws when the listener could not read the file. */
async function readFlagValue(url: string, signal: AbortSignal): Promise<boolean> {
  const reply = await fetch(url, { signal });
  if (!reply.ok) throw new Error(`GET ${reply.status}`);
  return featureFlagEnabled(await reply.json() as FeatureFlagResponse);
}

/** Ask the listener to flip the flag; throws unless it confirmed the write. */
async function writeFlagValue(url: string, wanted: boolean): Promise<FeatureFlagResponse> {
  const reply = await fetch(url, {
    method: "PUT",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ enabled: wanted }),
  });
  const body = (await readJsonOrThrow<FeatureFlagResponse>(reply)) ?? {};
  if (!featureFlagWriteAccepted(body)) throw new Error(`PUT ${reply.status}`);
  return body;
}

export function useCodexRequestUserInputFlag(apiBase: string): RequestUserInputFlagController {
  const t = useT();
  const [enabled, setEnabled] = useState(false);
  const [phase, setPhase] = useState<FlagPhase>("reading");
  const [pending, setPending] = useState(false);
  const [feedback, setFeedback] = useState<RequestUserInputFeedback | null>(null);
  const valueRef = useRef(false);
  const writingRef = useRef(false);
  const readTicketRef = useRef(0);

  const commit = useCallback((value: boolean) => {
    valueRef.current = value;
    setEnabled(value);
  }, []);

  const readFlag = useCallback(async () => {
    // A read landing between the optimistic flip and the PUT response would paint the
    // pre-save file value, so a write in flight suppresses reads entirely.
    if (writingRef.current) return;
    const ticket = (readTicketRef.current += 1);
    const current = () => !writingRef.current && ticket === readTicketRef.current;
    const bounded = createBoundedFetch(REQUEST_TIMEOUT_MS);
    try {
      const stored = await readFlagValue(endpoint(apiBase), bounded.signal);
      if (!current()) return;
      commit(stored);
      setPhase("ready");
    } catch {
      if (current()) setPhase("unreadable");
    } finally {
      bounded.clear();
    }
  }, [commit, apiBase]);

  useEffect(() => {
    // Deferred by a zero-delay timer rather than started inside the effect body: the first
    // state update then belongs to a callback, not to the render pass that mounted the card.
    const firstRead = window.setTimeout(() => { void readFlag(); }, 0);
    const stopPolling = startVisibilityPoll(() => { void readFlag(); }, POLL_INTERVAL_MS);
    return () => {
      window.clearTimeout(firstRead);
      stopPolling();
    };
  }, [readFlag]);

  const flipFlag = useCallback(async () => {
    if (writingRef.current || phase !== "ready") return;
    const previous = valueRef.current;
    const requested = !previous;
    commit(requested);
    writingRef.current = true;
    setPending(true);
    setFeedback(null);
    // Any read already in flight describes the file as it was before this write.
    readTicketRef.current += 1;
    try {
      const confirmed = await writeFlagValue(endpoint(apiBase), requested);
      commit(featureFlagEnabled(confirmed));
      setPhase("ready");
      setFeedback({ tone: "ok", message: t(featureFlagSavedMessageKey(confirmed)) });
    } catch (error) {
      commit(previous);
      setFeedback({ tone: "err", message: featureFlagFailureMessage(t, error) });
    } finally {
      writingRef.current = false;
      setPending(false);
    }
  }, [commit, apiBase, phase, t]);

  const refresh = useCallback(() => {
    void readFlag();
  }, [readFlag]);

  const toggle = useCallback(() => {
    void flipFlag();
  }, [flipFlag]);

  return {
    enabled,
    loadError: phase === "unreadable",
    busy: pending || phase === "reading",
    locked: pending || phase !== "ready",
    feedback,
    refresh,
    toggle,
  };
}

