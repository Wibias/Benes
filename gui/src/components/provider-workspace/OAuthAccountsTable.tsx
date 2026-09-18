/**
 * Divider-table Access view for OAuth/Codex accounts. Uses the shared pool
 * controller; does not mount a second account fetcher and owns no credential policy.
 */
import { useT, type TFn } from "../../i18n/shared";
import { IconInfo, IconMore } from "../../icons";
import { normalizeAccountPriority } from "../../account-priority";
import { accessWeeklyQuotaCell } from "../../codex-quota-utils";
import { formatAccessQuotaReset } from "../../provider-workspace/quota-presentation";
import { formatElapsedSince } from "../../relative-clock";
import { oauthAccessAccountLabel } from "../../provider-workspace/auth";
import { accessShowsSelectionOrder, accessTableScrolls } from "../../provider-workspace/access-presentation";
import {
  codexAccountHealthFlags,
  codexAccountShowsSwitch,
} from "../../lib/codex-account-card-policy";
import {
  doctorCopyButtonLabel,
  formatOAuthHealthLabel,
  oauthHealthShowsDoctor,
} from "../../oauth-health-display";
import { CodexTicketBadge } from "../codex-account-pool-helpers";
import type { CodexAccountEntry, CodexAccountPoolController } from "../../hooks/useCodexAccountPool";
import type { NoticeTone } from "../../ui";
import { Tooltip } from "../../ui";
import type { ReactNode } from "react";

/** The status dot: paused reads off, a health or reauth state reads warn, otherwise ok. */
function statusDotClass(paused: boolean, warn: boolean): string {
  if (paused) return "providers-pill-dot is-off";
  return warn ? "providers-pill-dot is-warn" : "providers-pill-dot is-ok";
}

/** Everything one row paints, derived once so the markup below only reads values. */
function accessRowView(account: CodexAccountEntry, activeId: string | null, t: TFn) {
  const flags = codexAccountHealthFlags(account);
  const healthLabel = formatOAuthHealthLabel(t, account.health);
  const isActive = account.isMain ? !activeId || activeId === "__main__" : activeId === account.id;
  return {
    flags,
    weekly: accessWeeklyQuotaCell(account.quota),
    resetText: resetTextFor(account.quota, t),
    isActive,
    status: account.paused
      ? t("prov.access.paused")
      : flags.showReauth
        ? t("codexAuth.needsReauth")
        : healthLabel ?? t("prov.access.ready"),
    dotClass: statusDotClass(account.paused, flags.showReauth || flags.inCooldown),
    showsSwitch: codexAccountShowsSwitch({
      paused: account.paused,
      isActive,
      showReauth: flags.showReauth,
      inCooldown: flags.inCooldown,
    }),
    showsDoctor: oauthHealthShowsDoctor(flags.healthStatus),
  };
}

function resetTextFor(quota: CodexAccountEntry["quota"], t: TFn): string {
  const weekly = accessWeeklyQuotaCell(quota);
  return weekly && typeof weekly.resetAt === "number" ? formatAccessQuotaReset(weekly.resetAt, t) : "";
}

export default function OAuthAccountsTable({
  controller,
  refreshingQuota,
  pausingExhausted,
  actionFeedback,
  actionFeedbackTone,
  heading,
  switchActionLabel,
  doctorCopyOutcomeFor,
  onRefresh,
  onPauseExhausted,
  onAddAccount,
  onTogglePause,
  onPriorityChange,
  onReauth,
  onEditAlias,
  onRemove,
  onSwitch,
  onOpenReset,
  onCopyDoctor,
}: {
  controller: CodexAccountPoolController;
  refreshingQuota: boolean;
  pausingExhausted: boolean;
  actionFeedback?: string | null;
  actionFeedbackTone?: NoticeTone | null;
  heading?: ReactNode;
  switchActionLabel: string;
  doctorCopyOutcomeFor?: (accountId: string) => "copied" | "unavailable" | null;
  onRefresh: () => void;
  onPauseExhausted: () => void;
  onAddAccount: () => void;
  onTogglePause: (account: CodexAccountEntry) => void;
  onPriorityChange: (account: CodexAccountEntry, priority: number) => void;
  onReauth: (id: string) => void;
  onEditAlias: (account: CodexAccountEntry) => void;
  onRemove: (id: string) => void;
  onSwitch: (account: CodexAccountEntry) => void;
  onOpenReset: (account: CodexAccountEntry) => void;
  onCopyDoctor: (accountId: string) => void;
}) {
  const t = useT();
  const { accounts, loadState, pauseUpdatingId, priorityUpdatingId, switchingId } = controller;
  const busy = refreshingQuota || pausingExhausted;
  const showsSelectionOrder = accessShowsSelectionOrder(accounts.length);

  return (
    <div className="providers-access-oauth">
      <div className="providers-block-head">
        {heading ?? <h4>{t("prov.access.oauthAccounts")}</h4>}
        <div className="providers-access-head-actions">
          <button type="button" className="providers-link providers-link--plain" disabled={busy} onClick={onRefresh}>
            {refreshingQuota ? t("codexAuth.refreshingQuota") : t("prov.access.refreshQuotas")}
          </button>
          <button type="button" className="providers-link providers-link--plain" disabled={busy} onClick={onAddAccount}>
            {t("prov.access.addAccount")}
          </button>
        </div>
      </div>
      {actionFeedback && (
        <p className={`providers-access-feedback${actionFeedbackTone ? ` is-${actionFeedbackTone}` : ""}`} role="status">
          {actionFeedback}
        </p>
      )}
      {loadState === "error" && (
        <p className="muted">{t("pws.accountsLoadFailed")}</p>
      )}
      {loadState === "loading" && accounts.length === 0 && (
        <p className="muted">{t("pws.accountsLoading")}</p>
      )}
      <div className="providers-access-table" role="table" aria-label={t("prov.access.oauthAccounts")}>
        <div className="providers-access-table-head" role="row">
          <span role="columnheader">{t("prov.access.colAccount")}</span>
          <span role="columnheader" className="providers-access-status">{t("prov.access.colStatus")}</span>
          <span role="columnheader" className="providers-access-quota">{t("prov.access.colWeeklyQuota")}</span>
          <span role="columnheader" className="providers-access-quota-reset" />
          <span role="columnheader" className="providers-access-order-cell">
            <span className="providers-access-order-head">
              {t("prov.access.colSelectionOrder")}
              <Tooltip content={t("accountPool.priorityHint")} side="bottom">
                <span className="providers-access-order-info">
                  <IconInfo />
                  <span className="sr-only">{t("accountPool.priorityHint")}</span>
                </span>
              </Tooltip>
            </span>
          </span>
          <span role="columnheader" className="providers-access-validated">{t("prov.access.colLastValidated")}</span>
          <span role="columnheader" className="providers-access-row-menu" aria-label={t("prov.menu.more")} />
        </div>
        <ul
          className={accessTableScrolls(accounts.length)
            ? "providers-access-table-body providers-access-table-body--scroll"
            : "providers-access-table-body"}
        >
          {accounts.length === 0 && (
            <li className="providers-access-table-row providers-access-table-row--empty" role="row">
              <span role="cell">—</span>
              <span role="cell">—</span>
              <span role="cell" />
              <span role="cell">—</span>
              <span role="cell" className="providers-access-order-cell">—</span>
              <span role="cell">—</span>
              <span role="cell" className="providers-access-row-menu">
                <span className="providers-menu-icon" aria-hidden="true"><IconMore /></span>
              </span>
            </li>
          )}
          {accounts.map(account => (
            <AccessAccountRow
              key={account.id}
              account={account}
              accounts={accounts}
              activeId={controller.activeId}
              t={t}
              switchActionLabel={switchActionLabel}
              doctorCopyOutcomeFor={doctorCopyOutcomeFor}
              showsSelectionOrder={showsSelectionOrder}
              pauseUpdatingId={pauseUpdatingId}
              priorityBusy={priorityUpdatingId === account.id || switchingId !== null}
              actions={{
                onTogglePause,
                onPriorityChange,
                onReauth,
                onEditAlias,
                onRemove,
                onSwitch,
                onOpenReset,
                onCopyDoctor,
              }}
            />
          ))}
          </ul>
        </div>
      <div className="providers-overview-footer">
        <button type="button" className="providers-link providers-link--plain" disabled={busy} onClick={onPauseExhausted}>
          {pausingExhausted ? t("codexAuth.pausingExhausted") : t("prov.access.pauseExhausted")}
        </button>
      </div>
    </div>
  );
}

type RowMenuActions = {
  onTogglePause: (account: CodexAccountEntry) => void;
  onPriorityChange: (account: CodexAccountEntry, priority: number) => void;
  onReauth: (id: string) => void;
  onEditAlias: (account: CodexAccountEntry) => void;
  onRemove: (id: string) => void;
  onSwitch: (account: CodexAccountEntry) => void;
  onOpenReset: (account: CodexAccountEntry) => void;
  onCopyDoctor: (accountId: string) => void;
};

/** One account row. Runs an action from the overflow menu and closes that menu first. */
function AccessAccountRow({
  account,
  accounts,
  activeId,
  t,
  switchActionLabel,
  doctorCopyOutcomeFor,
  showsSelectionOrder,
  pauseUpdatingId,
  priorityBusy,
  actions,
}: {
  account: CodexAccountEntry;
  accounts: readonly CodexAccountEntry[];
  activeId: string | null;
  t: TFn;
  switchActionLabel: string;
  doctorCopyOutcomeFor?: (accountId: string) => "copied" | "unavailable" | null;
  showsSelectionOrder: boolean;
  pauseUpdatingId: string | null;
  priorityBusy: boolean;
  actions: RowMenuActions;
}) {
  const view = accessRowView(account, activeId, t);

  const runFromMenu = (event: { currentTarget: EventTarget & HTMLElement }, run: () => void) => {
    const root = event.currentTarget.closest("details");
    if (root) root.open = false;
    run();
  };

  return (
    <li className="providers-access-table-row" role="row">
      <span role="cell" className="providers-access-account">{oauthAccessAccountLabel(accounts, account, t)}</span>
      <span role="cell" className="providers-access-status">
        <span className={view.dotClass} aria-hidden="true" />
        {view.status}
      </span>
      <span role="cell" className="providers-access-quota">
        {view.weekly ? <span className="providers-access-quota-used">{`${view.weekly.remainingPercent}%`}</span> : "—"}
        <CodexTicketBadge t={t} account={account} onClick={() => actions.onOpenReset(account)} />
      </span>
      <span role="cell" className="providers-access-quota-reset">{view.resetText}</span>
      <span role="cell" className="providers-access-order-cell">
        {showsSelectionOrder ? (
          <select
            className="providers-access-order"
            value={String(normalizeAccountPriority(account.priority))}
            disabled={priorityBusy}
            aria-label={t("prov.access.colSelectionOrder")}
            title={t("accountPool.priorityHint")}
            onChange={event => actions.onPriorityChange(account, Number.parseInt(event.target.value, 10))}
          >
            <option value="1">{t("accountPool.priorityEarlier")}</option>
            <option value="0">{t("accountPool.priorityNormal")}</option>
            <option value="-1">{t("accountPool.priorityLater")}</option>
          </select>
        ) : "—"}
      </span>
      <span role="cell" className="providers-access-validated">
        {account.quota?.updatedAt ? formatElapsedSince(account.quota.updatedAt, t) : "—"}
      </span>
      <span role="cell" className="providers-access-row-menu">
        <details className="providers-menu">
          <summary aria-label={t("prov.menu.more")}><IconMore /></summary>
          <div className="providers-menu-list">
            {view.showsSwitch && (
              <button type="button" onClick={event => runFromMenu(event, () => actions.onSwitch(account))}>
                {switchActionLabel}
              </button>
            )}
            <button
              type="button"
              disabled={pauseUpdatingId === account.id}
              onClick={event => runFromMenu(event, () => actions.onTogglePause(account))}
            >
              {account.paused ? t("codexAuth.resume") : t("codexAuth.pause")}
            </button>
            <button type="button" onClick={event => runFromMenu(event, () => actions.onReauth(account.id))}>
              {t("pws.reauthenticate")}
            </button>
            {view.showsDoctor && (
              <button type="button" onClick={event => runFromMenu(event, () => actions.onCopyDoctor(account.id))}>
                {doctorCopyButtonLabel(t, doctorCopyOutcomeFor?.(account.id))}
              </button>
            )}
            <button type="button" onClick={event => runFromMenu(event, () => actions.onEditAlias(account))}>
              {t("prov.editAlias")}
            </button>
            {!account.isMain && (
              <button type="button" onClick={event => runFromMenu(event, () => actions.onRemove(account.id))}>
                {t("common.remove")}
              </button>
            )}
          </div>
        </details>
      </span>
    </li>
  );
}
