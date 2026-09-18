import { type KeyboardEvent, type ReactNode } from "react";
import { navigateHash } from "../hash-routing";
import { Trans } from "../i18n/command-chip";
import type { TFn } from "../i18n/shared";
import { IconAlert } from "../icons";
import { EmptyState } from "../ui";
import { DashboardModelsSection } from "./dashboard-models-section";
import { DashboardPlaneBoard } from "./dashboard-plane-board";
import { DashboardProvidersSection } from "./dashboard-providers-section";
import {
  DASHBOARD_TAB_IDS,
  dashboardHashForSection,
  dashboardPanelA11y,
  dashboardSectionAfterTabKey,
  dashboardTabA11y,
  dashboardTabButtonId,
  dashboardWorkspaceChrome,
  type DashboardSection,
} from "./dashboard-tab-nav";
import { useDashboardData } from "./use-dashboard-data";

const UNREACHABLE_PANEL_STYLE = { marginTop: 40 };
const UNREACHABLE_TITLE_STYLE = { color: "var(--red)" };

function activateDashboardSection(section: DashboardSection) {
  navigateHash(dashboardHashForSection(section));
}

function focusDashboardTab(section: DashboardSection) {
  document.getElementById(dashboardTabButtonId(section))?.focus();
}

export default function Dashboard({ apiBase }: { apiBase: string }) {
  const d = useDashboardData(apiBase);
  if (d.error) return <DashboardListenerUnreachable t={d.t} />;
  return <DashboardWorkspace d={d} />;
}

function DashboardListenerUnreachable({ t }: { t: TFn }) {
  const title = t("dash.cannotConnect");
  return (
    <EmptyState
      style={UNREACHABLE_PANEL_STYLE}
      icon={<IconAlert />}
      title={<span style={UNREACHABLE_TITLE_STYLE}>{title}</span>}
    >
      <Trans k="dash.runStart" cmd="benes start" />
    </EmptyState>
  );
}

function dashboardTabClass(selected: boolean): string {
  return selected ? "page-tab page-tab--active" : "page-tab";
}

function buildPanes(d: ReturnType<typeof useDashboardData>): Record<DashboardSection, { label: string; body: ReactNode }> {
  return {
    overview: {
      label: d.t("dash.workspace.overview"),
      body: <DashboardPlaneBoard {...d} />,
    },
    providers: {
      label: d.t("dash.activeProviders"),
      body: <DashboardProvidersSection t={d.t} providers={d.providers} />,
    },
    models: {
      label: d.t("dash.availableModels"),
      body: (
        <DashboardModelsSection
          t={d.t}
          models={d.models}
          modelsLoading={d.modelsLoading}
          modelQuery={d.modelQuery}
          setModelQuery={d.setModelQuery}
          filteredGroups={d.filteredGroups}
          expandedProviders={d.expandedProviders}
          setExpandedProviders={d.setExpandedProviders}
        />
      ),
    },
  };
}

function DashboardWorkspace({ d }: { d: ReturnType<typeof useDashboardData> }) {
  const chrome = dashboardWorkspaceChrome(d.selectedSection);
  const panes = buildPanes(d);
  const active = panes[d.selectedSection] ?? panes.overview;

  function onTabKeyDown(event: KeyboardEvent<HTMLButtonElement>) {
    const nextId = dashboardSectionAfterTabKey(d.selectedSection, event.key);
    if (!nextId) return;
    event.preventDefault();
    activateDashboardSection(nextId);
    focusDashboardTab(nextId);
  }

  return (
    <div className={chrome.shellClass}>
      <div className="page-head">
        <h2>{d.t("nav.dashboard")}</h2>
      </div>
      {chrome.showTablist ? (
        <DashboardTabStrip
          t={d.t}
          selectedSection={d.selectedSection}
          panes={panes}
          onSelect={activateDashboardSection}
          onTabKeyDown={onTabKeyDown}
        />
      ) : null}
      <section className="dashboard-workspace-main" {...dashboardPanelA11y(d.selectedSection, chrome.overview)}>
        {active.body}
      </section>
    </div>
  );
}

function DashboardTabStrip({
  t,
  selectedSection,
  panes,
  onSelect,
  onTabKeyDown,
}: {
  t: TFn;
  selectedSection: DashboardSection;
  panes: Record<DashboardSection, { label: string }>;
  onSelect: (section: DashboardSection) => void;
  onTabKeyDown: (event: KeyboardEvent<HTMLButtonElement>) => void;
}) {
  return (
    <>
      <p className="page-sub">{t("dash.subtitle")}</p>
      <div className="page-tabs" role="tablist" aria-label={t("dash.workspace.sections")}>
        {DASHBOARD_TAB_IDS.map(id => {
          const selected = selectedSection === id;
          return (
            <button
              key={id}
              type="button"
              className={dashboardTabClass(selected)}
              onClick={() => onSelect(id)}
              onKeyDown={onTabKeyDown}
              {...dashboardTabA11y(id, selectedSection)}
            >
              {panes[id].label}
            </button>
          );
        })}
      </div>
    </>
  );
}
