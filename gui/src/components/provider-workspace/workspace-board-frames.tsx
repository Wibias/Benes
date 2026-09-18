import type { ReactNode } from "react";
import { useT } from "../../i18n/shared";
import type { WorkspaceKpis } from "../../provider-workspace/workspace-shell";

export type AddProviderIntent = { tier?: "accounts" | "free" | "paid"; custom?: boolean };

export function BoardKpi({ value, label, tone }: { value: number | null; label: string; tone?: "amber" }) {
  const deviation = tone && value != null && value > 0;
  return (
    <div className={`providers-kpi${deviation ? ` providers-kpi--${tone}` : ""}`}>
      <div className="providers-kpi-value">{value == null ? "—" : value}</div>
      <div className="providers-kpi-label">{label}</div>
    </div>
  );
}

/**
 * The five fleet counters in board order. Every cell is present in every board
 * state; a value the listener does not report yet renders as a dash.
 */
function FleetKpiStrip({ kpis }: { kpis: WorkspaceKpis | null }) {
  const t = useT();
  const cells: Array<{ key: string; value: number | null; label: string; tone?: "amber" }> = [
    { key: "total", value: kpis?.total ?? null, label: t("prov.overview.total") },
    { key: "healthy", value: kpis?.healthy ?? null, label: t("prov.overview.healthy") },
    { key: "attention", value: kpis?.attention ?? null, label: t("prov.kpis.attention"), tone: "amber" },
    { key: "disabled", value: kpis?.disabled ?? null, label: t("prov.overview.disabled") },
    { key: "models", value: kpis?.models ?? null, label: t("prov.overview.exposed") },
  ];
  return (
    <section className="providers-kpis" aria-label={t("prov.kpis.label")}>
      {cells.map(cell => (
        <BoardKpi key={cell.key} value={cell.value} label={cell.label} tone={cell.tone} />
      ))}
    </section>
  );
}

/** Rail + main-pane scaffold every board state shares. */
function WorkspaceSurface({ rail, children }: { rail: ReactNode; children?: ReactNode }) {
  const t = useT();
  return (
    <div className="pws-shell-container">
      <div className="pws-root">
        <aside className="pws-rail" aria-label={t("pws.providerList")}>{rail}</aside>
        <main className="pws-main" aria-label={t("pws.workspaceMainAria")}>{children}</main>
      </div>
    </div>
  );
}

export function ProvidersBoardFrame({ kpis, onAddProvider, children }: {
  kpis: WorkspaceKpis | null;
  onAddProvider: (intent?: AddProviderIntent) => void;
  children: ReactNode;
}) {
  const t = useT();
  return (
    <div className="providers-board">
      <header className="providers-head">
        <h2>{t("nav.providers")}</h2>
        <FleetKpiStrip kpis={kpis} />
        <div className="page-head-actions">
          <button type="button" className="btn providers-add" onClick={() => onAddProvider()}>{t("prov.add")}</button>
        </div>
      </header>
      {children}
    </div>
  );
}

export function WorkspaceLoadingFrame({
  failed,
  onAddProvider,
}: {
  failed: boolean;
  onAddProvider: (intent?: AddProviderIntent) => void;
}) {
  const t = useT();
  return (
    <ProvidersBoardFrame kpis={null} onAddProvider={onAddProvider}>
      <WorkspaceSurface rail={(
        <p className="muted pws-rail-empty" role="status">{failed ? t("prov.loadConfigFail") : t("common.loading")}</p>
      )} />
    </ProvidersBoardFrame>
  );
}

function WorkspaceEmptyState() {
  const t = useT();
  return (
    <div className="pws-empty-root pws-empty-root--quiet">
      <div className="pws-empty-hero pws-empty-hero--quiet">
        <h2>{t("pws.connectFirst")}</h2>
        <ol className="pws-empty-steps">
          <li><strong>{t("prov.add")}</strong><span className="muted">{t("pws.empty.browseFreeDesc")}</span></li>
          <li><strong>{t("pws.empty.connectAccount")}</strong><span className="muted">{t("pws.empty.connectAccountDesc")}</span></li>
          <li><strong>{t("pws.empty.addEndpoint")}</strong><span className="muted">{t("pws.empty.addEndpointDesc")}</span></li>
        </ol>
      </div>
    </div>
  );
}

export function WorkspaceZeroFrame({
  kpis,
  onAddProvider,
}: {
  kpis: WorkspaceKpis | null;
  onAddProvider: (intent?: AddProviderIntent) => void;
}) {
  const t = useT();
  return (
    <ProvidersBoardFrame kpis={kpis} onAddProvider={onAddProvider}>
      <WorkspaceSurface rail={(
        <div className="pws-zero-rail">
          <strong>{t("nav.providers").toUpperCase()} (0)</strong>
          <span>{t("pws.noProvidersConfigured")}</span>
          <span className="muted">{t("pws.connectFirst")}</span>
        </div>
      )}>
        <WorkspaceEmptyState />
      </WorkspaceSurface>
    </ProvidersBoardFrame>
  );
}

export function ProviderWorkspaceBoard({
  boardKind,
  failed,
  kpis,
  onAddProvider,
  children,
}: {
  boardKind: "loading" | "empty" | "ready";
  failed: boolean;
  kpis: WorkspaceKpis | null;
  onAddProvider: (intent?: AddProviderIntent) => void;
  children: ReactNode;
}) {
  if (boardKind === "loading") {
    return <WorkspaceLoadingFrame failed={failed} onAddProvider={onAddProvider} />;
  }
  if (boardKind === "empty") {
    return <WorkspaceZeroFrame kpis={kpis} onAddProvider={onAddProvider} />;
  }
  return (
    <ProvidersBoardFrame kpis={kpis} onAddProvider={onAddProvider}>
      {children}
    </ProvidersBoardFrame>
  );
}
