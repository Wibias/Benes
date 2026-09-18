import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import {
  applyHarnessRemote,
  disableHarnessRemote,
  loadHarnessBoard,
  persistSettings,
  persistSettingsRemote,
  refreshHarnessRemote,
  restoreHarnessRemote,
} from "./live";
import {
  groupHarnesses,
  HARNESS_FILTERS,
  summaryCounts,
  visibleHarnesses,
} from "./model";
import type { SettingsMap } from "./settings-decode";
import { useDataSurface } from "../../data-surface";
import { harnessBoardResourceKey, harnessBoardSessionKey } from "../../nav-board-resources";
import {
  IconFilter,
  IconRefresh,
  IconSearch,
} from "../../icons";
import { useT, type TKey } from "../../i18n/shared";
import { navigateHash, replaceHash } from "../../hash-routing";
import { describeRefusal } from "../integrations/refusal-copy";
import { Notice, ToastNotice } from "../../ui";
import { badgeTone, listBadge } from "./presentation";
import { HarnessDetail, HarnessMark } from "./HarnessDetail";
import { revealHarnessPath, type HarnessRevealTarget } from "./harness-api";
import { harnessHash, harnessShowsSettings, parseHarnessHash, type HarnessDetailTab } from "./hash";
import type { HarnessFilterId, HarnessGroupId, HarnessId, HarnessRecord } from "./types";

const FILTER_LABEL: Record<HarnessFilterId, TKey> = {
  applied: "harnesses.filter.applied",
  "not-applied": "harnesses.filter.notApplied",
  conflict: "harnesses.filter.conflict",
  "update-needed": "harnesses.filter.update",
  "not-installed": "harnesses.filter.notInstalled",
};

const GROUP_LABEL: Record<HarnessGroupId, TKey> = {
  connected: "harnesses.group.connected",
  available: "harnesses.group.available",
  "not-installed": "harnesses.group.notInstalled",
};

function selectedIdFromHash(hash = window.location.hash): HarnessId | null {
  return parseHarnessHash(hash).id;
}

function tabFromHash(hash = window.location.hash): HarnessDetailTab {
  return parseHarnessHash(hash).tab;
}

async function copyText(value: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(value);
    return true;
  } catch {
    return false;
  }
}

export default function HarnessesPage({ apiBase }: { apiBase: string }) {
  const t = useT();
  const searchRef = useRef<HTMLInputElement>(null);
  const activeRowRef = useRef<HTMLButtonElement>(null);
  const loadBoard = useCallback((signal: AbortSignal) => loadHarnessBoard(apiBase, signal), [apiBase]);
  const board = useDataSurface(harnessBoardResourceKey(apiBase), [apiBase], loadBoard, {
    isEmpty: () => false,
    sessionCacheKey: harnessBoardSessionKey(apiBase),
  });
  const [settingsMap, setSettingsMap] = useState<SettingsMap>({});
  const [query, setQuery] = useState("");
  const [filters, setFilters] = useState<Set<HarnessFilterId>>(new Set());
  const [filterOpen, setFilterOpen] = useState(false);
  const [toast, setToast] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  const [selectedId, setSelectedId] = useState<HarnessId | null>(() => selectedIdFromHash());
  const [tab, setTab] = useState<HarnessDetailTab>(() => tabFromHash());
  const harnesses = useMemo(
    () => (board.state.data ?? []).map((row) => (
      settingsMap[row.id] ? { ...row, settings: { ...row.settings, ...settingsMap[row.id] } } : row
    )),
    [board.state.data, settingsMap],
  );

  const visible = useMemo(
    () => visibleHarnesses(harnesses, query, filters, (harness) => t(harness.nameKey)),
    [harnesses, query, filters, t],
  );
  const groups = useMemo(() => groupHarnesses(visible), [visible]);
  const counts = useMemo(() => summaryCounts(harnesses), [harnesses]);
  const selected = harnesses.find((harness) => harness.id === selectedId) ?? visible[0] ?? harnesses[0] ?? null;
  const resolvedId = selected?.id ?? null;

  useEffect(() => {
    if (!resolvedId) return;
    const nextTab = harnessShowsSettings(resolvedId) ? tab : "overview";
    const wanted = harnessHash(resolvedId, nextTab);
    const current = parseHarnessHash(window.location.hash);
    if (current.id !== resolvedId || current.tab !== nextTab) {
      navigateHash(wanted);
    }
  }, [resolvedId, tab]);

  useEffect(() => {
    const sync = () => {
      const parsed = parseHarnessHash(window.location.hash);
      setSelectedId(parsed.id);
      setTab(parsed.tab);
      if (parsed.id && parsed.tab === "settings" && !harnessShowsSettings(parsed.id)) {
        replaceHash(harnessHash(parsed.id, "overview"));
      }
    };
    window.addEventListener("hashchange", sync);
    return () => window.removeEventListener("hashchange", sync);
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === "k") {
        event.preventDefault();
        searchRef.current?.focus();
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, []);

  useEffect(() => {
    // Keep the selected harness readable inside the rail's own scroller. `nearest` only moves
    // this list, and only when the row is actually outside it.
    activeRowRef.current?.scrollIntoView({ block: "nearest" });
  }, [resolvedId]);

  const showToast = useCallback((ok: boolean, text: string) => {
    setToast({ ok, text });
    window.setTimeout(() => {
      setToast((current) => (current?.text === text ? null : current));
    }, ok ? 4500 : 8000);
  }, []);

  const copyPath = useCallback(async (value: string) => {
    const ok = await copyText(value);
    showToast(ok, ok ? t("harnesses.copied") : t("harnesses.copyFailed"));
    return ok;
  }, [showToast, t]);

  const openPath = useCallback(async (harness: HarnessRecord, target: HarnessRevealTarget, path: string) => {
    try {
      await revealHarnessPath(apiBase, harness.id, target);
      showToast(true, t("harnesses.opened"));
    } catch (error) {
      const copied = await copyText(path);
      const detail = error instanceof Error && error.message ? error.message : t("harnesses.openFailed");
      showToast(false, copied ? `${detail} — ${t("harnesses.copied")}` : detail);
    }
  }, [apiBase, showToast, t]);

  const run = async (action: () => Promise<void>, okText: string) => {
    if (busy) return;
    setBusy(true);
    try {
      await action();
      await board.refresh();
      showToast(true, okText);
    } catch (error) {
      showToast(false, describeRefusal(t, error, t("harnesses.actionFailed")));
    } finally {
      setBusy(false);
    }
  };

  const onRefresh = () => {
    board.refresh();
    showToast(true, t("harnesses.refreshed"));
  };

  const toggleFilter = (id: HarnessFilterId) => {
    setFilters((current) => {
      const next = new Set(current);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  return (
    <div className="harnesses-board">
      <header className="harnesses-head">
        <h2>{t("nav.harnesses")}</h2>
        <section className="harnesses-kpis" aria-label={t("harnesses.summary.label")}>
          <KpiStat label={t("harnesses.summary.active")} value={counts.active} />
          <KpiStat label={t("harnesses.summary.available")} value={counts.available} />
          <KpiStat label={t("harnesses.summary.conflict")} value={counts.conflict} tone="amber" />
          <KpiStat label={t("harnesses.summary.update")} value={counts.updateNeeded} />
          <KpiStat label={t("harnesses.summary.total")} value={counts.total} />
        </section>
        <div className="page-head-actions">
          <button
            type="button"
            className="btn btn-ghost btn-icon"
            onClick={onRefresh}
            aria-label={t("harnesses.refresh")}
          >
            <IconRefresh />
          </button>
        </div>
      </header>

      {board.state.showError && (
        <Notice tone="err">{describeRefusal(t, board.state.error, t("harnesses.loadFailed"))}</Notice>
      )}
      {toast && (
        <ToastNotice
          tone={toast.ok ? "ok" : "err"}
          dismissLabel={t("common.close")}
          onDismiss={() => setToast(null)}
        >
          {toast.text}
        </ToastNotice>
      )}

      <div className="harnesses-split">
        <section className="harnesses-rail" aria-label={t("harnesses.list.label")}>
          <div className="harnesses-search-row">
            <label className="harnesses-search">
              <IconSearch />
              <span className="sr-only">{t("harnesses.search")}</span>
              <input
                ref={searchRef}
                value={query}
                onChange={(event) => setQuery(event.target.value)}
                placeholder={t("harnesses.search")}
              />
              <kbd>{t("harnesses.searchShortcut")}</kbd>
            </label>
            <div className="harnesses-filter">
              <button
                type="button"
                className={`btn btn-ghost btn-icon${filters.size ? " is-active" : ""}`}
                aria-expanded={filterOpen}
                aria-haspopup="dialog"
                onClick={() => setFilterOpen((open) => !open)}
                aria-label={t("harnesses.filter")}
              >
                <IconFilter />
              </button>
              {filterOpen && (
                <div className="harnesses-filter-pop" role="dialog" aria-label={t("harnesses.filter")}>
                  {HARNESS_FILTERS.map((id) => (
                    <label key={id} className="harnesses-filter-item">
                      <input type="checkbox" checked={filters.has(id)} onChange={() => toggleFilter(id)} />
                      {t(FILTER_LABEL[id])}
                    </label>
                  ))}
                  <button
                    type="button"
                    className="btn btn-ghost"
                    onClick={() => setFilters(new Set())}
                    disabled={filters.size === 0}
                  >
                    {t("harnesses.filterClear")}
                  </button>
                </div>
              )}
            </div>
          </div>

          <div className="harnesses-groups">
            {(["connected", "available", "not-installed"] as const).map((group) => (
              groups[group].length === 0 ? null : (
                <div key={group} className="harnesses-group">
                  <h3>
                    {t(GROUP_LABEL[group])}
                    <span>({groups[group].length})</span>
                  </h3>
                  <ul>
                    {groups[group].map((harness) => {
                      const active = selected?.id === harness.id;
                      return (
                        <li key={harness.id}>
                          <button
                            ref={active ? activeRowRef : undefined}
                            type="button"
                            className={`harnesses-row${active ? " is-active" : ""}${harness.issue === "conflict" ? " is-conflict" : ""}`}
                            aria-current={active ? "true" : undefined}
                            onClick={() => {
                              setSelectedId(harness.id);
                              setTab("overview");
                              navigateHash(harnessHash(harness.id, "overview"));
                            }}
                          >
                            <HarnessMark id={harness.id} compact />
                            <span className="harnesses-row-name">{t(harness.nameKey)}</span>
                            <span className={`harnesses-row-badge harnesses-row-badge--${badgeTone(harness)}`}>{t(listBadge(harness))}</span>
                          </button>
                        </li>
                      );
                    })}
                  </ul>
                </div>
              )
            ))}
            {visible.length === 0 && <p className="page-sub">{t("harnesses.emptyList")}</p>}
          </div>
          <p className="harnesses-rail-foot">{t("harnesses.count", { count: String(visible.length) })}</p>
        </section>

        {selected ? (
          <HarnessDetail
            key={selected.id}
            harness={selected}
            busy={busy}
            tab={harnessShowsSettings(selected.id) ? tab : "overview"}
            apiBase={apiBase}
            onApply={() => void run(() => applyHarnessRemote(apiBase, selected.id), t("harnesses.applied"))}
            onDisable={() => void run(() => disableHarnessRemote(apiBase, selected.id), t("harnesses.disabled"))}
            onRefresh={() => void run(() => refreshHarnessRemote(apiBase, selected.id), t("harnesses.reapplied"))}
            onRestore={(opId) => void run(() => restoreHarnessRemote(apiBase, opId), t("harnesses.restored"))}
            onSetting={(patch) => {
              setSettingsMap(persistSettings(selected.id, patch));
              void persistSettingsRemote(apiBase, selected.id, patch).then(
                () => showToast(true, t("harnesses.settingsSaved")),
                (error) => showToast(false, describeRefusal(t, error, t("harnesses.actionFailed"))),
              );
            }}
            onCopy={(value) => { void copyPath(value); }}
            onOpenPath={(target, path) => { void openPath(selected, target, path); }}
            onTab={(next) => {
              setTab(next);
              navigateHash(harnessHash(selected.id, next));
            }}
            onToast={showToast}
            onSidecarRefetch={() => board.refresh()}
          />
        ) : (
          <p className="page-sub harnesses-empty-detail">{t("harnesses.emptyDetail")}</p>
        )}
      </div>
    </div>
  );
}

function KpiStat({
  label,
  value,
  tone,
}: {
  label: string;
  value: number;
  tone?: "amber";
}) {
  const deviation = tone && value > 0;
  return (
    <div className={`harnesses-kpi${deviation ? ` harnesses-kpi--${tone}` : ""}`}>
      <div className="harnesses-kpi-value">{value}</div>
      <div className="harnesses-kpi-label">{label}</div>
    </div>
  );
}
