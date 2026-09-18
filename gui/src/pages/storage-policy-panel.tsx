/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { Locale, TFn } from "../i18n/shared";
import { PolicyForm } from "./storage-policy-fields";
import { useCleanupPolicy } from "./use-cleanup-policy";

type AutoCleanupPolicyPanelProps = {
  readonly apiBase: string;
  readonly locale: Locale;
  readonly t: TFn;
  readonly onDone: () => void;
};

/** The pane every state of this panel renders into. */
function PanelShell({ children }: { children: React.ReactNode }) {
  return <section className="storage-cleanup-pane">{children}</section>;
}

/** The retry a pane offers when its first read failed. */
function PanelRetry({ label, onRetry }: { label: string; onRetry: () => void }) {
  return (
    <>
      {" "}
      <button type="button" className="btn btn-ghost btn-sm" onClick={onRetry}>
        {label}
      </button>
    </>
  );
}

/**
 * The automatic-cleanup policy the Cleanup tab owns.
 *
 * This file is the pane only: which policy is current, what the reader is drafting, and how a
 * commit reaches the listener all live in `useCleanupPolicy`, so the panel cannot drift out of
 * step with the loader that feeds it.
 */
export function AutoCleanupPolicyPanel({ apiBase, locale, t, onDone }: AutoCleanupPolicyPanelProps) {
  const policy = useCleanupPolicy({ apiBase, locale, t, onDone });
  const { state } = policy;

  if (state.loading && state.policy === null) {
    return (
      <PanelShell>
        <p className="muted storage-policy-help">{t("storage.policy.loading")}</p>
      </PanelShell>
    );
  }

  if (state.policy === null) {
    return (
      <PanelShell>
        {state.error && (
          <p className="err" role="alert">
            {t("storage.policy.loadFailed")}
            <PanelRetry label={t("common.retry")} onRetry={() => void policy.loadPolicy()} />
          </p>
        )}
      </PanelShell>
    );
  }

  return (
    <PolicyForm
      policy={state.policy}
      t={t}
      locale={locale}
      saving={state.saving}
      running={state.running}
      drafts={state.drafts}
      error={state.error}
      status={state.status}
      markDirty={policy.markDirty}
      setEditing={policy.setEditing}
      onDrafts={policy.editDrafts}
      savePolicy={patch => { void policy.savePolicy(patch); }}
      runNow={() => { void policy.runNow(); }}
      formatWhen={policy.formatWhen}
      clearFeedback={policy.clearFeedback}
    />
  );
}