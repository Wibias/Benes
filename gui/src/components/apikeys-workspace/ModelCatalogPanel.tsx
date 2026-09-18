/**
 * Benes dashboard source. The Models tab of the API workspace.
 *
 * The tab draws a model derived by `deriveModelCatalogView`; it decides nothing
 * about the catalog itself. That is what keeps six different sentences apart:
 * a first load, a refresh that must not blank last-good rows, a failed refresh
 * beside those rows, a cold failure where nothing ever loaded, a catalog that is
 * genuinely empty, and a search that matched nothing.
 *
 * The area under the header is one of a small set of semantic variants, so the
 * body is a switch over the variant rather than a stack of conditionals, and
 * each table cell reads its content off the row model instead of deriving it.
 */
import type { ReactNode } from "react";
import { DataSurfaceSkeleton } from "../data-surface";
import {
  deriveModelCatalogView,
  type CatalogColumn,
  type CatalogRowView,
  type CatalogSurface,
  type ExternalModelRow,
  type ModelCatalogState,
  type ProbeChipView,
} from "../../api-access/model-catalog-view";
import type { GatewayInboundProtocol } from "../../api-access/model-tests";
import { useT } from "../../i18n/shared";

/** Everything the tab can ask the page to do. */
export interface ModelCatalogActions {
  readonly search: (value: string) => void;
  readonly copyId: (modelId: string) => void;
  readonly test: (model: ExternalModelRow, protocol: GatewayInboundProtocol) => void;
  readonly retry: () => void;
}

export function ModelCatalogPanel({
  state,
  actions,
}: {
  state: ModelCatalogState;
  actions: ModelCatalogActions;
}) {
  const t = useT();
  const catalog = deriveModelCatalogView(state, t);

  return (
    <div className="api-models-panel awi-models">
      <div className="api-panel-head">
        <h3 className="awi-board-title">{catalog.title}</h3>
        <p className="muted small">{catalog.subtitle}</p>
      </div>
      <input
        type="search"
        className="input awi-search-field"
        value={state.query}
        onChange={event => actions.search(event.target.value)}
        placeholder={catalog.searchPlaceholder}
        aria-label={catalog.searchPlaceholder}
      />
      {catalog.probeNote !== null && <p className="muted small">{catalog.probeNote}</p>}
      {catalog.failure !== null && (
        <div className="api-models-error">
          {/* The page-level notice this replaced was announced, so the message
              that moved next to its retry is announced here too. */}
          <p className="muted small" role="alert">{catalog.failure.message}</p>
          <button type="button" className="btn btn-ghost btn-sm" onClick={actions.retry}>
            {catalog.failure.retryLabel}
          </button>
        </div>
      )}
      {catalog.refreshing !== null && (
        <p className="muted small" aria-live="polite">{catalog.refreshing}</p>
      )}
      <CatalogArea surface={catalog.surface} columns={catalog.columns} actions={actions} />
      {catalog.disabledNote !== null && <p className="muted small">{catalog.disabledNote}</p>}
    </div>
  );
}

/** The catalog area: exactly one variant, plus the shared scroll frame for rows. */
function CatalogArea({
  surface,
  columns,
  actions,
}: {
  surface: CatalogSurface;
  columns: readonly CatalogColumn[];
  actions: ModelCatalogActions;
}): ReactNode {
  switch (surface.kind) {
    case "loading":
      return <DataSurfaceSkeleton label={surface.label} rows={3} />;
    // Failed cold: the error block above is the whole story. Printing "no
    // models" here would assert an empty catalog we never managed to read.
    case "unavailable":
      return null;
    case "empty":
    case "no-match":
      return <p className="muted small api-models-empty">{surface.message}</p>;
    case "rows":
      return <CatalogTable columns={columns} rows={surface.rows} actions={actions} />;
  }
}

function CatalogTable({
  columns,
  rows,
  actions,
}: {
  columns: readonly CatalogColumn[];
  rows: readonly CatalogRowView[];
  actions: ModelCatalogActions;
}) {
  return (
    <div className="api-models-scroll">
      <table className="tbl">
        <thead>
          <tr>
            {columns.map(column => <th key={column.id}>{column.header}</th>)}
          </tr>
        </thead>
        <tbody>
          {rows.map(row => <CatalogRow key={row.id} row={row} actions={actions} />)}
        </tbody>
      </table>
    </div>
  );
}

function CatalogRow({
  row,
  actions,
}: {
  row: CatalogRowView;
  actions: ModelCatalogActions;
}) {
  return (
    <tr>
      <td>
        <div className="api-model-cell">
          <code>{row.id}</code>
          {row.displayName !== undefined && <span className="muted small">{row.displayName}</span>}
        </div>
      </td>
      <td>{row.source}</td>
      <td>
        <div className="api-model-actions">
          <button
            type="button"
            className="btn btn-sm btn-ghost"
            onClick={() => { actions.copyId(row.id); }}
          >
            {row.copyLabel}
          </button>
          {row.chips.map(chip => (
            <ProbeChip
              key={chip.protocol}
              chip={chip}
              onTest={() => { actions.test(row.model, chip.protocol); }}
            />
          ))}
        </div>
      </td>
    </tr>
  );
}

/** One model × protocol probe control, with its own announced result. */
function ProbeChip({ chip, onTest }: { chip: ProbeChipView; onTest: () => void }) {
  return (
    <span className="api-model-test-chip">
      <button
        type="button"
        className="btn btn-sm btn-ghost"
        disabled={chip.disabled}
        title={chip.hint}
        onClick={onTest}
      >
        {chip.label}
      </button>
      {chip.status !== undefined && (
        <span
          className={chip.status.className}
          role="status"
          aria-live="polite"
          aria-atomic="true"
          title={chip.status.detail}
        >
          {chip.status.text}
        </span>
      )}
    </span>
  );
}
