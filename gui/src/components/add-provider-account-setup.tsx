/**
 * Account-login setup pane for the Add Provider modal.
 *
 * One renderer draws every account pane. Which actions a row offers — and in
 * which order — comes from the account action kind in
 * `lib/add-provider-form-policy.ts`, so the codex lane, the logged-in lane, a
 * login already in flight, and a signed-out row cannot drift apart.
 */
import { useT, type TFn } from "../i18n/shared";
import { ProviderMark } from "./ProviderMark";
import type { AccountLoginRow, AccountLoginStatus } from "./provider-catalog/ProviderCatalog";
import {
  accountSetupActionKind,
  accountSetupCodexLoginKind,
  accountSetupStatusCopy,
  type AccountSetupActionKind,
} from "../lib/add-provider-form-policy";
import { OPENAI_ACCOUNT_ACCESS_HASH } from "../provider-workspace/provider-access-hash";

/** A rendered account action: an `href` makes it a link, otherwise a button. */
type AccountSetupAction = {
  key: string;
  label: string;
  primary?: boolean;
  disabled?: boolean;
  href?: string;
  run: () => void;
};

type AccountSetupHandlers = {
  onLogin?: (provider: string, addAccount?: boolean) => void;
  onCancelLogin?: (provider: string) => void;
  onLogout?: (provider: string) => void;
  onManage?: (provider: string) => void;
};

type AccountSetupActionInput = {
  row: AccountLoginRow;
  loggedIn: boolean;
  busy: boolean;
  t: TFn;
} & AccountSetupHandlers;

function accountSetupActionClass(action: AccountSetupAction): string {
  return action.primary ? "btn btn-primary" : "btn btn-ghost";
}

function codexLoginLabelKey(loggedIn: boolean, busy: boolean): "codexAuth.enablingOpenai" | "modal.accountAdd" | "modal.accountLogin" {
  const loginKind = accountSetupCodexLoginKind(loggedIn, busy);
  if (loginKind === "enabling") return "codexAuth.enablingOpenai";
  return loginKind === "add" ? "modal.accountAdd" : "modal.accountLogin";
}

/** The codex lane: manage the pool entry, then (re)start the device login. */
function codexAccountActions(input: AccountSetupActionInput): AccountSetupAction[] {
  const { row, loggedIn, busy, t, onLogin } = input;
  const actions: AccountSetupAction[] = [];
  if (loggedIn) {
    actions.push({
      key: "manage",
      label: t("modal.accountManage"),
      href: row.href ?? `#${OPENAI_ACCOUNT_ACCESS_HASH}`,
      run: () => undefined,
    });
  }
  if (onLogin) {
    actions.push({
      key: "login",
      label: t(codexLoginLabelKey(loggedIn, busy)),
      primary: !loggedIn,
      disabled: busy,
      run: () => onLogin(row.id),
    });
  }
  return actions;
}

/** A row this app already signed in: manage, add another, cancel, sign out. */
function loggedInAccountActions(input: AccountSetupActionInput): AccountSetupAction[] {
  const { row, busy, t, onLogin, onCancelLogin, onLogout, onManage } = input;
  const actions: AccountSetupAction[] = [];
  if (onManage) actions.push({ key: "manage", label: t("modal.accountManage"), run: () => onManage(row.id) });
  if (onLogin) {
    actions.push({
      key: "add",
      label: busy ? t("prov.waitingBrowser") : t("modal.accountAdd"),
      disabled: busy,
      run: () => onLogin(row.id, true),
    });
  }
  if (busy && onCancelLogin) {
    actions.push({ key: "cancel", label: t("common.cancel"), run: () => onCancelLogin(row.id) });
  }
  if (onLogout && !busy) {
    actions.push({ key: "logout", label: t("modal.accountLogout"), run: () => onLogout(row.id) });
  }
  return actions;
}

function accountActionsFor(kind: AccountSetupActionKind, input: AccountSetupActionInput): AccountSetupAction[] {
  if (kind === "codex") return codexAccountActions(input);
  if (kind === "logged-in") return loggedInAccountActions(input);
  if (kind === "busy") {
    const { row, t, onCancelLogin } = input;
    return onCancelLogin ? [{ key: "cancel", label: t("common.cancel"), run: () => onCancelLogin(row.id) }] : [];
  }
  const { row, t, onLogin } = input;
  return onLogin ? [{ key: "login", label: t("modal.accountLogin"), primary: true, run: () => onLogin(row.id) }] : [];
}

function AccountSetupActions(input: AccountSetupActionInput) {
  const kind = accountSetupActionKind({ kind: input.row.kind, loggedIn: input.loggedIn, busy: input.busy });
  const actions = accountActionsFor(kind, input);
  return (
    <>
      {actions.map(action => (action.href
        ? <a key={action.key} className={accountSetupActionClass(action)} href={action.href}>{action.label}</a>
        : (
          <button
            key={action.key}
            type="button"
            className={accountSetupActionClass(action)}
            disabled={action.disabled}
            onClick={action.run}
          >
            {action.label}
          </button>
        )))}
    </>
  );
}

/** Account status line: the signed-in email when there is one, else the key copy. */
function accountStatusText(loggedIn: boolean, email: string | undefined, t: TFn): string {
  const copy = accountSetupStatusCopy(loggedIn, email);
  if (copy.kind === "email") return copy.email;
  return t(copy.kind === "logged-in" ? "modal.accountLoggedIn" : "modal.accountLoggedOut");
}

export function AccountSetup(props: {
  row: AccountLoginRow;
  status?: AccountLoginStatus;
  busy: boolean;
} & AccountSetupHandlers) {
  const t = useT();
  const loggedIn = !!props.status?.loggedIn;
  return (
    <div className="add-provider-setup">
      <header className="add-provider-setup-head">
        <ProviderMark name={props.row.id} className="add-provider-setup-icon" />
        <div>
          <h3>{props.row.label}</h3>
          <p className="muted">{accountStatusText(loggedIn, props.status?.email, t)}</p>
        </div>
      </header>
      <div className="add-provider-account-actions">
        <AccountSetupActions
          row={props.row}
          loggedIn={loggedIn}
          busy={props.busy}
          t={t}
          onLogin={props.onLogin}
          onCancelLogin={props.onCancelLogin}
          onLogout={props.onLogout}
          onManage={props.onManage}
        />
      </div>
    </div>
  );
}
