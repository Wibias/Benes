/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useEffect, useMemo, useState } from "react";
import { replaceHash } from "../hash-routing";
import {
  SESSION_SEARCH_DEBOUNCE_MS,
  apiErrorCode,
  isStoreUnavailable,
  parseSessionDetail,
  parseSessionFilterValues,
  parseSessionListResult,
  sessionFiltersActive,
  sessionDetailPath,
  sessionFiltersPath,
  sessionsListPath,
  type SessionDetail,
  type SessionFilterValues,
  type SessionListQuery,
  type SessionSummary,
} from "./sessions-contract";
import { sessionsComboFromHash, sessionsPolicyFromHash } from "./sessions-hash-filter";

type ListSnapshot = {
  key: string;
  sessions: SessionSummary[];
  hasMore: boolean;
  nextCursor?: string;
  error: "unavailable" | "generic" | null;
};

type SessionsRoutingFilter = {
  policyId: string;
  comboId: string;
};

const EMPTY_ROUTING_FILTER: SessionsRoutingFilter = { policyId: "", comboId: "" };

function routingFilterFromHash(hash: string): SessionsRoutingFilter {
  return {
    policyId: sessionsPolicyFromHash(hash),
    comboId: sessionsComboFromHash(hash),
  };
}

function useDebouncedSearch(search: string) {
  const [debouncedQ, setDebouncedQ] = useState("");
  useEffect(() => {
    const timer = window.setTimeout(() => setDebouncedQ(search), SESSION_SEARCH_DEBOUNCE_MS);
    return () => window.clearTimeout(timer);
  }, [search]);
  return debouncedQ;
}

function useSessionFilterCatalog(apiBase: string) {
  const [filterValues, setFilterValues] = useState<SessionFilterValues>({
    protocols: [], namespaces: [], providers: [], models: [], policyIds: [], comboIds: [],
  });
  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        const res = await fetch(`${apiBase}${sessionFiltersPath()}`, { signal: controller.signal });
        if (!res.ok) return;
        setFilterValues(parseSessionFilterValues(await res.json()));
      } catch {
        /* keep empty filter metadata */
      }
    })();
    return () => controller.abort();
  }, [apiBase]);
  return filterValues;
}

function useSessionList(apiBase: string, listQuery: SessionListQuery) {
  const queryKey = sessionsListPath(listQuery);
  const [listSnapshot, setListSnapshot] = useState<ListSnapshot>({
    key: "", sessions: [], hasMore: false, error: null,
  });
  const [loadingMore, setLoadingMore] = useState(false);

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        const res = await fetch(`${apiBase}${queryKey}`, { signal: controller.signal });
        const body = await res.json().catch(() => null);
        if (controller.signal.aborted) return;
        if (!res.ok) {
          setListSnapshot({
            key: queryKey,
            sessions: [],
            hasMore: false,
            error: isStoreUnavailable(apiErrorCode(body), res.status) ? "unavailable" : "generic",
          });
          return;
        }
        const parsed = parseSessionListResult(body);
        setListSnapshot({
          key: queryKey,
          sessions: parsed.sessions,
          hasMore: parsed.hasMore,
          nextCursor: parsed.nextCursor,
          error: null,
        });
      } catch (error) {
        if ((error as { name?: string }).name === "AbortError") return;
        setListSnapshot({ key: queryKey, sessions: [], hasMore: false, error: "generic" });
      }
    })();
    return () => controller.abort();
  }, [apiBase, queryKey]);

  async function loadOlder() {
    const nextCursor = listSnapshot.key === queryKey ? listSnapshot.nextCursor : undefined;
    if (!nextCursor || loadingMore) return;
    setLoadingMore(true);
    try {
      const res = await fetch(`${apiBase}${sessionsListPath({ ...listQuery, cursor: nextCursor })}`);
      const body = await res.json().catch(() => null);
      if (!res.ok) return;
      const parsed = parseSessionListResult(body);
      setListSnapshot(current => {
        if (current.key !== queryKey) return current;
        return {
          ...current,
          sessions: [...current.sessions, ...parsed.sessions],
          hasMore: parsed.hasMore,
          nextCursor: parsed.nextCursor,
        };
      });
    } finally {
      setLoadingMore(false);
    }
  }

  const listLoading = listSnapshot.key !== queryKey;
  return {
    queryKey,
    listLoading,
    sessions: listLoading ? [] : listSnapshot.sessions,
    hasMore: !listLoading && listSnapshot.hasMore,
    loadingMore,
    listError: listLoading ? null : listSnapshot.error,
    loadOlder,
  };
}

function useSessionDetail(apiBase: string, activeId: string | null) {
  const [detailSnap, setDetailSnap] = useState<{ id: string; detail: SessionDetail | null } | null>(null);
  useEffect(() => {
    if (!activeId) return;
    const controller = new AbortController();
    void (async () => {
      try {
        const res = await fetch(`${apiBase}${sessionDetailPath(activeId)}`, { signal: controller.signal });
        const body = await res.json().catch(() => null);
        if (controller.signal.aborted) return;
        setDetailSnap({ id: activeId, detail: res.ok ? parseSessionDetail(body) : null });
      } catch (error) {
        if ((error as { name?: string }).name === "AbortError") return;
        setDetailSnap({ id: activeId, detail: null });
      }
    })();
    return () => controller.abort();
  }, [apiBase, activeId]);
  return {
    detail: activeId && detailSnap?.id === activeId ? detailSnap.detail : null,
    detailLoading: Boolean(activeId) && detailSnap?.id !== activeId,
  };
}

export function useSessionsWorkspace(apiBase: string) {
  const [search, setSearch] = useState("");
  const [filters, setFilters] = useState<SessionListQuery>({});
  const [routingFilter, setRoutingFilter] = useState<SessionsRoutingFilter>(() => (
    routingFilterFromHash(typeof window === "undefined" ? "" : window.location.hash)
  ));
  const [filterOpen, setFilterOpen] = useState(false);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const debouncedQ = useDebouncedSearch(search);
  const filterValues = useSessionFilterCatalog(apiBase);

  useEffect(() => {
    const syncRoutingFilter = () => setRoutingFilter(routingFilterFromHash(window.location.hash));
    window.addEventListener("hashchange", syncRoutingFilter);
    return () => window.removeEventListener("hashchange", syncRoutingFilter);
  }, []);

  const clearRoutingFilter = () => {
    if (!routingFilter.policyId && !routingFilter.comboId) return;
    replaceHash("sessions");
    setRoutingFilter(EMPTY_ROUTING_FILTER);
  };

  const updateFilters = (patch: Partial<SessionListQuery>) => {
    if ((routingFilter.policyId && "policy" in patch) || (routingFilter.comboId && "combo" in patch)) {
      clearRoutingFilter();
    }
    setFilters(current => ({ ...current, ...patch }));
  };

  const clearFilters = () => {
    clearRoutingFilter();
    setFilters({});
  };

  const listQuery = useMemo<SessionListQuery>(() => ({
    q: debouncedQ,
    namespace: filters.namespace,
    protocol: filters.protocol,
    provider: filters.provider,
    model: filters.model,
    policy: routingFilter.policyId || filters.policy,
    combo: routingFilter.comboId || filters.combo,
  }), [debouncedQ, filters, routingFilter]);
  const list = useSessionList(apiBase, listQuery);
  const activeId = selectedId && list.sessions.some(row => row.id === selectedId)
    ? selectedId
    : (list.sessions[0]?.id ?? null);
  const { detail, detailLoading } = useSessionDetail(apiBase, activeId);
  return {
    search,
    setSearch,
    listQuery,
    filterOpen,
    setFilterOpen,
    filterValues,
    updateFilters,
    clearFilters,
    routingPolicyId: routingFilter.policyId,
    routingComboId: routingFilter.comboId,
    listLoading: list.listLoading,
    sessions: list.sessions,
    hasMore: list.hasMore,
    loadingMore: list.loadingMore,
    listError: list.listError,
    activeId,
    setSelectedId,
    detail,
    detailLoading,
    filteredEmpty: list.sessions.length === 0 && (Boolean(debouncedQ.trim()) || sessionFiltersActive(listQuery)),
    loadOlder: list.loadOlder,
  };
}
