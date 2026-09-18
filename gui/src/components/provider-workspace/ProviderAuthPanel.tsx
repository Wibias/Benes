/**
 * ProviderAuthPanel — credential lanes for the workspace Access tab.
 *
 * The call site states the lane it renders (`forceSurface`) and the provider
 * shape is already resolved by `deriveAccessDescriptor`, so this module only
 * composes the lane component and supplies the shared defaults.
 */
import type { ReactNode } from "react";
import type { WorkspaceItem } from "../../provider-workspace/catalog";
import type { AccountLoadState } from "../../hooks/useProviderCredentials";
import type { LoginHint } from "../../pages/providers-chrome";
import type { ApiKeyRow, OAuthAccountRow } from "../../provider-workspace/provider-credential-api";
import type { ProviderAuthSurface, ProviderOAuthLoginView } from "../../provider-workspace/auth";
import type { ProviderAuthHandlers } from "./ProviderAccess";
import { OAuthAccountsPanel } from "./auth-panel-oauth";
import { ApiKeysPanel } from "./auth-panel-keys";

const NO_ACCOUNTS: OAuthAccountRow[] = [];
const NO_KEYS: ApiKeyRow[] = [];

/** Credential rows the Access tab has already loaded for this provider. */
interface ProviderAuthPanelData {
  oauth?: ProviderOAuthLoginView;
  accounts?: OAuthAccountRow[];
  keys?: ApiKeyRow[];
  accountLoadState?: AccountLoadState;
  switchingAccountId?: string | null;
}

/** In-flight login/switch activity shared with the Access tab. */
interface ProviderAuthPanelActivity {
  busy?: boolean;
  loginHint?: LoginHint | null;
  authHandlers?: ProviderAuthHandlers;
}

export interface ProviderAuthPanelProps extends ProviderAuthPanelData, ProviderAuthPanelActivity {
  item: WorkspaceItem;
  apiBase: string;
  /** The lane this call site renders. */
  forceSurface: ProviderAuthSurface;
  omitChrome?: boolean;
  heading?: ReactNode;
}

export default function ProviderAuthPanel(props: ProviderAuthPanelProps) {
  const { forceSurface, authHandlers, item, omitChrome = false } = props;
  if (forceSurface === null || !authHandlers) return null;
  if (forceSurface === "api-keys") {
    return (
      <ApiKeysPanel
        item={item}
        keys={props.keys ?? NO_KEYS}
        authHandlers={authHandlers}
        omitChrome={omitChrome}
        heading={props.heading}
      />
    );
  }
  return (
    <OAuthAccountsPanel
      item={item}
      apiBase={props.apiBase}
      oauth={props.oauth}
      accounts={props.accounts ?? NO_ACCOUNTS}
      accountLoadState={props.accountLoadState ?? "ready"}
      switchingAccountId={props.switchingAccountId ?? null}
      busy={props.busy ?? false}
      loginHint={props.loginHint}
      authHandlers={authHandlers}
      omitChrome={omitChrome}
    />
  );
}
