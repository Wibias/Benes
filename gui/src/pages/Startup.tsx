/**
 * The Control page: the shell, the board's health panel, and the page toast.
 *
 * Every read and every action lives in `startup-page-controller`; this module composes the
 * model it returns, so what is left here is layout order and nothing else.
 */
import { ToastNotice } from "../ui";
import { ControlHealthPanel } from "./control-board";
import { ControlPageShell } from "./control-page-shell";
import { useControlPage } from "./startup-page-controller";

export default function Startup({ apiBase, restartEpoch = 0 }: { apiBase: string; restartEpoch?: number }) {
  const page = useControlPage(apiBase, restartEpoch);
  const { t, slots } = page;

  return (
    <>
      <ControlPageShell
        t={t}
        appServerState={page.appServerState}
        codexController={page.codex}
        healthRefreshing={page.refreshing}
        onRefresh={page.refreshBoard}
        healthPanel={(
          <ControlHealthPanel
            t={t}
            loadState={page.loadState}
            data={page.data}
            failed={page.failed}
            loading={page.refreshing}
            installBusy={slots.installBusy}
            tray={slots.tray}
            trayBusy={slots.trayBusy}
            trayError={slots.trayError}
            restoreBusy={slots.restoreBusy}
            apiBase={apiBase}
            refreshNonce={page.nonce}
            runtimeNotice={slots.notice}
            copied={slots.copied}
            onCopy={command => { void page.copyCommand(command); }}
            onInstall={page.runInstallAction}
            onTrayAction={page.runTrayAction}
            onRestoreNative={page.restoreNative}
            onRefresh={page.refresh}
          />
        )}
      />
      {page.toast ? (
        <ToastNotice tone={page.toast.tone} dismissLabel={t("common.close")} onDismiss={page.dismissToast}>
          {page.toast.text}
        </ToastNotice>
      ) : null}
    </>
  );
}

