/** Benes dashboard client for the Go proxy (`internal/server`). */
import { EmptyState, Notice } from "../ui";
import { useI18n } from "../i18n/shared";
import { SessionsFilters } from "./sessions-filters";
import { SessionsDetail } from "./sessions-detail";
import { SessionsList } from "./sessions-list";
import { SessionsRoutingBanner } from "./sessions-routing-banner";
import { SessionsSearch } from "./sessions-search";
import { useSessionsWorkspace } from "./use-sessions-workspace";

export default function Sessions({ apiBase }: { apiBase: string }) {
  const { t, locale } = useI18n();
  const board = useSessionsWorkspace(apiBase);
  return (
    <div className="sessions-board">
      <div className="page-head">
        <h2>{t("sessions.title")}</h2>
      </div>
      {board.listError === "unavailable" && <Notice tone="err">{t("sessions.unavailable")}</Notice>}
      {board.listError === "generic" && <Notice tone="err">{t("sessions.loadError")}</Notice>}
      <div className="sessions-split">
        <section className="sessions-master" aria-label={t("sessions.list.label")}>
          <div className="sessions-toolbar">
            <SessionsSearch t={t} value={board.search} onChange={board.setSearch} />
            <SessionsFilters
              t={t}
              open={board.filterOpen}
              values={board.filterValues}
              query={board.listQuery}
              onToggle={() => board.setFilterOpen(open => !open)}
              onChange={board.updateFilters}
              onClear={board.clearFilters}
            />
          </div>
          <SessionsRoutingBanner
            t={t}
            policyId={board.routingPolicyId}
            comboId={board.routingComboId}
          />
          {board.listLoading ? (
            <p className="sessions-detail-empty">{t("common.loading")}</p>
          ) : board.sessions.length === 0 ? (
            <EmptyState title={t(board.filteredEmpty ? "sessions.emptyFiltered" : "sessions.empty")} />
          ) : (
            <SessionsList
              t={t}
              locale={locale}
              sessions={board.sessions}
              selectedId={board.activeId}
              hasMore={board.hasMore}
              loadingMore={board.loadingMore}
              onSelect={board.setSelectedId}
              onLoadOlder={() => { void board.loadOlder(); }}
            />
          )}
        </section>
        <SessionsDetail t={t} locale={locale} detail={board.detail} loading={board.detailLoading} />
      </div>
    </div>
  );
}
