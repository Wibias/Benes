/**
 * Benes dashboard source. The API workspace: Keys | Client exports | Endpoints |
 * Models | Examples.
 *
 * The workspace is five panels over one tab registry, and it takes its props as
 * surfaces rather than as a flat bag: each panel's inputs travel together as the
 * one subject they describe (the key list, the client export, the endpoint
 * facts, the model catalog), so a panel cannot read a field that belongs to a
 * different panel and the page's own hook stays the only assembler.
 *
 * The endpoint facts are one surface with two renderers — the Endpoints panel
 * and the Examples panel read the same routes, and the Claude-conditional
 * Messages surface is a property of those facts, not of either renderer.
 */
import { useEffect, useState, type ReactNode } from "react";
import { useT } from "../../i18n/shared";
import type { ApiAuthMatrixRow } from "../../api-access/auth-matrix";
import type { ApiEndpointInfo } from "../../api-access/endpoints";
import type { ApiKeyEntry } from "../../api-access/keys";
import type { ModelCatalogState } from "../../api-access/model-catalog-view";
import { apiPanelDomId, apiTabDomId, API_TABS, type ApiTab } from "../../pages/api-tab";
import { ApiKeysEndpointsPanel } from "../../pages/api-keys-endpoints-panel";
import { ModelCatalogPanel, type ModelCatalogActions } from "./ModelCatalogPanel";
import { RequestExamplesPanel } from "./RequestExamplesPanel";
import { ApiKeysDetailPane } from "./ApiKeysDetailPane";
import ApiKeysListPanel from "./ApiKeysListPanel";
import ClientConfigPanel from "./ClientConfigPanel";

/** How long the confirm control stays inert after it appears. */
const CONFIRM_ARM_MS = 300;

/** The Keys panel's inputs: the listener's key board, and how to remove a row. */
export interface ApiKeyListSurface {
  readonly entries: ApiKeyEntry[];
  /** A board that loaded before but whose latest read failed. */
  readonly listFailed: boolean;
  readonly attributionSince?: string;
  readonly historyTruncated: boolean;
  readonly localeTag?: string;
  readonly onDelete: (id: string) => Promise<boolean>;
}

/** The client-config export panel's inputs. */
export interface ApiClientExportSurface {
  readonly apiBase: string;
  /** Whether the listener holds at least one key, so the export can authenticate. */
  readonly hasKeys: boolean;
}

/** The endpoint facts both the Endpoints and the Examples panel render. */
export interface ApiEndpointSurface {
  readonly endpoints: ApiEndpointInfo;
  readonly authMatrix: ApiAuthMatrixRow[];
  /** The Messages surface only exists while the Claude harness is enabled. */
  readonly claudeEnabled: boolean;
}

/** The Models panel's inputs, as the accepted catalogue view model takes them. */
export interface ApiModelSurface {
  readonly state: ModelCatalogState;
  readonly actions: ModelCatalogActions;
}

export interface ApiKeysWorkspaceProps {
  tab: ApiTab;
  keys: ApiKeyListSurface;
  clients: ApiClientExportSurface;
  endpoints: ApiEndpointSurface;
  models: ApiModelSurface;
}

export default function ApiKeysWorkspace(props: ApiKeysWorkspaceProps) {
  const keys = useKeySelection(props.keys.entries, props.keys.onDelete);
  const panels: Record<ApiTab, ReactNode> = {
    keys: (
      <KeyListPanel surface={props.keys} selection={keys} />
    ),
    clients: <ClientConfigPanel apiBase={props.clients.apiBase} hasKeys={props.clients.hasKeys} />,
    endpoints: <ApiKeysEndpointsPanel endpoints={props.endpoints.endpoints} authMatrix={props.endpoints.authMatrix} />,
    models: <ModelCatalogPanel state={props.models.state} actions={props.models.actions} />,
    examples: <RequestExamplesPanel endpoints={props.endpoints.endpoints} claudeCodeEnabled={props.endpoints.claudeEnabled} />,
  };

  return (
    <div className="apikeys-workspace-shell">
      {API_TABS.map(tab => (
        <ApiTabPanel key={tab} tab={tab} active={tab === props.tab}>
          {panels[tab]}
        </ApiTabPanel>
      ))}
    </div>
  );
}
/**
 * One panel's ARIA frame. Every panel is mounted and only the addressed one is
 * shown, so switching tabs never re-runs a panel's own requests.
 */
function ApiTabPanel({ tab, active, children }: { tab: ApiTab; active: boolean; children: ReactNode }) {
  return (
    <div role="tabpanel" id={apiPanelDomId(tab)} aria-labelledby={apiTabDomId(tab)} hidden={!active}>
      {children}
    </div>
  );
}

/** The Keys panel: the list, and the detail pane for whichever key is selected. */
function KeyListPanel({ surface, selection }: { surface: ApiKeyListSurface; selection: KeySelection }) {
  const t = useT();
  const selected = selection.selected;
  return (
    <div className="awi-split" aria-label={t("api.workspace.details")}>
      <ApiKeysListPanel
        keys={surface.entries}
        keysLoadFailed={surface.listFailed}
        attributionSince={surface.attributionSince}
        localeTag={surface.localeTag}
        busy={selection.deleting}
        selectedId={selected?.id ?? null}
        onSelect={selection.select}
      />
      <div className="awi-detail-col">
        {selected ? (
          <ApiKeysDetailPane
            selected={selected}
            localeTag={surface.localeTag}
            attributionSince={surface.attributionSince}
            historyTruncated={surface.historyTruncated}
            confirmDelete={selection.confirming}
            confirmArmed={selection.armed}
            deleting={selection.deleting}
            deletingFailed={selection.failed}
            onConfirmDelete={() => { void selection.confirm(); }}
            onCancelDelete={selection.cancel}
            onRequestDelete={() => selection.request(selected.id)}
          />
        ) : (
          <p className="muted">{t("api.emptyKeyDetail")}</p>
        )}
      </div>
    </div>
  );
}

/** What the Keys panel reads and writes about its selection. */
export interface KeySelection {
  /** The chosen key, the board's first one, or `null` when there are none. */
  readonly selected: ApiKeyEntry | null;
  readonly confirming: boolean;
  readonly armed: boolean;
  readonly deleting: boolean;
  readonly failed: boolean;
  readonly select: (id: string) => void;
  readonly cancel: () => void;
  readonly request: (id: string) => void;
  readonly confirm: () => Promise<void>;
}

/**
 * Selection and the two-step delete confirm.
 *
 * Removal is armed in two steps so a single click on a row's action cannot
 * destroy a key: the request shows the confirm control, the control stays inert
 * until the arming delay has passed, and only the confirm performs the delete.
 * Choosing another row, cancelling, or a successful removal drops the pending
 * intent, so a half-armed confirm never survives a change of selection.
 */
function useKeySelection(entries: ApiKeyEntry[], onDelete: (id: string) => Promise<boolean>): KeySelection {
  const [chosenId, setChosenId] = useState<string | null>(null);
  const [pendingId, setPendingId] = useState<string | null>(null);
  const [armed, setArmed] = useState(false);
  const [deleting, setDeleting] = useState(false);
  const [failed, setFailed] = useState(false);

  const selected = entries.find(entry => entry.id === chosenId) ?? entries[0] ?? null;
  const selectedId = selected?.id ?? null;
  const confirming = pendingId !== null && pendingId === selectedId;

  useEffect(() => {
    if (!confirming) return undefined;
    const timer = window.setTimeout(() => setArmed(true), CONFIRM_ARM_MS);
    return () => window.clearTimeout(timer);
  }, [confirming]);

  const cancel = () => {
    setPendingId(null);
    setArmed(false);
  };

  const confirm = async () => {
    if (selected === null || !confirming || !armed || deleting) return;
    setDeleting(true);
    setFailed(false);
    try {
      if (await onDelete(selected.id)) {
        cancel();
        setChosenId(null);
        return;
      }
      setFailed(true);
    } finally {
      setDeleting(false);
    }
  };

  return {
    selected,
    confirming,
    armed,
    deleting,
    failed,
    select: id => {
      setChosenId(id);
      setPendingId(null);
      setArmed(false);
      setFailed(false);
    },
    cancel,
    request: id => {
      setArmed(false);
      setPendingId(id);
    },
    confirm,
  };
}
