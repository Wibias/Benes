import { catalogValue, getActiveLocale, type Locale } from "./i18n/shared.ts";

const DIALOG_ID = "benes-admin-token-dialog";
const ACCOUNT_NAME = "Benes";
const TITLE_ID = `${DIALOG_ID}-title`;
const USER_ID = `${DIALOG_ID}-username`;
const SECRET_ID = `${DIALOG_ID}-password`;

export type AdminTokenValidation = "accepted" | "rejected" | "unavailable";
export type AdminTokenVerifier = (token: string) => Promise<AdminTokenValidation>;

const FORM_MARKUP = [
  `<form class="modal-card" method="post" autocomplete="on">`,
  `<div class="modal-head"><h3 id="${TITLE_ID}"></h3></div>`,
  `<div><label class="field-label" for="${USER_ID}"></label>`,
  `<input class="input" id="${USER_ID}" type="text" name="username" autocomplete="username" readonly></div>`,
  `<div style="margin-top: var(--space-4)"><label class="field-label" for="${SECRET_ID}"></label>`,
  `<input class="input" id="${SECRET_ID}" type="password" name="password" autocomplete="current-password" required spellcheck="false" autocapitalize="none"></div>`,
  `<div class="notice notice-err" role="alert" hidden></div>`,
  `<div class="modal-actions">`,
  `<button type="button" class="btn btn-ghost" data-admin-cancel></button>`,
  `<button type="submit" class="btn btn-primary" data-admin-submit></button>`,
  `</div></form>`,
].join("");

function mustQuery<T extends Element>(root: ParentNode, selector: string): T {
  const node = root.querySelector(selector);
  if (!node) throw new Error(`admin token dialog missing ${selector}`);
  return node as T;
}

class ManagementSignInDialog {
  private readonly dialog: HTMLDialogElement;
  private readonly form: HTMLFormElement;
  private readonly password: HTMLInputElement;
  private readonly submit: HTMLButtonElement;
  private readonly alert: HTMLElement;
  private readonly cancel: HTMLButtonElement;
  private readonly previousFocus: HTMLElement | null;
  private finished = false;

  private readonly locale: Locale;
  private readonly verifyToken: AdminTokenVerifier;
  private readonly finish: (value: string | null) => void;

  constructor(
    locale: Locale,
    verifyToken: AdminTokenVerifier,
    finish: (value: string | null) => void,
  ) {
    this.locale = locale;
    this.verifyToken = verifyToken;
    this.finish = finish;
    this.previousFocus = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    this.dialog = document.createElement("dialog");
    this.dialog.id = DIALOG_ID;
    this.dialog.className = "modal-overlay";
    this.dialog.setAttribute("aria-labelledby", TITLE_ID);
    this.dialog.innerHTML = FORM_MARKUP;
    this.form = mustQuery(this.dialog, "form");
    this.form.action = window.location.href;
    mustQuery<HTMLElement>(this.dialog, "h3").textContent = catalogValue(locale, "auth.adminTokenTitle");
    mustQuery<HTMLLabelElement>(this.dialog, `label[for="${USER_ID}"]`).textContent = catalogValue(locale, "auth.adminAccountLabel");
    mustQuery<HTMLLabelElement>(this.dialog, `label[for="${SECRET_ID}"]`).textContent = catalogValue(locale, "auth.adminTokenFieldLabel");
    mustQuery<HTMLInputElement>(this.dialog, `#${USER_ID}`).value = ACCOUNT_NAME;
    this.password = mustQuery(this.dialog, `#${SECRET_ID}`);
    this.submit = mustQuery(this.dialog, "[data-admin-submit]");
    this.cancel = mustQuery(this.dialog, "[data-admin-cancel]");
    this.alert = mustQuery(this.dialog, '[role="alert"]');
    this.cancel.textContent = catalogValue(locale, "common.cancel");
    this.submit.textContent = catalogValue(locale, "common.ok");
    this.form.addEventListener("submit", (event) => this.onSubmit(event));
    this.cancel.addEventListener("click", () => this.closeWith(null));
    this.dialog.addEventListener("cancel", (event) => {
      event.preventDefault();
      this.closeWith(null);
    });
  }

  open(): void {
    document.body.append(this.dialog);
    if (typeof this.dialog.showModal === "function") this.dialog.showModal();
    else this.dialog.setAttribute("open", "");
    queueMicrotask(() => this.password.focus());
  }

  private setBusy(busy: boolean): void {
    this.password.disabled = busy;
    this.submit.disabled = busy;
  }

  private showFailure(copy: string): void {
    this.password.value = "";
    this.setBusy(false);
    this.alert.textContent = copy;
    this.alert.hidden = false;
    this.password.focus();
  }

  private closeWith(value: string | null): void {
    if (this.finished) return;
    this.finished = true;
    this.dialog.returnValue = value ?? "";
    if (this.dialog.open) this.dialog.close();
    this.dialog.remove();
    this.previousFocus?.focus();
    this.finish(value);
  }

  private onSubmit(event: Event): void {
    event.preventDefault();
    const token = this.password.value.trim();
    if (!token) {
      this.password.value = "";
      this.password.reportValidity();
      return;
    }
    this.setBusy(true);
    this.alert.hidden = true;
    this.alert.textContent = "";
    void this.verifyToken(token).then((result) => {
      if (this.finished) return;
      if (result === "accepted") {
        this.closeWith(token);
        return;
      }
      this.showFailure(catalogValue(this.locale, result === "rejected" ? "auth.adminTokenRejected" : "auth.adminTokenUnavailable"));
    }).catch(() => {
      if (this.finished) return;
      this.showFailure(catalogValue(this.locale, "auth.adminTokenUnavailable"));
    });
  }
}

/**
 * Password-manager-compatible sign-in form for the in-memory management token.
 * Benes never writes the submitted secret to web storage.
 */
export function promptForAdminToken(
  verifyToken: AdminTokenVerifier,
  locale: Locale = getActiveLocale(),
): Promise<string | null> {
  return new Promise((resolve) => {
    const dialog = new ManagementSignInDialog(locale, verifyToken, resolve);
    dialog.open();
  });
}
