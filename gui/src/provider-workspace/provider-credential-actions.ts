/**
 * Access-tab credential mutations. HTTP lives in provider-credential-api;
 * this module maps responses onto notices and which boards to republish.
 */
import type { TFn } from "../i18n/shared.ts";
import { oauthAccountDisplayLabel } from "./auth.ts";
import {
  deleteApiKey,
  deleteOAuthAccount,
  managementFailureCopy,
  postApiKey,
  putActiveApiKey,
  putActiveOAuthAccount,
  putApiKeyAlias,
  putOAuthAccountAlias,
  type ApiKeyRow,
  type OAuthAccountRow,
} from "./provider-credential-api.ts";

export type CredentialBoard = "accounts" | "keys" | "keys-and-config";

export type CredentialActionResult =
  | { status: "ignored" }
  | { status: "failed"; message: string }
  | { status: "ok"; message: string; board: CredentialBoard };

type Copy = TFn;

export async function activateStoredOAuthAccount(input: {
  apiBase: string;
  provider: string;
  account: OAuthAccountRow;
  roster: OAuthAccountRow[];
  blocked: boolean;
  t: Copy;
}): Promise<CredentialActionResult> {
  if (input.blocked || input.account.active || input.account.needsReauth) return { status: "ignored" };
  const email = oauthAccountDisplayLabel(input.roster, input.account, input.t);
  try {
    const response = await putActiveOAuthAccount(input.apiBase, input.provider, input.account.id);
    if (!response.ok) return { status: "failed", message: input.t("prov.accountSwitchFail") };
    return { status: "ok", message: input.t("prov.accountSwitched", { email }), board: "accounts" };
  } catch {
    return { status: "failed", message: input.t("prov.accountSwitchFail") };
  }
}

export async function dropStoredOAuthAccount(input: {
  apiBase: string;
  provider: string;
  account: OAuthAccountRow;
  roster: OAuthAccountRow[];
  t: Copy;
  confirm: (message: string) => boolean;
}): Promise<CredentialActionResult> {
  const email = oauthAccountDisplayLabel(input.roster, input.account, input.t);
  if (!input.confirm(input.t("prov.accountRemoveConfirm", { email }))) return { status: "ignored" };
  try {
    const response = await deleteOAuthAccount(input.apiBase, input.provider, input.account.id);
    if (!response.ok) return { status: "failed", message: input.t("prov.accountRemoveFail", { email }) };
    return { status: "ok", message: input.t("prov.accountRemoved", { email }), board: "accounts" };
  } catch {
    return { status: "failed", message: input.t("prov.accountRemoveFail", { email }) };
  }
}

export async function activateStoredApiKey(input: {
  apiBase: string;
  provider: string;
  entry: ApiKeyRow;
  t: Copy;
}): Promise<CredentialActionResult> {
  if (input.entry.active) return { status: "ignored" };
  const response = await putActiveApiKey(input.apiBase, input.provider, input.entry.id);
  if (response.ok) {
    return {
      status: "ok",
      message: input.t("prov.keySwitched", { key: input.entry.label ?? input.entry.masked }),
      board: "keys",
    };
  }
  return { status: "failed", message: await managementFailureCopy(response, input.t("prov.keySwitchFail")) };
}

export async function dropStoredApiKey(input: {
  apiBase: string;
  provider: string;
  entry: ApiKeyRow;
  t: Copy;
  confirm: (message: string) => boolean;
}): Promise<CredentialActionResult> {
  const key = input.entry.label ?? input.entry.masked;
  if (!input.confirm(input.t("prov.keyRemoveConfirm", { key }))) return { status: "ignored" };
  const response = await deleteApiKey(input.apiBase, input.provider, input.entry.id);
  if (!response.ok) return { status: "ignored" };
  return { status: "ok", message: input.t("prov.keyRemoved", { key }), board: "keys-and-config" };
}

export async function storeApiKeySecret(input: {
  apiBase: string;
  provider: string;
  secret: string;
  t: Copy;
}): Promise<CredentialActionResult> {
  const key = input.secret.trim();
  if (!key) return { status: "ignored" };
  try {
    const response = await postApiKey(input.apiBase, input.provider, key);
    if (!response.ok) {
      return { status: "failed", message: await managementFailureCopy(response, input.t("prov.keyAddFail")) };
    }
    return { status: "ok", message: input.t("prov.keyAdded", { name: input.provider }), board: "keys-and-config" };
  } catch {
    return { status: "failed", message: input.t("prov.keyAddFail") };
  }
}

export async function renameStoredCredential(input: {
  apiBase: string;
  provider: string;
  kind: "oauth" | "api-key";
  id: string;
  current: string | undefined;
  t: Copy;
  prompt: (message: string, initial: string) => string | null;
}): Promise<CredentialActionResult> {
  const entered = input.prompt(input.t("prov.aliasPrompt"), input.current ?? "");
  if (entered === null) return { status: "ignored" };
  const alias = entered.trim();
  const response = input.kind === "oauth"
    ? await putOAuthAccountAlias(input.apiBase, input.provider, input.id, alias)
    : await putApiKeyAlias(input.apiBase, input.provider, input.id, alias);
  if (!response.ok) {
    return { status: "failed", message: await managementFailureCopy(response, input.t("prov.aliasSaveFailed")) };
  }
  return {
    status: "ok",
    message: input.t("prov.aliasSaved"),
    board: input.kind === "oauth" ? "accounts" : "keys",
  };
}
