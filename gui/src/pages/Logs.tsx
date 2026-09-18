import { useEffect, useState } from "react";
import { LOCALES, useI18n } from "../i18n/shared";
import { EmptyState, Notice } from "../ui";
import { DataSurfaceSkeleton } from "../components/data-surface";
import Debug from "./Debug";
import type { LogsTab } from "./logs-tab-keydown";
import { logsTabElementId, logsTabKeyDown, readTabFromHash, selectLogsTab } from "./logs-tab-keydown";
import { DiagnosticsDetail } from "./diagnostics-detail";
import { DiagnosticsFilters } from "./diagnostics-filters";
import { DiagnosticsTable } from "./diagnostics-table";
import { useDiagnosticsWorkspace } from "./use-diagnostics-workspace";

/** The Diagnostics request board, as this page reads it. */
type DiagnosticsBoard = ReturnType<typeof useDiagnosticsWorkspace>;

/**
 * The Diagnostics workspace tabs, in render order.
 *
 * One row per tab names the panel it controls and the copy it shows. The strip and the panels
 * are two renders of this list, so a tab cannot end up addressing another tab's panel.
 */
const LOGS_TABS = [
  { tab: "logs", panelId: "logs-panel-logs", labelKey: "logs.tabLogs" },
  { tab: "debug", panelId: "logs-panel-debug", labelKey: "logs.tabDebug" },
] as const;

const [REQUESTS_TAB, DEBUG_TAB] = LOGS_TABS;

/** Placeholder rows the request list reserves while its first page is loading. */
const REQUEST_PLACEHOLDER_ROWS = 6;

type LogsTabState = { tab: LogsTab; debugMounted: boolean };

/** Read the address bar once, for the first render. */
function initialTabState(): LogsTabState {
  const tab = readTabFromHash();
  return { tab, debugMounted: tab === DEBUG_TAB.tab };
}

/** Subscribe to address-bar changes for as long as the page stays mounted. */
function onHashChange(listener: () => void): () => void {
  window.addEventListener("hashchange", listener);
  return () => window.removeEventListener("hashchange", listener);
}

/**
 * The active Diagnostics tab, owned by the address bar.
 *
 * The Debug panel holds polls and a virtualized scroll view, so it is mounted the first time its
 * tab becomes active and never unmounted again: leaving the tab must not tear capture state
 * down. Both fields are written in one update, so the panel mounts in the same commit as the tab
 * that reveals it.
 */
function useLogsTab(): LogsTabState {
  const [state, setState] = useState<LogsTabState>(initialTabState);

  useEffect(() => onHashChange(() => {
    setState(previous => {
      const tab = readTabFromHash();
      return tab === DEBUG_TAB.tab ? { tab, debugMounted: true } : { ...previous, tab };
    });
  }), []);

  return state;
}

/** The `lang` attribute the date and time formatters should render with. */
function htmlLangFor(locale: string): string | undefined {
  return LOCALES.find(item => item.code === locale)?.htmlLang;
}

/** The page head and the tab strip it introduces. */
function DiagnosticsTabStrip({ tab, onSelectTab }: { tab: LogsTab; onSelectTab: (tab: LogsTab) => void }) {
  const { t } = useI18n();
  return (
    <>
      <div className="page-head">
        <h2>{t("nav.diagnostics")}</h2>
      </div>
      <div className="page-tabs" role="tablist" aria-label={t("nav.diagnostics")}>
        {LOGS_TABS.map(item => {
          const active = item.tab === tab;
          return (
            <button
              key={item.tab}
              type="button"
              role="tab"
              id={logsTabElementId(item.tab)}
              aria-selected={active}
              aria-controls={item.panelId}
              tabIndex={active ? 0 : -1}
              className={active ? "page-tab page-tab--active" : "page-tab"}
              onClick={() => onSelectTab(item.tab)}
              onKeyDown={logsTabKeyDown}
            >
              {t(item.labelKey)}
            </button>
          );
        })}
      </div>
    </>
  );
}

/**
 * One tabpanel.
 *
 * The panel id and the tab that owns it come from the descriptor the strip renders, so
 * `labelledby` cannot point at a button that does not exist.
 */
function WorkspacePanel({
  spec,
  active,
  className,
  children,
}: {
  spec: (typeof LOGS_TABS)[number];
  active: LogsTab;
  className: string;
  children: React.ReactNode;
}) {
  return (
    <div
      role="tabpanel"
      id={spec.panelId}
      aria-labelledby={logsTabElementId(spec.tab)}
      hidden={spec.tab !== active}
      className={className}
    >
      {children}
    </div>
  );
}

/** Retry affordance for a request list whose last load failed. */
function RequestsLoadFailure({ onRetry, retrying }: { onRetry: () => void; retrying: boolean }) {
  const { t } = useI18n();
  return (
    <Notice tone="err">
      {t("logs.loadError")}{" "}
      <button type="button" className="btn btn-ghost btn-sm" onClick={onRetry} disabled={retrying}>
        {t("common.retry")}
      </button>
    </Notice>
  );
}

/** The request list in the three states it can be in: loading, empty, or rows. */
function RequestsList({ board }: { board: DiagnosticsBoard }) {
  const { t, locale } = useI18n();
  if (board.loading && board.rows.length === 0) {
    return <DataSurfaceSkeleton label={t("common.loading")} rows={REQUEST_PLACEHOLDER_ROWS} />;
  }
  if (board.rows.length === 0) {
    return <EmptyState title={t(board.sessionId ? "logs.empty.sessionRing" : "logs.noRequests")} />;
  }
  return (
    <DiagnosticsTable
      t={t}
      locale={locale}
      localeTag={htmlLangFor(locale)}
      serverTimeZone={board.serverTimeZone}
      rows={board.rows}
      selectedId={board.selectedId}
      hasOlder={board.hasOlder}
      loadingOlder={board.loadingOlder}
      onSelect={board.setSelectedId}
      onLoadOlder={() => { void board.loadOlder(); }}
    />
  );
}

type LogsProps = { apiBase: string };

export default function Logs({ apiBase }: LogsProps) {
  const { t, locale } = useI18n();
  const { tab, debugMounted } = useLogsTab();
  const board = useDiagnosticsWorkspace(apiBase, tab === REQUESTS_TAB.tab);
  const localeTag = htmlLangFor(locale);

  return (
    <div className="diagnostics-board">
      <DiagnosticsTabStrip tab={tab} onSelectTab={selectLogsTab} />
      {debugMounted && (
        <WorkspacePanel spec={DEBUG_TAB} active={tab} className="diagnostics-debug-panel">
          <Debug apiBase={apiBase} embedded active={tab === DEBUG_TAB.tab} />
        </WorkspacePanel>
      )}
      <WorkspacePanel spec={REQUESTS_TAB} active={tab} className="diagnostics-requests-panel">
        <DiagnosticsFilters
          t={t}
          filters={board.filters}
          sessionId={board.sessionId}
          options={board.filterOptions}
          autoRefresh={board.autoRefresh}
          refreshing={board.loading}
          onChange={board.setFilters}
          onClear={board.clearFilters}
          onAutoRefresh={board.setAutoRefresh}
          onRefresh={board.refresh}
        />
        {board.error && (
          <RequestsLoadFailure onRetry={board.refresh} retrying={board.loading} />
        )}
        <div className="diagnostics-split">
          <section className="diagnostics-master" aria-label={t("logs.list.label")}>
            <RequestsList board={board} />
          </section>
          <DiagnosticsDetail
            t={t}
            locale={locale}
            localeTag={localeTag}
            serverTimeZone={board.serverTimeZone}
            detail={board.detail}
            loading={board.detailLoading}
            error={board.detailError}
          />
        </div>
      </WorkspacePanel>
    </div>
  );
}