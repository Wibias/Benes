import { useCallback, useEffect, useEffectEvent, useReducer, useRef, useState } from "react";
import { useVirtualizer } from "@tanstack/react-virtual";
import { setClientResourceData, useKeyedClientResource } from "../client-resource";
import { useDataSurface } from "../data-surface";
import type { DataSurfaceState } from "../data-surface";
import { readJsonIfOk, readJsonOrThrow } from "../fetch-json";
import { readSessionListCache, writeSessionListCache } from "../session-list-cache";
import { createBoundedFetch } from "../bounded-fetch";
import { startVisibilityPoll } from "../visibility-poll";
import { useI18n } from "../i18n/shared";
import { DebugClaudeInboundPanel } from "./debug-claude-inbound-panel";
import { DebugLogViewer } from "./debug-log-viewer";
import type { DebugLogVirtualizer } from "./debug-log-viewer";
import { DebugOutputToolbar } from "./debug-settings-panel";
import { DebugSettingsGate } from "./debug-settings-gate";
import { debugLogsPath, debugLogsQuery, mergeDebugLogEntries } from "./debug-logs";
import {
  DEBUG_STREAMS,
  isClaudeInboundApplicable,
  isStreamEnabled,
  type ClaudeInboundEntry,
  type DebugFlag,
  type DebugLogEntry,
  type DebugSettings,
  type LogStream,
} from "./debug-shared";

/** Height and overscan the output pane reserves for one captured line. */
const LOG_ROW_HEIGHT = 20;
const LOG_OVERSCAN_ROWS = 30;

/** Cadence of the settings and inbound polls, in milliseconds. */
const SETTINGS_POLL_MS = 2000;
const INBOUND_POLL_MS = 2000;

/** Cadence of the output poll, and how long one of its ticks may wait for its page. */
const OUTPUT_POLL_MS = 1000;
const OUTPUT_POLL_DEADLINE_MS = 10_000;

/** The stream the pane opens on, and whether it follows new lines until the reader scrolls. */
const OPENING_STREAM: LogStream = "provider";
const FOLLOW_BY_DEFAULT = true;

/** Local-storage key the last accepted settings response is kept under. */
function settingsCacheKey(apiBase: string): string {
  return `benes.debug.settings.v1:${apiBase}`;
}

/** In-memory resource key the settings poll publishes to. */
function settingsResourceKey(apiBase: string): string {
  return `debug-settings:${apiBase}`;
}

/**
 * Read the settings once and keep them current.
 *
 * The accepted response is written to the session cache as well as to the resource, so a reload
 * shows the switches immediately instead of an empty pane.
 */
async function loadSettings(apiBase: string, signal: AbortSignal): Promise<DebugSettings> {
  const response = await fetch(`${apiBase}/api/debug`, { signal });
  const payload = await readJsonOrThrow<DebugSettings>(response);
  if (payload === undefined) throw new Error("debug settings response carried no body");
  writeSessionListCache(settingsCacheKey(apiBase), payload);
  return payload;
}

/** Apply one settings write. `null` stands for a refused or unreadable reply. */
async function putSettings(
  apiBase: string,
  body: Record<string, unknown>,
): Promise<DebugSettings | null> {
  const response = await fetch(`${apiBase}/api/debug`, {
    method: "PUT",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
  });
  return (await readJsonIfOk<DebugSettings>(response)) ?? null;
}

/**
 * A queue that runs one write at a time.
 *
 * The listener applies switches in order, so a second toggle has to wait for the first reply
 * rather than race it. A failed write settles the queue instead of wedging it, and the caller
 * still awaits the task it enqueued.
 */
function createWriteQueue(): (task: () => Promise<void>) => Promise<void> {
  let tail: Promise<void> = Promise.resolve();
  return (task) => {
    const run = tail.then(task, task);
    tail = run.then(() => undefined, () => undefined);
    return run;
  };
}

type DebugSettingsResource = {
  settings: DebugSettings | null;
  state: DataSurfaceState<DebugSettings>;
  busy: boolean;
  refresh: () => void;
  setFlag: (flag: DebugFlag, enabled: boolean) => void;
  reset: () => void;
};

/**
 * The debug switches: their current values, their load state, and the queued writes behind them.
 *
 * Only the newest write may publish its reply, so a slower earlier reply cannot overwrite a
 * switch the reader toggled after it.
 */
function useDebugSettings(apiBase: string, active: boolean): DebugSettingsResource {
  const cacheKey = settingsCacheKey(apiBase);
  const resourceKey = settingsResourceKey(apiBase);
  const cached = readSessionListCache<DebugSettings>(cacheKey);
  const [busy, setBusy] = useState(false);
  const [enqueueWrite] = useState(createWriteQueue);
  const newestWrite = useRef(0);

  const poll = useDataSurface<DebugSettings>(
    resourceKey,
    [apiBase],
    signal => loadSettings(apiBase, signal),
    {
      pollMs: SETTINGS_POLL_MS,
      enabled: active,
      isEmpty: () => false,
      initialData: cached ?? undefined,
    },
  );

  const write = async (body: Record<string, unknown>): Promise<void> => {
    const generation = ++newestWrite.current;
    setBusy(true);
    try {
      await enqueueWrite(async () => {
        const payload = await putSettings(apiBase, body);
        if (!payload || generation !== newestWrite.current) return;
        writeSessionListCache(cacheKey, payload);
        setClientResourceData(resourceKey, payload);
      });
    } catch {
      /* a refused write leaves the switches showing the last accepted values */
    } finally {
      if (generation === newestWrite.current) setBusy(false);
    }
  };

  return {
    settings: poll.data ?? cached ?? null,
    state: poll.state,
    busy,
    refresh: () => poll.refresh(),
    setFlag: (flag, enabled) => { void write({ [flag]: enabled }); },
    reset: () => { void write({ reset: true }); },
  };
}

/**
 * Inbound Claude requests the recorder captured, for the diagnostic table.
 *
 * `claudeFlag` is passed through as the listener reported it, so the resource reloads exactly
 * when the listener starts or stops reporting that switch.
 */
function useClaudeInbound(
  apiBase: string,
  active: boolean,
  claudeFlag: boolean | undefined,
): ClaudeInboundEntry[] {
  const poll = useKeyedClientResource(
    `debug-claude-inbound:${apiBase}`,
    [apiBase, claudeFlag],
    async (signal) => {
      const response = await fetch(`${apiBase}/api/claude/inbound-debug`, { signal });
      const payload = await readJsonIfOk<{ entries?: ClaudeInboundEntry[] }>(response);
      const entries = payload?.entries;
      return Array.isArray(entries) ? entries : [];
    },
    { pollMs: INBOUND_POLL_MS, enabled: active && claudeFlag === true },
  );
  return poll.data ?? [];
}

/**
 * The stream the toolbar should fall back to, or `null` when the current one needs no
 * replacement.
 *
 * Switching a capture off while its stream is on screen would otherwise leave the pane showing
 * a stream the listener no longer records.
 */
function replacementStream(settings: DebugSettings | null, current: LogStream): LogStream | null {
  if (!settings || isStreamEnabled(settings, current)) return null;
  return DEBUG_STREAMS.find(candidate => isStreamEnabled(settings, candidate)) ?? null;
}

/** Transitions the output buffer accepts. */
type PageAction =
  | { kind: "clear" }
  | { kind: "replaced"; page: DebugLogEntry[] }
  | { kind: "appended"; page: DebugLogEntry[] };

/**
 * Buffer transitions for the tail.
 *
 * A first page replaces the buffer outright, because the listener answered a request that
 * carried no cursor; a later page extends it, and the merge caps the result.
 */
function pageBuffer(rows: DebugLogEntry[], action: PageAction): DebugLogEntry[] {
  switch (action.kind) {
    case "clear":
      return [];
    case "replaced":
      return mergeDebugLogEntries(rows, action.page, true);
    case "appended":
      return mergeDebugLogEntries(rows, action.page, false);
  }
}

type DebugLogTail = {
  entries: DebugLogEntry[];
  refreshing: boolean;
  scrollRef: React.RefObject<HTMLDivElement | null>;
  virtualizer: DebugLogVirtualizer;
  refresh: () => void;
};

/**
 * The tail of one debug stream, plus the virtualizer that renders it.
 *
 * The tail owns its cursor and its request generation. A page may only be appended when it
 * belongs to the newest request, so a reply arriving after the stream changed cannot mix one
 * stream's lines into another's buffer. Switching streams, or becoming active with an empty
 * buffer, re-reads the first page; an unchanged stream that already shows lines is left alone,
 * so a pause and resume does not restart the tail.
 */
function useDebugLogTail({
  apiBase,
  stream,
  enabled,
  active,
  follow,
}: {
  apiBase: string;
  stream: LogStream;
  enabled: boolean;
  active: boolean;
  follow: boolean;
}): DebugLogTail {
  const path = debugLogsPath(apiBase, stream);
  const [entries, dispatchPage] = useReducer(pageBuffer, []);
  const [refreshing, setRefreshing] = useState(false);
  const scrollRef = useRef<HTMLDivElement>(null);
  const cursor = useRef(0);
  const newestRequest = useRef(0);
  const session = useRef("");
  const tickInFlight = useRef(false);

  // eslint-disable-next-line react-hooks/incompatible-library -- known useVirtualizer limitation
  const virtualizer = useVirtualizer({
    count: entries.length,
    getScrollElement: () => scrollRef.current,
    estimateSize: () => LOG_ROW_HEIGHT,
    overscan: LOG_OVERSCAN_ROWS,
    getItemKey: index => entries[index]!.seq,
  });

  const fetchPage = useCallback(async (initial: boolean, signal?: AbortSignal): Promise<void> => {
    const request = ++newestRequest.current;
    if (!enabled) {
      if (request === newestRequest.current) {
        dispatchPage({ kind: "clear" });
        cursor.current = 0;
      }
      return;
    }
    setRefreshing(true);
    try {
      const response = await fetch(`${path}?${debugLogsQuery(initial, cursor.current)}`, { signal });
      const page = await readJsonIfOk<DebugLogEntry[]>(response);
      if (signal?.aborted || request !== newestRequest.current) return;
      if (!Array.isArray(page) || page.length === 0) return;
      dispatchPage({ kind: initial ? "replaced" : "appended", page });
      cursor.current = page[page.length - 1]!.seq;
    } catch {
      /* an abandoned or failed page leaves the lines already on screen in place */
    } finally {
      if (request === newestRequest.current) setRefreshing(false);
    }
  }, [path, enabled]);

  useEffect(() => {
    if (!active) return;
    const next = `${apiBase}|${stream}|${enabled}`;
    const switched = session.current !== next;
    session.current = next;
    if (!switched && entries.length > 0) return;
    cursor.current = 0;
    const controller = new AbortController();
    const pending = window.setTimeout(() => {
      if (switched) dispatchPage({ kind: "clear" });
      void fetchPage(true, controller.signal);
    }, 0);
    return () => {
      window.clearTimeout(pending);
      newestRequest.current += 1;
      controller.abort();
    };
    // The identity gate above already scopes this effect to one stream, so re-running it for a
    // new fetchPage or a longer buffer would only restart the tail it is meant to keep.
    // eslint-disable-next-line react-hooks/exhaustive-deps -- stream identity only
    // oxlint-disable-next-line react/react-compiler -- existing exhaustive-deps exception is intentional
  }, [active, apiBase, stream, enabled]);

  const pollTick = useEffectEvent(() => {
    if (tickInFlight.current) return;
    tickInFlight.current = true;
    const bounded = createBoundedFetch(OUTPUT_POLL_DEADLINE_MS);
    void fetchPage(false, bounded.signal).finally(() => {
      bounded.clear();
      tickInFlight.current = false;
    });
  });

  useEffect(() => {
    if (!active || !follow || !enabled) return;
    return startVisibilityPoll(() => pollTick(), OUTPUT_POLL_MS);
  }, [active, follow, enabled]);

  useEffect(() => {
    if (!follow || entries.length === 0) return;
    virtualizer.scrollToIndex(entries.length - 1, { align: "end" });
  }, [entries, follow, virtualizer]);

  return {
    entries,
    refreshing,
    scrollRef,
    virtualizer,
    refresh: () => { void fetchPage(true); },
  };
}

type DebugProps = {
  apiBase: string;
  embedded?: boolean;
  active?: boolean;
};

export default function Debug({ apiBase, embedded = false, active = true }: DebugProps) {
  const { t } = useI18n();
  const [stream, setStream] = useState<LogStream>(OPENING_STREAM);
  const [follow, setFollow] = useState(FOLLOW_BY_DEFAULT);

  const {
    settings: debug,
    state: debugState,
    busy: debugBusy,
    refresh: refreshSettings,
    setFlag,
    reset,
  } = useDebugSettings(apiBase, active);

  const inboundEntries = useClaudeInbound(apiBase, active, debug?.claude);
  const streamEnabled = isStreamEnabled(debug, stream);
  const {
    entries,
    refreshing,
    scrollRef,
    virtualizer,
    refresh: refreshTail,
  } = useDebugLogTail({ apiBase, stream, enabled: streamEnabled, active, follow });

  useEffect(() => {
    const next = replacementStream(debug, stream);
    if (next === null) return;
    const handle = window.setTimeout(() => setStream(next), 0);
    return () => window.clearTimeout(handle);
  }, [debug, stream]);

  return (
    <div className={`debug-board${embedded ? " debug-board--embedded" : ""}`}>
      {!embedded && (
        <div className="page-head">
          <h2>{t("debug.title")}</h2>
        </div>
      )}
      <DebugSettingsGate
        debug={debug}
        debugState={debugState}
        debugBusy={debugBusy}
        onRetry={refreshSettings}
        onSetFlag={setFlag}
        onReset={reset}
      />
      <section className="debug-section">
        <div className="debug-section-head">
          <h3>{t("debug.output")}</h3>
        </div>
        <DebugOutputToolbar
          debug={debug}
          stream={stream}
          refreshing={refreshing}
          follow={follow}
          onStreamChange={setStream}
          onRefresh={refreshTail}
          onFollowChange={setFollow}
        />
        <DebugLogViewer
          visible={debug !== null && streamEnabled}
          stream={stream}
          entries={entries}
          scrollRef={scrollRef}
          virtualizer={virtualizer}
        />
      </section>
      {debug && isClaudeInboundApplicable(debug) && debug.claude && (
        <DebugClaudeInboundPanel entries={inboundEntries} />
      )}
    </div>
  );
}