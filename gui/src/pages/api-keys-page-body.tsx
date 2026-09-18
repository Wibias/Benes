/**
 * Benes dashboard source. The API page's frame, its load gate, and the adapter
 * that hands the workspace its surfaces.
 *
 * The page has exactly three states and they are mutually exclusive: the cold
 * gate (nothing has arrived), the ready workspace, and the frame that stays
 * mounted through both so the tab strip and the page head never move. The gate
 * is a variant rather than a pair of booleans, so a cold failure can never be
 * rendered beside a skeleton.
 *
 * The ready adapter is deliberately thin. It exists so the page component does
 * not have to know the workspace's prop shape; it forwards the assembled
 * surfaces and nothing else.
 */
import type { ReactNode } from "react";
import { Notice, ToastNotice } from "../ui";
import { useT } from "../i18n/shared";
import { DataSurfaceSkeleton } from "../components/data-surface";
import ApiKeysWorkspace, {
  type ApiClientExportSurface,
  type ApiEndpointSurface,
  type ApiKeyListSurface,
  type ApiModelSurface,
} from "../components/apikeys-workspace/ApiKeysWorkspace";
import type { ApiTab } from "./api-tab";
import { ApiTabStrip } from "./api-tab-strip";
import { selectApiTab } from "./api-tab";
import { ApiKeyCreateDialog } from "./api-key-create-dialog";

/** Everything the ready workspace needs, grouped by the surface it belongs to. */
export interface ApiKeysReadyModel {
  keys: ApiKeyListSurface;
  clients: ApiClientExportSurface;
  endpoints: ApiEndpointSurface;
  models: ApiModelSurface;
}

/** The create-key dialog, as the page owns it. */
interface ApiKeyDialogModel {
  open: boolean;
  name: string;
  creating: boolean;
  newKey: string | null;
  copied: boolean;
  onNameChange: (value: string) => void;
  onCreate: () => void;
  onCopy: () => void;
  onClose: () => void;
}

export function ApiKeysPageFrame({
  tab,
  busy,
  actionError,
  keysError,
  createOpen,
  newName,
  creating,
  newKey,
  copied,
  onOpenCreate,
  onCloseCreate,
  onNewNameChange,
  onCreate,
  onCopyKey,
  onClearActionError,
  children,
}: {
  tab: ApiTab;
  busy: boolean;
  actionError: string | null;
  keysError: boolean;
  createOpen: boolean;
  newName: string;
  creating: boolean;
  newKey: string | null;
  copied: boolean;
  onOpenCreate: () => void;
  onCloseCreate: () => void;
  onNewNameChange: (value: string) => void;
  onCreate: () => void;
  onCopyKey: () => void;
  onClearActionError: () => void;
  children: ReactNode;
}) {
  const dialog: ApiKeyDialogModel = {
    open: createOpen,
    name: newName,
    creating,
    newKey,
    copied,
    onNameChange: onNewNameChange,
    onCreate,
    onCopy: onCopyKey,
    onClose: onCloseCreate,
  };
  return (
    <section className="api-page" aria-busy={busy || undefined}>
      <ApiPageHead onCreate={onOpenCreate} />
      <PageNotices actionError={actionError} keysError={keysError} onClearActionError={onClearActionError} />
      <ApiTabStrip tab={tab} onSelect={selectApiTab} />
      {children}
      <ApiKeyCreateDialog {...dialog} />
    </section>
  );
}

/** The page head: its title, and the one action the page owns. */
function ApiPageHead({ onCreate }: { onCreate: () => void }) {
  const t = useT();
  return (
    <div className="page-head">
      <h2>{t("api.title")}</h2>
      <div className="page-head-actions">
        <button type="button" className="btn api-create-key" onClick={onCreate}>
          {t("api.createKey")}
        </button>
      </div>
    </div>
  );
}

/**
 * The two page-level messages, each with the dismissal its shape needs.
 *
 * A failed action is transient and dismissible; a failed key-board read is a
 * fact about the page and stays until the next read succeeds.
 */
function PageNotices({
  actionError,
  keysError,
  onClearActionError,
}: {
  actionError: string | null;
  keysError: boolean;
  onClearActionError: () => void;
}) {
  const t = useT();
  return (
    <>
      {actionError && (
        <ToastNotice tone="err" dismissLabel={t("common.close")} onDismiss={onClearActionError}>
          {actionError}
        </ToastNotice>
      )}
      {keysError && <Notice tone="err">{t("api.keysLoadFailed")}</Notice>}
    </>
  );
}

/** Which of the gate's three answers applies, and nothing in between. */
type LoadGateVariant = "skeleton" | "failed" | "ready";

/** The skeleton only precedes an attempt; a cold failure supersedes it. */
function loadGateVariant(showSkeleton: boolean, failedCold: boolean): LoadGateVariant {
  if (showSkeleton) return "skeleton";
  return failedCold ? "failed" : "ready";
}

export function ApiKeysLoadGate({
  showSkeleton,
  failedCold,
  errorMessage,
  onRetry,
  loadingLabel,
  retryLabel,
}: {
  showSkeleton: boolean;
  failedCold: boolean;
  errorMessage: string;
  onRetry: () => void;
  loadingLabel: string;
  retryLabel: string;
}) {
  switch (loadGateVariant(showSkeleton, failedCold)) {
    case "skeleton":
      return <DataSurfaceSkeleton label={loadingLabel} rows={4} />;
    case "failed":
      return (
        <>
          <Notice tone="err">{errorMessage}</Notice>
          <button type="button" className="btn btn-ghost btn-sm" onClick={onRetry}>{retryLabel}</button>
        </>
      );
    case "ready":
      return null;
  }
}

/** The ready workspace: the assembled surfaces, forwarded as they arrived. */
export function ApiKeysReady({ tab, keys, clients, endpoints, models }: ApiKeysReadyModel & { tab: ApiTab }) {
  return (
    <ApiKeysWorkspace
      tab={tab}
      keys={keys}
      clients={clients}
      endpoints={endpoints}
      models={models}
    />
  );
}