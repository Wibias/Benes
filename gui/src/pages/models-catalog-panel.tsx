/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { ReactNode } from "react";
import { IconBoxes, IconInfo } from "../icons";
import type { TFn } from "../i18n/shared";
import { Notice, EmptyState, Tooltip } from "../ui";

export function ModelsCatalogEmptyState({ t, empty }: { t: TFn; empty: boolean }) {
  if (!empty) return null;
  const title = t("models.noRouted");
  const hint = t("models.noRoutedHint");
  return <EmptyState icon={<IconBoxes />} title={title}>{hint}</EmptyState>;
}

type CatalogColumn = {
  id: "model" | "context" | "visibility";
  className: string;
  heading: (t: TFn, collapseControls: ReactNode) => ReactNode;
};

const COLUMNS: CatalogColumn[] = [
  {
    id: "model",
    className: "models-catalog-col-model",
    heading: (t, collapseControls) => (
      <span className="models-catalog-th-model">
        {t("models.col.model")}
        {collapseControls}
      </span>
    ),
  },
  {
    id: "context",
    className: "models-catalog-col-context",
    heading: t => t("models.col.context"),
  },
  {
    id: "visibility",
    className: "models-catalog-col-visibility",
    heading: t => {
      const hint = t("models.col.visibilityHint");
      return (
        <span className="models-catalog-th-visibility">
          {t("models.col.visibility")}
          <Tooltip content={hint} side="top" maxWidth={320}>
            <span style={{ cursor: "help" }} aria-label={hint}>
              <IconInfo width={13} height={13} aria-hidden="true" />
            </span>
          </Tooltip>
        </span>
      );
    },
  },
];

function CatalogTable({
  t,
  collapseControls,
  providerList,
  empty,
}: {
  t: TFn;
  collapseControls: ReactNode;
  providerList: ReactNode;
  empty: boolean;
}) {
  return (
    <table className="models-catalog-table">
      <colgroup>
        {COLUMNS.map(column => <col key={column.id} className={column.className} />)}
      </colgroup>
      <thead>
        <tr>
          {COLUMNS.map(column => (
            <th key={column.id} scope="col">
              {column.heading(t, collapseControls)}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {providerList}
        {empty ? (
          <tr className="models-catalog-empty-row">
            <td colSpan={COLUMNS.length}>
              <ModelsCatalogEmptyState t={t} empty />
            </td>
          </tr>
        ) : null}
      </tbody>
    </table>
  );
}

function CatalogFooter({
  t,
  counts,
}: {
  t: TFn;
  counts: { providers: number; models: number; visible: number };
}) {
  return (
    <div className="models-catalog-tfoot">
      <span>{t("models.footer.providers", { n: counts.providers })}</span>
      <span className="models-catalog-tfoot-right">
        <span>{t("models.footer.models", { n: counts.models })}</span>
        <span className="models-catalog-tfoot-visible">
          {t("models.footer.visible", { n: counts.visible })}
        </span>
      </span>
    </div>
  );
}

export function ModelsCatalogPanel({
  t,
  showError,
  refreshing,
  empty,
  footer,
  toolbar,
  settings,
  collapseControls,
  providerList,
  modals,
}: {
  t: TFn;
  showError: boolean;
  refreshing: boolean;
  empty: boolean;
  footer: { providers: number; models: number; visible: number };
  toolbar: ReactNode;
  settings: ReactNode;
  collapseControls: ReactNode;
  providerList: ReactNode;
  modals: ReactNode;
}) {
  return (
    <div className="models-workspace-shell">
      {showError ? <Notice tone="err">{t("models.loadFail")}</Notice> : null}
      <div className="models-workspace-root" aria-busy={refreshing || undefined}>
        <section className="models-workspace-main" aria-label={t("models.workspace.mainAria")}>
          {toolbar}
          {settings}
          <div className="models-catalog-table-wrap">
            <div className="models-catalog-table-scroll">
              <CatalogTable
                t={t}
                collapseControls={collapseControls}
                providerList={providerList}
                empty={empty}
              />
            </div>
            <CatalogFooter t={t} counts={footer} />
          </div>
        </section>
      </div>
      {modals}
    </div>
  );
}
