/**
 * Benes dashboard source. The Clients panel: the export clients the listener
 * serves, and the generated file for whichever one is selected.
 *
 * The list is the export authority's own order (`api-access/export-clients`), so
 * a client the listener can serve is a row here and nothing else; the search
 * matches the reader's own label for a client, never an id.
 *
 * The selected client is read from its keyed resource, not lifted out of the
 * row: the row and this pane share one identity in the dashboard store, so the
 * preview shows the same answer the row is showing instead of a second request.
 * The transfer triple is the detail view's own, so the bytes this panel copies
 * or downloads are the ones on screen.
 */
import { useCallback, useMemo, useState } from "react";
import { IconSearch } from "../../icons.tsx";
import { useT } from "../../i18n/shared.ts";
import { copyTextToClipboard } from "../../copy-feedback.ts";
import {
  EXPORT_CLIENT_IDS,
  exportClientLabelKey,
  type ClientConfigTransfer,
  type ExportClientId,
} from "../../api-access/export-clients.ts";
import {
  clientConfigCopyAnnouncement,
  clientConfigDetailView,
  clientConfigDownloadAnnouncement,
  clientConfigRowView,
  clientConfigStatus,
  downloadClientConfig,
  useClientConfigEnvelope,
  type ClientConfigDetailView,
} from "./client-config-resource.ts";
import ClientConfigRow from "./ClientConfigRow.tsx";
import { ClientConfigDetail } from "./ClientConfigDetail.tsx";

export default function ClientConfigPanel({
  apiBase,
  hasKeys,
}: {
  apiBase: string;
  hasKeys: boolean;
}) {
  const t = useT();
  const [query, setQuery] = useState("");
  const [selected, setSelected] = useState<ExportClientId>(EXPORT_CLIENT_IDS[0]);
  const [announcement, setAnnouncement] = useState("");
  const status = clientConfigStatus(useClientConfigEnvelope(apiBase, selected).state);
  const selectedLabel = t(exportClientLabelKey(selected));
  const statusView = clientConfigRowView(status, selectedLabel, t);
  const view = status.kind === "ready" ? clientConfigDetailView(status.envelope, hasKeys, t) : null;

  const visible = useMemo(() => {
    const needle = query.trim().toLowerCase();
    if (needle === "") return EXPORT_CLIENT_IDS;
    return EXPORT_CLIENT_IDS.filter(client => t(exportClientLabelKey(client)).toLowerCase().includes(needle));
  }, [query, t]);

  // The generated text is moved through the shared clipboard owner, so this
  // panel gets the legacy fallback a plain-HTTP LAN dashboard needs, and a
  // refused write is announced as a failure instead of as a completed copy.
  const copy = useCallback(async (transfer: ClientConfigTransfer) => {
    setAnnouncement(clientConfigCopyAnnouncement(await copyTextToClipboard(transfer.text), t));
  }, [t]);

  const download = useCallback((detail: ClientConfigDetailView) => {
    downloadClientConfig(detail.transfer);
    setAnnouncement(clientConfigDownloadAnnouncement(detail.transfer, detail.destination, t));
  }, [t]);

  return (
    <div className="awi-split awi-split--clients">
      <div className="awi-master">
        <label className="awi-search">
          <IconSearch aria-hidden="true" />
          <input
            type="search"
            value={query}
            onChange={event => setQuery(event.target.value)}
            placeholder={t("api.searchClients")}
            aria-label={t("api.searchClients")}
          />
        </label>
        <div className="awi-table-wrap">
          <table className="awi-table awi-clients-table" aria-label={t("api.clientConfig.rowsLabel")}>
            <thead>
              <tr>
                <th>{t("api.colClient")}</th>
                <th>{t("api.clientConfig.modelsCol")}</th>
              </tr>
            </thead>
            <tbody>
              {visible.map(client => (
                <ClientConfigRow
                  key={client}
                  client={client}
                  label={t(exportClientLabelKey(client))}
                  apiBase={apiBase}
                  selected={selected === client}
                  onSelect={() => setSelected(client)}
                />
              ))}
            </tbody>
          </table>
        </div>
      </div>
      <div className="awi-detail-col">
        {view === null ? (
          <p className="muted">
            {statusView.kind === "failed" ? statusView.message : statusView.kind === "loading" ? statusView.label : ""}
          </p>
        ) : (
          <ClientConfigDetail
            view={view}
            onAction={id => {
              if (id === "copy") void copy(view.transfer);
              else download(view);
            }}
          />
        )}
      </div>
      <div className="sr-only" aria-live="polite" aria-atomic="true">{announcement}</div>
    </div>
  );
}
