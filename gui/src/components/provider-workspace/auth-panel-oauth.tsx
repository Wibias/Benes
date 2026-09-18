import { useEffect, useRef, useState } from "react";
import { useI18n, useT } from "../../i18n/shared";
import { projectQuotaSurface } from "../../provider-workspace/quota-presentation";
import { IconTrash } from "../../icons";
import type { WorkspaceItem } from "../../provider-workspace/catalog";
import { oauthAccountDisplayLabel } from "../../provider-workspace/auth";
import { displayAccountId } from "../../lib/privacy";
import {
  formatOAuthHealthLabel,
  formatOAuthHealthSummary,
  oauthHealthBadgeClass,
} from "../../oauth-health-display";
import AnthropicAccountPoolSettings from "./AnthropicAccountPoolSettings";
import { LoginUrlBlock } from "../login-url-block";
import ProviderQuotaMeters from "../ProviderQuotaMeters";
import { useCopyFeedback } from "../use-copy-feedback";
import {
  oauthAccountInteraction,
  oauthSessionLoggedIn,
} from "../../provider-workspace/access-presentation";
import {
  runCockpitImport,
  type CockpitImportResult,
} from "../../provider-workspace/cockpit-import";
import type { AccountLoadState } from "../../hooks/useProviderCredentials";
import type { LoginHint } from "../../pages/providers-chrome";
import type { OAuthAccountRow } from "../../provider-workspace/provider-credential-api";
import type { ProviderAuthHandlers } from "./ProviderAccess";

const QUOTA_ENRICH_RESERVE_MS = 4_000;
const QUOTA_SURFACE_THRESHOLD = 80;

type CockpitImportPhase = "idle" | "invalid" | "failed" | "complete";

/**
 * Reserves stacked bar height while the soft `&quota=1` enrichment is still in
 * flight, so rows do not shove downward when WHAM answers.
 *
 * This is deliberately a timed state machine rather than a derived boolean: the
 * reservation has to EXPIRE after QUOTA_ENRICH_RESERVE_MS so a stalled enrichment
 * cannot leave skeleton rows up forever.
 */
function useQuotaSlotReservation(accounts: OAuthAccountRow[]): boolean {
  const [reserved, setReserved] = useState(false);
  useEffect(() => {
    const awaitingQuota = accounts.some(account => account.quota == null && !account.quotaUnavailable);
    if (accounts.length === 0 || !awaitingQuota) {
      // eslint-disable-next-line react-hooks/set-state-in-effect, react/react-compiler
      setReserved(false);
      return;
    }
    setReserved(true);
    const timer = window.setTimeout(() => setReserved(false), QUOTA_ENRICH_RESERVE_MS);
    return () => window.clearTimeout(timer);
  }, [accounts]);
  return reserved;
}

export function OAuthAccountsPanel({
  item, apiBase, oauth, accounts, accountLoadState, switchingAccountId, busy, loginHint, authHandlers, omitChrome,
}: {
  item: WorkspaceItem;
  apiBase: string;
  oauth?: { loggedIn: boolean; email?: string; error?: string };
  accounts: OAuthAccountRow[];
  accountLoadState: AccountLoadState;
  switchingAccountId: string | null;
  busy: boolean;
  loginHint?: LoginHint | null;
  authHandlers: ProviderAuthHandlers;
  omitChrome: boolean;
}) {
  const t = useT();
  const reserveQuotaSlots = useQuotaSlotReservation(accounts);
  const hintForThisProvider = loginHint?.provider === item.name ? loginHint : null;
  const loggedIn = oauthSessionLoggedIn(accounts.length, oauth);
  const flaggedAccount = accounts.find(account => account.active && account.needsReauth);

  return (
    <section className={omitChrome ? undefined : "providers-block pwi-auth-section"} aria-label={t("pws.availableAccounts")}>
      {!omitChrome && (
        <OAuthPanelHead
          itemName={item.name}
          loggedIn={loggedIn}
          busy={busy}
          switchingAccountId={switchingAccountId}
          authHandlers={authHandlers}
        />
      )}
      <div className="pwi-auth-body">
        {item.name === "anthropic" && (
          <AnthropicAccountPoolSettings apiBase={apiBase} accountCount={accounts.length} />
        )}
        {item.name === "google-antigravity" && (
          <CockpitImportPanel
            apiBase={apiBase}
            providerName={item.name}
            onRetryAccounts={authHandlers.onRetryAccounts}
          />
        )}
        <OAuthStatusRow
          itemName={item.name}
          oauth={oauth}
          loggedIn={loggedIn}
          accountsLength={accounts.length}
          flaggedAccount={flaggedAccount}
          busy={busy}
          authHandlers={authHandlers}
        />
        {busy && hintForThisProvider && (
          <OAuthLoginWait itemName={item.name} hint={hintForThisProvider} authHandlers={authHandlers} />
        )}
        <OAuthAccountsStates
          itemName={item.name}
          accounts={accounts}
          accountLoadState={accountLoadState}
          loggedIn={loggedIn}
          switchingAccountId={switchingAccountId}
          busy={busy}
          reserveQuotaSlots={reserveQuotaSlots}
          authHandlers={authHandlers}
        />
      </div>
    </section>
  );
}

function OAuthPanelHead({
  itemName, loggedIn, busy, switchingAccountId, authHandlers,
}: {
  itemName: string;
  loggedIn: boolean;
  busy: boolean;
  switchingAccountId: string | null;
  authHandlers: ProviderAuthHandlers;
}) {
  const t = useT();
  return (
    <div className="providers-block-head">
      <h4>{t("pws.availableAccounts")}</h4>
      {loggedIn ? (
        <button type="button" className="providers-link" disabled={busy || Boolean(switchingAccountId)} onClick={() => void authHandlers.onLogin(itemName, true)}>
          {t("pws.addAccount")}
        </button>
      ) : (
        <button type="button" className="providers-link" disabled={busy} onClick={() => void authHandlers.onLogin(itemName, false)}>
          {busy ? t("prov.waitingBrowser") : t("prov.login")}
        </button>
      )}
    </div>
  );
}

/** Login state line: the dot and the copy both follow the flagged active account. */
function OAuthStatusRow({
  itemName, oauth, loggedIn, accountsLength, flaggedAccount, busy, authHandlers,
}: {
  itemName: string;
  oauth?: { loggedIn: boolean; email?: string; error?: string };
  loggedIn: boolean;
  accountsLength: number;
  flaggedAccount?: OAuthAccountRow;
  busy: boolean;
  authHandlers: ProviderAuthHandlers;
}) {
  const t = useT();
  const statusText = loggedIn
    ? (accountsLength > 0 ? t("pws.loggedInTitle") : (oauth?.email ?? t("pws.loggedInTitle")))
    : (oauth?.error || t("pws.notLoggedInTitle"));
  return (
    <div className="pwi-auth-status-row">
      <span
        className={`pwi-auth-dot ${flaggedAccount ? "pwi-auth-dot--warn" : loggedIn ? "pwi-auth-dot--ok" : "pwi-auth-dot--off"}`}
        aria-hidden="true"
      />
      <span className="pwi-auth-status-text">{statusText}</span>
      <span className="pwi-auth-actions">
        {flaggedAccount && (
          <button type="button" className="providers-link" disabled={busy} onClick={() => void authHandlers.onReauth(itemName, flaggedAccount.id)}>
            {t("pws.reauthenticate")}
          </button>
        )}
        {loggedIn && (
          <button type="button" className="providers-link" onClick={() => void authHandlers.onLogout(itemName)}>{t("prov.logout")}</button>
        )}
      </span>
    </div>
  );
}

function DeviceCodeBlock({ code }: { code: string }) {
  const t = useT();
  const feedback = useCopyFeedback<string>();
  const outcome = feedback.outcomeFor(code);
  const label = outcome === "copied"
    ? t("prov.codeCopied")
    : outcome === "unavailable"
      ? t("prov.linkCopyUnavailable")
      : t("prov.copyCode");
  return (
    <div className="pwi-device-code-wrap">
      <span>{t("prov.deviceCode")}</span>
      <code className="pwi-device-code">{code}</code>
      <button type="button" className="btn btn-primary btn-sm" onClick={() => feedback.copy(code, code)}>
        <span aria-live="polite">{label}</span>
      </button>
    </div>
  );
}

function OAuthLoginWait({
  itemName, hint, authHandlers,
}: {
  itemName: string;
  hint: LoginHint;
  authHandlers: ProviderAuthHandlers;
}) {
  const t = useT();
  return (
    <div className="pwi-auth-wait">
      <span className="pwi-spin-inline" aria-hidden="true" />
      <div className="pwi-auth-wait-copy">
        <div className="pwi-auth-wait-title">{t("prov.waitingBrowser")}</div>
        {hint.deviceCode && <DeviceCodeBlock code={hint.deviceCode} />}
        <LoginUrlBlock url={hint.url ?? ""} />
        {authHandlers.onCancelLogin && (
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => void authHandlers.onCancelLogin?.(itemName)}>
            {t("common.cancel")}
          </button>
        )}
      </div>
    </div>
  );
}

function OAuthAccountsLoadError({
  itemName, onRetryAccounts,
}: {
  itemName: string;
  onRetryAccounts?: ProviderAuthHandlers["onRetryAccounts"];
}) {
  const t = useT();
  return (
    <div className="pwi-auth-state pwi-auth-state--error" role="alert">
      <span>{t("pws.accountsLoadFailed")}</span>
      {onRetryAccounts && (
        <button type="button" className="btn btn-ghost btn-sm" onClick={() => void onRetryAccounts(itemName)}>
          {t("pws.retryAccounts")}
        </button>
      )}
    </div>
  );
}

function OAuthAccountsStates({
  itemName, accounts, accountLoadState, loggedIn, switchingAccountId, busy, reserveQuotaSlots, authHandlers,
}: {
  itemName: string;
  accounts: OAuthAccountRow[];
  accountLoadState: AccountLoadState;
  loggedIn: boolean;
  switchingAccountId: string | null;
  busy: boolean;
  reserveQuotaSlots: boolean;
  authHandlers: ProviderAuthHandlers;
}) {
  const t = useT();
  const loadingFirstRead = accountLoadState === "loading" && accounts.length === 0;
  if (loadingFirstRead) {
    return (
      <div className="pwi-auth-state" role="status">
        <span className="pwi-spin-inline" aria-hidden="true" />
        {t("pws.accountsLoading")}
      </div>
    );
  }
  const emptyButLoggedIn = accountLoadState === "ready" && loggedIn && accounts.length === 0;
  return (
    <>
      {accountLoadState === "error" && (
        <OAuthAccountsLoadError itemName={itemName} onRetryAccounts={authHandlers.onRetryAccounts} />
      )}
      {accounts.length > 0 && (
        <ul className="pwi-auth-list">
          {accounts.map(account => (
            <OAuthAccountListItem
              key={account.id}
              itemName={itemName}
              accounts={accounts}
              account={account}
              switchingAccountId={switchingAccountId}
              busy={busy}
              reserveQuotaSlots={reserveQuotaSlots}
              authHandlers={authHandlers}
            />
          ))}
        </ul>
      )}
      {emptyButLoggedIn && <div className="pwi-auth-state pwi-auth-state--empty">{t("pws.noAccounts")}</div>}
    </>
  );
}

/** One row action: reauth, relabel, or remove. Hidden actions simply do not appear. */
interface AccountRowAction {
  id: string;
  label: string;
  ariaLabel?: string;
  muted?: boolean;
  disabled: boolean;
  run: () => void;
}

function AccountRowActions({
  actions, accountKey,
}: {
  actions: AccountRowAction[];
  accountKey: string;
}) {
  return (
    <>
      {actions.map(action => (
        <button
          key={action.id}
          type="button"
          className={action.muted ? "btn btn-ghost btn-sm pwi-auth-row-remove" : "btn btn-ghost btn-sm"}
          aria-label={action.ariaLabel}
          title={action.ariaLabel}
          disabled={action.disabled}
          onClick={action.run}
        >
          {action.muted
            ? <IconTrash style={{ width: 13, height: 13 }} aria-hidden="true" />
            : action.label}
        </button>
      ))}
      <span className="sr-only">{accountKey}</span>
    </>
  );
}

function OAuthAccountListItem({
  itemName, accounts, account, switchingAccountId, busy, reserveQuotaSlots, authHandlers,
}: {
  itemName: string;
  accounts: OAuthAccountRow[];
  account: OAuthAccountRow;
  switchingAccountId: string | null;
  busy: boolean;
  reserveQuotaSlots: boolean;
  authHandlers: ProviderAuthHandlers;
}) {
  const t = useT();
  const interaction = oauthAccountInteraction(account, switchingAccountId);
  const label = oauthAccountDisplayLabel(accounts, account, t);
  const removeLabel = `${t("common.remove")} — ${label}`;
  const actionDisabled = busy || Boolean(switchingAccountId);
  const actions: AccountRowAction[] = [];
  if (interaction.showReauth) {
    actions.push({
      id: "reauth",
      label: t("pws.reauthenticate"),
      disabled: actionDisabled,
      run: () => void authHandlers.onReauth(itemName, account.id),
    });
  }
  actions.push({
    id: "alias",
    label: t("prov.editAlias"),
    disabled: false,
    run: () => void authHandlers.onEditAlias(itemName, "oauth", account.id, account.alias),
  });
  actions.push({
    id: "remove",
    label: t("common.remove"),
    ariaLabel: removeLabel,
    muted: true,
    disabled: Boolean(switchingAccountId),
    run: () => void authHandlers.onRemoveAccount(itemName, account),
  });
  const showsQuota = account.quota != null || account.quotaUnavailable || (reserveQuotaSlots && account.quota == null);

  return (
    <li className={`pwi-auth-acct${account.active ? " pwi-auth-acct--active" : ""}`}>
      <div className={`pwi-auth-row${account.active ? " pwi-auth-row--active" : ""}`}>
        <OAuthAccountMainButton
          itemName={itemName}
          account={account}
          label={label}
          interaction={interaction}
          authHandlers={authHandlers}
        />
        <AccountRowActions actions={actions} accountKey={account.id} />
      </div>
      {showsQuota && <OAuthAccountQuota account={account} />}
    </li>
  );
}

/** Badges the main row can carry, in the order the row renders them. */
function AccountRowBadges({
  interaction, account, healthLabel,
}: {
  interaction: ReturnType<typeof oauthAccountInteraction>;
  account: OAuthAccountRow;
  healthLabel: string | null;
}) {
  const t = useT();
  const badges: Array<{ id: string; className: string; label: string }> = [];
  if (healthLabel) badges.push({ id: "health", className: oauthHealthBadgeClass(account.health?.status), label: healthLabel });
  if (interaction.showReauth && !healthLabel) badges.push({ id: "reauth", className: "badge badge-amber", label: t("pws.reauth") });
  if (account.active) badges.push({ id: "active", className: "badge badge-primary", label: t("prov.accountActive") });
  if (interaction.switching) badges.push({ id: "switching", className: "badge badge-muted", label: t("pws.accountSwitching") });
  return (
    <>
      {badges.map(badge => (
        <span key={badge.id} className={badge.className}>{badge.label}</span>
      ))}
    </>
  );
}

function OAuthAccountMainButton({
  itemName, account, label, interaction, authHandlers,
}: {
  itemName: string;
  account: OAuthAccountRow;
  label: string;
  interaction: ReturnType<typeof oauthAccountInteraction>;
  authHandlers: ProviderAuthHandlers;
}) {
  const t = useT();
  const maskedId = displayAccountId(account.id);
  const healthLabel = formatOAuthHealthLabel(t, account.health);
  const healthSummary = formatOAuthHealthSummary(t, itemName, account.id, account.health);
  const secondary = [account.email, `${t("prov.accountId")}: ${maskedId}`].filter(Boolean).join(" · ");
  const dotClass = interaction.showReauth
    ? "pwi-auth-dot--warn"
    : (account.active ? "pwi-auth-dot--ok" : "pwi-auth-dot--off");
  return (
    <button type="button" className="pwi-auth-row-main"
      onClick={() => { if (interaction.canActivate) void authHandlers.onSwitchAccount(itemName, account); }}
      aria-current={account.active ? "true" : undefined}
      aria-label={`${label}${account.active ? ` — ${t("pws.accountCurrent")}` : ""}`}
      disabled={interaction.rowDisabled}>
      <span className={`pwi-auth-dot ${dotClass}`} aria-hidden="true" />
      <span className="pwi-auth-row-copy">
        <span className="pwi-auth-row-label">{label}</span>
        <span className="pwi-auth-row-secondary">{secondary}</span>
        {healthSummary && <span className="pwi-auth-row-secondary faint">{healthSummary}</span>}
        {interaction.inCooldown && (
          <span className="pwi-auth-row-secondary faint">{t("pws.healthCooldownHint")}</span>
        )}
      </span>
      <AccountRowBadges interaction={interaction} account={account} healthLabel={healthLabel} />
    </button>
  );
}

function OAuthAccountQuota({ account }: { account: OAuthAccountRow }) {
  const t = useT();
  const { locale } = useI18n();
  if (account.quotaUnavailable) {
    return (
      <div className="pwi-auth-acct-quota">
        <p className="muted pwi-auth-acct-quota-stale">{t("pws.accountQuotaUnavailable")}</p>
      </div>
    );
  }
  return (
    <div className="pwi-auth-acct-quota">
      <ProviderQuotaMeters surface={projectQuotaSurface({
        quota: account.quota ?? null,
        plan: null,
        threshold: QUOTA_SURFACE_THRESHOLD,
        t,
        locale,
        layout: "stacked",
        pending: account.quota == null,
      })} />
    </div>
  );
}

/** Status line under the cockpit import control; only the current outcome shows. */
function CockpitImportStatus({ status, result }: { status: CockpitImportPhase; result: CockpitImportResult | null }) {
  const t = useT();
  if (status === "invalid") return <>{t("pws.cockpitImportInvalid")}</>;
  if (status === "failed") return <>{t("pws.cockpitImportFailed")}</>;
  if (status === "complete" && result) {
    return (
      <>
        {t("pws.cockpitImportComplete", {
          imported: result.importedCount,
          updated: result.updatedCount,
          failed: result.failedCount,
          unsupported: result.unsupportedCount,
        })}
      </>
    );
  }
  return null;
}

function CockpitImportPanel({
  apiBase, providerName, onRetryAccounts,
}: {
  apiBase: string;
  providerName: string;
  onRetryAccounts?: ProviderAuthHandlers["onRetryAccounts"];
}) {
  const t = useT();
  const [busy, setBusy] = useState(false);
  const [status, setStatus] = useState<CockpitImportPhase>("idle");
  const [result, setResult] = useState<CockpitImportResult | null>(null);
  const fileRef = useRef<HTMLInputElement>(null);

  const importFile = (file: File | undefined) => {
    void runCockpitImport(file, {
      busy,
      begin() {
        setBusy(true);
        setStatus("idle");
        setResult(null);
      },
      finish() {
        if (fileRef.current) fileRef.current.value = "";
        setBusy(false);
      },
      setInvalid() { setStatus("invalid"); },
      setFailed() { setStatus("failed"); },
      setComplete(imported) {
        setResult(imported);
        setStatus("complete");
      },
      async postDocument(document) {
        const response = await fetch(`${apiBase}/api/oauth/accounts/import`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ provider: "google-antigravity", format: "cockpit-tools", document }),
        });
        if (!response.ok) return { ok: false, payload: null };
        return { ok: true, payload: await response.json().catch(() => null) };
      },
      async refreshAccounts() {
        await onRetryAccounts?.(providerName);
      },
    });
  };

  const descriptionId = "cockpit-import-description";
  const statusId = "cockpit-import-status";

  return (
    <div className="pwi-auth-add-key">
      <div>
        <div id={descriptionId} className="pwi-auth-row-secondary">
          {t("pws.cockpitImportDescription")}
        </div>
        <label className="sr-only" htmlFor="cockpit-import-file">{t("pws.cockpitImportFileLabel")}</label>
        <input
          ref={fileRef}
          id="cockpit-import-file"
          type="file"
          accept="application/json,.json"
          className="sr-only"
          aria-describedby={`${descriptionId} ${statusId}`}
          disabled={busy}
          onChange={event => { importFile(event.currentTarget.files?.[0]); }}
        />
      </div>
      <button type="button" className="btn btn-ghost btn-sm" disabled={busy}
        onClick={() => fileRef.current?.click()}>
        {busy ? t("pws.cockpitImporting") : t("pws.cockpitImportChooseFile")}
      </button>
      <div id={statusId} role="status" aria-live="polite">
        <CockpitImportStatus status={status} result={result} />
      </div>
    </div>
  );
}
