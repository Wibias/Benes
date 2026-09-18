import { useState } from "react";
import { useT } from "../i18n/shared";
import { ToastNotice } from "../ui";
import { useApiKeysPage } from "./use-api-keys-page";
import { ApiKeysLoadGate, ApiKeysPageFrame, ApiKeysReady } from "./api-keys-page-body";
import {
  createKeyDialogState,
  nextConsumedSecret,
  readyDialogFields,
  shouldRenderApiReady,
  useApiTab,
} from "./use-api-workspace-chrome";

export default function ApiKeys({ apiBase, active = true }: { apiBase: string; active?: boolean }) {
  const t = useT();
  const page = useApiKeysPage(apiBase, active);
  const tab = useApiTab();
  const [createOpen, setCreateOpen] = useState(false);
  const [consumedSecret, setConsumedSecret] = useState<string | null>(null);
  const ready = page.ready;
  const fields = readyDialogFields(ready);
  const dialogSecret = createKeyDialogState(ready ? ready.newKey : null, createOpen, consumedSecret);

  return (
    <>
      {page.testNotice && (
        <ToastNotice
          tone="err"
          onDismiss={page.onDismissTestNotice}
          dismissLabel={t("common.close")}
        >
          {page.testNotice}
        </ToastNotice>
      )}
      <ApiKeysPageFrame
        tab={tab}
        busy={page.busy}
        actionError={page.actionError}
        keysError={page.keysError}
        createOpen={createOpen}
        newName={fields.newName}
        creating={fields.creating}
        newKey={dialogSecret}
        copied={fields.copied}
        onOpenCreate={() => setCreateOpen(true)}
        onCloseCreate={() => {
          setConsumedSecret(nextConsumedSecret(dialogSecret, consumedSecret));
          setCreateOpen(false);
        }}
        onNewNameChange={fields.onNewNameChange}
        onCreate={fields.onCreate}
        onCopyKey={fields.onCopyKey}
        onClearActionError={page.onClearActionError}
      >
        <ApiKeysLoadGate
          showSkeleton={page.showSkeleton}
          failedCold={page.failedCold}
          errorMessage={page.errorMessage}
          onRetry={() => page.onRetryKeys()}
          loadingLabel={t("api.activeKeysLoading")}
          retryLabel={t("common.retry")}
        />
        {shouldRenderApiReady(page.showSkeleton, page.failedCold, Boolean(ready)) && ready
          ? <ApiKeysReady tab={tab} {...ready} />
          : null}
      </ApiKeysPageFrame>
    </>
  );
}
