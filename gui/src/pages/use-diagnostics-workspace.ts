import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { startVisibilityPoll } from "../visibility-poll";
import {
  DIAGNOSTICS_MAX_LIMIT,
  DIAGNOSTICS_PAGE_SIZE,
  type DiagnosticsRequestDetail,
  type DiagnosticsRequestSummary,
  type DiagnosticsToolbarFilters,
  EMPTY_DIAGNOSTICS_FILTERS,
  diagnosticsDetailPath,
  diagnosticsListPath,
  mergeDiagnosticsRows,
  nextSnapshotLimit,
  parseDiagnosticsDetail,
  parseDiagnosticsList,
  rowInTimeRange,
  serverListQueryFromFilters,
  snapshotHasOlder,
  uniqueSorted,
} from "./diagnostics-contract";
import { logsSessionIdFromHash } from "./logs-session-filter";

const POLL_MS = 2000;

type ListState = {
  rows: DiagnosticsRequestSummary[];
  cursor: string;
  limit: number;
  hasOlder: boolean;
  error: string | null;
};

const EMPTY_LIST: ListState = {
  rows: [],
  cursor: "",
  limit: DIAGNOSTICS_PAGE_SIZE,
  hasOlder: false,
  error: null,
};

function sessionIdNow(): string {
  return typeof window === "undefined" ? "" : logsSessionIdFromHash(window.location.hash);
}

async function readJson(res: Response): Promise<unknown> {
  return res.json().catch(() => null);
}

async function fetchList(
  apiBase: string,
  filters: DiagnosticsToolbarFilters,
  sessionId: string,
  extras: { cursor?: string; limit: number },
  signal?: AbortSignal,
) {
  const path = diagnosticsListPath(serverListQueryFromFilters(filters, sessionId, extras));
  const res = await fetch(`${apiBase}${path}`, { signal });
  const parsed = res.ok ? parseDiagnosticsList(await readJson(res)) : null;
  return {
    rows: parsed?.requests ?? [],
    cursor: parsed?.nextCursor ?? "",
    reset: parsed?.reset === true,
    truncated: parsed?.historyTruncated === true,
    ok: res.ok,
  };
}

export function useDiagnosticsWorkspace(apiBase: string, active: boolean) {
  const [sessionId, setSessionId] = useState(sessionIdNow);
  const [filters, setFilters] = useState<DiagnosticsToolbarFilters>(EMPTY_DIAGNOSTICS_FILTERS);
  const [autoRefresh, setAutoRefresh] = useState(true);
  const [list, setList] = useState<ListState>(EMPTY_LIST);
  const [optionRows, setOptionRows] = useState<DiagnosticsRequestSummary[]>([]);
  const [loading, setLoading] = useState(true);
  const [loadingOlder, setLoadingOlder] = useState(false);
  const [chosenId, setChosenId] = useState<string | null>(null);
  const [detail, setDetail] = useState<DiagnosticsRequestDetail | null>(null);
  const [detailError, setDetailError] = useState<{ id: string; code: string } | null>(null);
  const [serverTimeZone, setServerTimeZone] = useState<string | undefined>();
  const [clockMs, setClockMs] = useState(0);
  const cursorRef = useRef("");
  const limitRef = useRef(DIAGNOSTICS_PAGE_SIZE);
  const rowsRef = useRef<DiagnosticsRequestSummary[]>([]);
  const hasOlderRef = useRef(false);
  const pollInFlight = useRef(false);

  const applyList = useCallback((next: ListState) => {
    cursorRef.current = next.cursor;
    limitRef.current = next.limit;
    rowsRef.current = next.rows;
    hasOlderRef.current = next.hasOlder;
    setList(next);
  }, []);

  const queryFilters = useMemo<DiagnosticsToolbarFilters>(() => ({
    timeRange: "all",
    status: filters.status,
    protocol: filters.protocol,
    provider: filters.provider,
    model: filters.model,
  }), [filters.model, filters.protocol, filters.provider, filters.status]);

  useEffect(() => {
    const onHash = () => setSessionId(sessionIdNow());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);

  useEffect(() => {
    const controller = new AbortController();
    void fetch(`${apiBase}/api/settings`, { signal: controller.signal })
      .then(res => (res.ok ? res.json() as Promise<{ timeZone?: unknown }> : null))
      .then(body => {
        if (typeof body?.timeZone === "string" && body.timeZone.trim()) {
          setServerTimeZone(body.timeZone.trim());
        }
      })
      .catch(() => {});
    return () => controller.abort();
  }, [apiBase]);

  const loadFilterOptions = useCallback(async (signal?: AbortSignal) => {
    try {
      const result = await fetchList(
        apiBase,
        EMPTY_DIAGNOSTICS_FILTERS,
        sessionId,
        { limit: DIAGNOSTICS_MAX_LIMIT },
        signal,
      );
      if (signal?.aborted || !result.ok) return;
      setOptionRows(mergeDiagnosticsRows([], result.rows, "snapshot"));
    } catch (error) {
      if ((error as { name?: string }).name === "AbortError") return;
    }
  }, [apiBase, sessionId]);

  const loadSnapshot = useCallback(async (limit: number, signal?: AbortSignal) => {
    const snapshotLimit = filters.timeRange === "all" ? limit : DIAGNOSTICS_MAX_LIMIT;
    try {
      const result = await fetchList(apiBase, queryFilters, sessionId, { limit: snapshotLimit }, signal);
      if (signal?.aborted) return;
      if (!result.ok) {
        applyList({ ...EMPTY_LIST, error: "http" });
        return;
      }
      applyList({
        rows: mergeDiagnosticsRows([], result.rows, "snapshot"),
        cursor: result.cursor,
        limit: snapshotLimit,
        hasOlder: snapshotHasOlder(result.rows.length, snapshotLimit),
        error: null,
      });
    } catch (error) {
      if ((error as { name?: string }).name === "AbortError") return;
      applyList({ ...EMPTY_LIST, error: "network" });
    } finally {
      if (!signal?.aborted) setLoading(false);
    }
  }, [apiBase, applyList, filters.timeRange, queryFilters, sessionId]);

  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    const timeout = window.setTimeout(() => {
      setLoading(true);
      void loadSnapshot(DIAGNOSTICS_PAGE_SIZE, controller.signal);
    }, 0);
    return () => {
      window.clearTimeout(timeout);
      controller.abort();
    };
  }, [active, loadSnapshot]);

  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    const timeout = window.setTimeout(() => {
      void loadFilterOptions(controller.signal);
    }, 0);
    return () => {
      window.clearTimeout(timeout);
      controller.abort();
    };
  }, [active, filters.timeRange, loadFilterOptions, queryFilters]);

  const pollNewer = useCallback(async () => {
    if (pollInFlight.current || !cursorRef.current) return;
    pollInFlight.current = true;
    try {
      const result = await fetchList(apiBase, queryFilters, sessionId, {
        cursor: cursorRef.current,
        limit: DIAGNOSTICS_PAGE_SIZE,
      });
      if (!result.ok) return;
      if (result.reset || result.truncated) {
        setLoading(true);
        await loadSnapshot(limitRef.current || DIAGNOSTICS_PAGE_SIZE);
        return;
      }
      if (result.rows.length === 0) {
        cursorRef.current = result.cursor || cursorRef.current;
        return;
      }
      setOptionRows(current => mergeDiagnosticsRows(current, result.rows, "incremental"));
      applyList({
        rows: mergeDiagnosticsRows(rowsRef.current, result.rows, "incremental"),
        cursor: result.cursor || cursorRef.current,
        limit: limitRef.current,
        hasOlder: hasOlderRef.current,
        error: null,
      });
    } catch {
      return;
    } finally {
      pollInFlight.current = false;
    }
  }, [apiBase, applyList, loadSnapshot, queryFilters, sessionId]);

  useEffect(() => {
    if (!active || !autoRefresh) return;
    return startVisibilityPoll(() => { void pollNewer(); }, POLL_MS);
  }, [active, autoRefresh, pollNewer]);

  useEffect(() => {
    if (filters.timeRange === "all") return;
    const timer = window.setInterval(() => setClockMs(Date.now()), 30_000);
    return () => window.clearInterval(timer);
  }, [filters.timeRange]);

  const loadOlder = useCallback(async () => {
    if (loadingOlder || !list.hasOlder) return;
    const limit = nextSnapshotLimit(list.limit);
    setLoadingOlder(true);
    try {
      const result = await fetchList(apiBase, queryFilters, sessionId, { limit });
      if (!result.ok) return;
      applyList({
        rows: mergeDiagnosticsRows([], result.rows, "snapshot"),
        cursor: result.cursor,
        limit,
        hasOlder: snapshotHasOlder(result.rows.length, limit),
        error: null,
      });
    } catch {
      return;
    } finally {
      setLoadingOlder(false);
    }
  }, [apiBase, applyList, list.hasOlder, list.limit, loadingOlder, queryFilters, sessionId]);

  const visibleRows = useMemo(() => {
    if (filters.timeRange === "all" || clockMs === 0) return list.rows;
    return list.rows.filter(row => rowInTimeRange(row, filters.timeRange, clockMs));
  }, [clockMs, filters.timeRange, list.rows]);

  const selectedId = chosenId && visibleRows.some(row => row.requestId === chosenId)
    ? chosenId
    : visibleRows[0]?.requestId ?? null;

  useEffect(() => {
    if (!active || !selectedId) return;
    const controller = new AbortController();
    const requestId = selectedId;
    void (async () => {
      try {
        const res = await fetch(`${apiBase}${diagnosticsDetailPath(requestId)}`, { signal: controller.signal });
        const body = await readJson(res);
        if (controller.signal.aborted) return;
        if (res.status === 404) {
          setDetail(null);
          setDetailError({ id: requestId, code: "request_not_retained" });
          return;
        }
        const parsed = parseDiagnosticsDetail(body);
        setDetail(parsed);
        setDetailError(parsed ? null : { id: requestId, code: "invalid" });
      } catch (error) {
        if ((error as { name?: string }).name === "AbortError") return;
        setDetail(null);
        setDetailError({ id: requestId, code: "network" });
      }
    })();
    return () => controller.abort();
  }, [active, apiBase, selectedId]);

  const filterScopeRows = useMemo(() => {
    const rows = mergeDiagnosticsRows(optionRows, list.rows, "incremental");
    if (filters.timeRange === "all" || clockMs === 0) return rows;
    return rows.filter(row => rowInTimeRange(row, filters.timeRange, clockMs));
  }, [clockMs, filters.timeRange, list.rows, optionRows]);

  const filterOptions = useMemo(() => ({
    statuses: uniqueSorted(filterScopeRows.map(row => String(row.status))),
    protocols: uniqueSorted(filterScopeRows.map(row => row.protocol)),
    providers: uniqueSorted(filterScopeRows.map(row => row.provider)),
    models: uniqueSorted(filterScopeRows.map(row => row.resolvedModel)),
  }), [filterScopeRows]);

  return {
    sessionId,
    filters,
    setFilters: (patch: Partial<DiagnosticsToolbarFilters>) => {
      if (patch.timeRange && patch.timeRange !== "all") setClockMs(Date.now());
      setFilters(current => ({ ...current, ...patch }));
    },
    clearFilters: () => setFilters(EMPTY_DIAGNOSTICS_FILTERS),
    autoRefresh,
    setAutoRefresh,
    rows: visibleRows,
    hasOlder: list.hasOlder,
    loading,
    loadingOlder,
    error: list.error,
    selectedId,
    setSelectedId: setChosenId,
    detail: selectedId && detail?.requestId === selectedId ? detail : null,
    detailError: selectedId && detailError?.id === selectedId ? detailError.code : null,
    detailLoading: Boolean(selectedId) && detail?.requestId !== selectedId && detailError?.id !== selectedId,
    serverTimeZone,
    filterOptions,
    refresh: () => {
      setLoading(true);
      void loadSnapshot(limitRef.current || DIAGNOSTICS_PAGE_SIZE);
      void loadFilterOptions();
    },
    loadOlder,
  };
}
