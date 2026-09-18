/**
 * Benes dashboard source. The Clients panel's data, views, and transport.
 *
 * The listener serves every export client from one route, and each client gets
 * its own keyed resource from the shared dashboard store. Two things follow from
 * that: a client whose request fails cannot disable its neighbours, because they
 * never shared an identity or an attempt; and the list row and the detail pane
 * are two readers of the same identity, so the preview never asks for bytes the
 * row is already holding.
 *
 * The envelope is the listener's, decoded by the accepted `api-access` owner. A
 * rejected envelope resolves to `null` and is reported as a failure rather than
 * as an empty panel, because a client the listener answered about but the
 * dashboard could not read is not the same as one that answered nothing.
 *
 * Everything the two client surfaces print is resolved here, so the components
 * render rather than decide. The transfer triple is built once and carried by the
 * detail view, which is what keeps the bytes on screen and the bytes a reader
 * copies from ever disagreeing.
 */
import { useCallback } from "react";
import { useDataSurface, type DataSurfaceResource, type DataSurfaceState } from "../../data-surface.ts";
import {
  clientConfigTransfer,
  exportClientLabelKey,
  parseClientConfigEnvelope,
  type ClientConfigEnvelope,
  type ClientConfigTransfer,
  type ExportClientId,
} from "../../api-access/export-clients.ts";
import type { TFn } from "../../i18n/shared.ts";

const CLIENT_CONFIG_ROUTE = "/api/client-config";

/** The shared identity for one export client's envelope. */
export function clientConfigResourceKey(apiBase: string, client: ExportClientId): string {
  return `api-client-config:${apiBase}:${client}`;
}

/** The route the listener serves that identity from. */
function clientConfigUrl(apiBase: string, client: ExportClientId): string {
  return `${apiBase}${CLIENT_CONFIG_ROUTE}?client=${encodeURIComponent(client)}`;
}

/** One client's envelope, and the revalidation the panel can ask for. */
export function useClientConfigEnvelope(
  apiBase: string,
  client: ExportClientId,
): DataSurfaceResource<ClientConfigEnvelope | null> {
  const load = useCallback(async (signal: AbortSignal) => {
    const response = await fetch(clientConfigUrl(apiBase, client), { signal });
    if (!response.ok) throw new Error(String(response.status));
    return parseClientConfigEnvelope(await response.json() as unknown, client);
  }, [apiBase, client]);
  return useDataSurface(clientConfigResourceKey(apiBase, client), [apiBase, client], load, {
    isEmpty: envelope => envelope === null,
  });
}

/** What a reader can say about one client's request. */
export type ClientConfigStatus =
  | { readonly kind: "loading" }
  | { readonly kind: "failed" }
  | { readonly kind: "ready"; readonly envelope: ClientConfigEnvelope };

/**
 * One client's request, as both surfaces read it.
 *
 * The failure tests come first on purpose: a client that answered successfully
 * before and refused on a retry still reads as failed, so the retry control
 * stays available instead of the row showing the last good count over a request
 * that no longer works.
 */
export function clientConfigStatus(state: DataSurfaceState<ClientConfigEnvelope | null>): ClientConfigStatus {
  if (state.kind === "failed-cold" || state.kind === "failed-with-stale") return { kind: "failed" };
  if (state.data === undefined) return { kind: "loading" };
  if (state.data === null) return { kind: "failed" };
  return { kind: "ready", envelope: state.data };
}

/** What one client's row prints, already resolved. */
export type ClientConfigRowView =
  | { readonly kind: "loading"; readonly label: string }
  | { readonly kind: "failed"; readonly message: string; readonly retryLabel: string }
  | { readonly kind: "ready"; readonly models: string };

/**
 * The row's second cell for one client.
 *
 * The models cell states the listener's own count, and a client that failed
 * states the failure with its own retry, so one broken client is actionable
 * without reloading the list around it.
 */
export function clientConfigRowView(status: ClientConfigStatus, label: string, t: TFn): ClientConfigRowView {
  if (status.kind === "loading") return { kind: "loading", label: t("api.clientConfig.loading") };
  if (status.kind === "failed") {
    return {
      kind: "failed",
      message: t("api.clientConfig.rowError", { client: label }),
      retryLabel: t("common.retry"),
    };
  }
  return { kind: "ready", models: String(status.envelope.modelCount) };
}

/** One overview line in the detail pane: a label, and the listener's value. */
export interface ClientConfigOverviewRow {
  readonly label: string;
  readonly value: string;
  readonly code?: boolean;
}

/** The two ways the listener's generated file leaves the dashboard. */
export type ClientConfigActionId = "copy" | "download";

/** One transport control in the detail pane's head. */
export interface ClientConfigAction {
  readonly id: ClientConfigActionId;
  readonly label: string;
}

/** One export client's detail pane, as it is rendered. */
export interface ClientConfigDetailView {
  readonly title: string;
  /** Where the client reads the file the listener generated. */
  readonly destination: string;
  readonly actions: readonly ClientConfigAction[];
  readonly overviewTitle: string;
  readonly overview: readonly ClientConfigOverviewRow[];
  readonly notes: readonly string[];
  readonly preview: {
    readonly title: string;
    readonly note: string;
    readonly label: string;
    readonly text: string;
  };
  /** What the two actions move: the listener's own bytes, name, and media type. */
  readonly transfer: ClientConfigTransfer;
}

/**
 * The generated-config pane for one client.
 *
 * The preview and the two actions read the same transfer triple, so what a
 * reader sees cannot differ from what they copy. `text` is previewed byte for
 * byte because four of these clients do not read JSON, and a preview rebuilt
 * from `config` would show a file those clients cannot load.
 */
export function clientConfigDetailView(
  envelope: ClientConfigEnvelope,
  hasKeys: boolean,
  t: TFn,
): ClientConfigDetailView {
  const label = t(exportClientLabelKey(envelope.client));
  const notes = [
    t("api.clientConfig.mergeWarning"),
    t("api.clientConfig.whereBody"),
  ];
  if (!hasKeys) notes.push(t("api.clientConfig.noKeyYet", { env: envelope.apiKeyEnv }));
  return {
    title: label,
    destination: envelope.destination,
    actions: [
      { id: "copy", label: t("api.copy") },
      { id: "download", label: t("api.clientConfig.download") },
    ],
    overviewTitle: t("api.clientConfig.overview"),
    overview: [
      { label: t("api.clientConfig.destinationShort"), value: envelope.destination },
      { label: t("api.clientConfig.format"), value: envelope.format },
      { label: t("api.clientConfig.modelCountLabel"), value: String(envelope.modelCount) },
      { label: t("api.clientConfig.modelsWithoutLimitsLabel"), value: String(envelope.modelsWithoutLimits) },
      { label: t("api.clientConfig.apiKeyEnv"), value: envelope.apiKeyEnv, code: true },
      { label: t("api.clientConfig.exportHintLabel"), value: envelope.exportHint },
    ],
    notes,
    preview: {
      title: t("api.clientConfig.generated"),
      note: t("api.clientConfig.exactBytes"),
      label: t("api.clientConfig.jsonLabel", { client: label }),
      text: envelope.text,
    },
    transfer: clientConfigTransfer(envelope),
  };
}

/**
 * Hand the listener's generated file to the browser's own downloader.
 *
 * The anchor is an implementation detail of a real control: the filename and
 * media type travel with the bytes, so a downloaded file is the one the listener
 * named rather than one the dashboard invented.
 */
export function downloadClientConfig(transfer: ClientConfigTransfer): void {
  const url = URL.createObjectURL(new Blob([transfer.text], { type: transfer.mediaType }));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = transfer.filename;
  anchor.click();
  URL.revokeObjectURL(url);
}

/**
 * What the panel's live region says after a transport action.
 *
 * Copy reports the clipboard's own outcome, so a denied or absent clipboard can
 * never announce success; download names the file and where the client reads it,
 * because a file in the downloads folder has applied nothing yet.
 */
export function clientConfigCopyAnnouncement(copied: boolean, t: TFn): string {
  return t(copied ? "api.clientConfig.copiedAnnounce" : "api.clientConfig.copyFailed");
}

export function clientConfigDownloadAnnouncement(
  transfer: ClientConfigTransfer,
  destination: string,
  t: TFn,
): string {
  return t("api.clientConfig.downloadedAnnounce", { filename: transfer.filename, destination });
}
