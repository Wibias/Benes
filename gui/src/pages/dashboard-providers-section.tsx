import type { ReactNode } from "react";
import { Trans } from "../i18n/command-chip";
import type { TFn } from "../i18n/shared";
import { formatProviderDisplayName } from "../provider-icons";
import { EmptyState } from "../ui";
import type { DashboardProviderRow } from "./dashboard-core-poll";

const PROVIDER_INIT_COMMAND = "benes init";

type ProviderRowView = {
  key: string;
  displayName: string;
  adapter: string;
  baseUrl: string;
  modelLabel: string;
};

type ProviderCellView = {
  key: string;
  className: string;
  body: ReactNode;
};

function providerCatalogHeadings(t: TFn): string[] {
  return [t("dash.col.name"), t("dash.col.adapter"), t("dash.col.baseUrl"), t("dash.col.model")];
}

/** Absent adapter, base URL, and default model read as a dash, never as an empty cell. */
function toProviderRowView(provider: DashboardProviderRow, t: TFn): ProviderRowView {
  return {
    key: provider.name,
    displayName: formatProviderDisplayName(provider.name, t),
    adapter: provider.adapter ?? "—",
    baseUrl: provider.baseUrl ?? "—",
    modelLabel: provider.defaultModel ?? "—",
  };
}

function providerRowCells(row: ProviderRowView): ProviderCellView[] {
  return [
    { key: "name", className: "font-semibold", body: row.displayName },
    { key: "adapter", className: "", body: <span className="chip">{row.adapter}</span> },
    { key: "url", className: "muted mono text-label", body: row.baseUrl },
    { key: "model", className: "muted", body: row.modelLabel },
  ];
}

export function DashboardProvidersSection({
  t,
  providers,
}: {
  t: TFn;
  providers: DashboardProviderRow[];
}) {
  return (
    <>
      <SectionCountHeading label={t("dash.activeProviders")} count={providers.length} />
      {providers.length === 0 ? (
        <EmptyState title={<Trans k="dash.noProviders" cmd={PROVIDER_INIT_COMMAND} />} />
      ) : (
        <ProviderInventory
          headings={providerCatalogHeadings(t)}
          rows={providers.map(provider => toProviderRowView(provider, t))}
        />
      )}
    </>
  );
}

function SectionCountHeading({ label, count }: { label: string; count: number }) {
  return (
    <div className="h-section">
      {label} <span className="count">{count}</span>
    </div>
  );
}

function ProviderInventory({
  headings,
  rows,
}: {
  headings: string[];
  rows: ProviderRowView[];
}) {
  return (
    <div className="tbl-wrap">
      <table className="tbl">
        <thead>
          <tr>
            {headings.map(heading => <th key={heading}>{heading}</th>)}
          </tr>
        </thead>
        <tbody>
          {rows.map(row => <ProviderInventoryRow key={row.key} row={row} />)}
        </tbody>
      </table>
    </div>
  );
}

function ProviderInventoryRow({ row }: { row: ProviderRowView }) {
  return (
    <tr>
      {providerRowCells(row).map(cell => (
        <td key={cell.key} className={cell.className || undefined}>{cell.body}</td>
      ))}
    </tr>
  );
}
