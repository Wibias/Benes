/**
 * Benes dashboard source. One export client's row in the Clients panel.
 *
 * The row owns its client's request: the shared keyed store holds one identity
 * per client, so a client that fails keeps its own retry instead of taking the
 * list down with it. What the row prints is resolved by
 * `./client-config-resource` — this file draws that answer and decides nothing
 * about it.
 */
import { useT } from "../../i18n/shared.ts";
import type { ExportClientId } from "../../api-access/export-clients.ts";
import {
  clientConfigRowView,
  clientConfigStatus,
  useClientConfigEnvelope,
  type ClientConfigRowView,
} from "./client-config-resource.ts";

export default function ClientConfigRow({
  client,
  label,
  apiBase,
  selected,
  onSelect,
}: {
  client: ExportClientId;
  label: string;
  apiBase: string;
  selected: boolean;
  onSelect: () => void;
}) {
  const t = useT();
  const resource = useClientConfigEnvelope(apiBase, client);
  const view = clientConfigRowView(clientConfigStatus(resource.state), label, t);
  return (
    <tr className={selected ? "is-selected" : undefined}>
      <td>
        <button type="button" className="awi-row-select" aria-pressed={selected} onClick={onSelect}>
          {label}
        </button>
      </td>
      <td>
        <ClientModelsCell view={view} onRetry={() => resource.refresh({ forceLoading: true })} />
      </td>
    </tr>
  );
}

/** The models cell: the listener's count, its own retry, or a first load. */
function ClientModelsCell({ view, onRetry }: { view: ClientConfigRowView; onRetry: () => void }) {
  if (view.kind === "ready") return view.models;
  if (view.kind === "loading") return <span className="muted">{view.label}</span>;
  return (
    <span className="awi-row-actions">
      <span role="alert">{view.message}</span>
      <button type="button" className="providers-link providers-link--plain" onClick={onRetry}>
        {view.retryLabel}
      </button>
    </span>
  );
}
