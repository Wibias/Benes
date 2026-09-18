/** Field-visibility policy for the add-provider form and setup panes. */
export type AddProviderAuthPanel =
  | { kind: "forward" }
  | { kind: "local" }
  | { kind: "free-tier" }
  | { kind: "api-key"; showDashboard: boolean; showTransport: boolean };

export function addProviderSetupGuideVisible(input: {
  isReservedForward: boolean;
  isCustom: boolean;
  isLocal: boolean;
  keyOptional?: boolean;
  note?: string;
}): boolean {
  return !input.isReservedForward && !input.isCustom && !input.isLocal && !input.keyOptional && !!input.note;
}

export function addProviderBaseUrlPlaceholderHintVisible(baseUrl: string): boolean {
  return /\{[^}]*\}/.test(baseUrl);
}

export function addProviderShowsEndpointChoices(
  choices: Array<{ id: string }> | undefined,
): boolean {
  return Boolean(choices && choices.length > 0);
}

export function addProviderEndpointLabelKey(
  choiceId: string,
): "modal.endpoint.tokenPlan" | "modal.endpoint.payAsYouGo" | "modal.endpoint.custom" | null {
  if (choiceId === "token-plan") return "modal.endpoint.tokenPlan";
  if (choiceId === "payg") return "modal.endpoint.payAsYouGo";
  if (choiceId === "custom") return "modal.endpoint.custom";
  return null;
}

export function addProviderAuthPanel(input: {
  authMode: string;
  keyOptional?: boolean;
  dashboardUrl?: string;
  adapter: string;
}): AddProviderAuthPanel {
  if (input.authMode === "forward") return { kind: "forward" };
  if (input.authMode === "local") return { kind: "local" };
  if (input.keyOptional) return { kind: "free-tier" };
  return {
    kind: "api-key",
    showDashboard: Boolean(input.dashboardUrl),
    showTransport: input.adapter === "anthropic" && input.authMode === "key",
  };
}

export function addProviderShowsPrivateNetworkHint(allowPrivateNetwork: boolean | undefined): boolean {
  return allowPrivateNetwork ?? false;
}

export type AddProviderAccessKind = "local" | "free" | "paid";

export function addProviderAccessKind(input: { isLocal: boolean; tier: string }): AddProviderAccessKind {
  if (input.isLocal) return "local";
  if (input.tier === "free") return "free";
  return "paid";
}

export function addProviderConnectionCapabilities(input: {
  auth: string;
  oauthProvider?: string;
  oauthSupported: string[];
  keyOnOauth?: boolean;
}): { canOauth: boolean; canKey: boolean; oauthId: string; oauthReady: boolean } {
  const canOauth = input.auth === "oauth";
  const canKey = input.auth === "key" || (canOauth && input.keyOnOauth !== false);
  const oauthId = input.oauthProvider ?? "";
  return {
    canOauth,
    canKey,
    oauthId,
    oauthReady: canOauth && input.oauthSupported.includes(oauthId),
  };
}

/** Inline API-key Connect: selected method plus a key, unless the preset is keyless. */
export function addProviderKeyConnectEnabled(input: {
  authMode: string;
  apiKey: string;
  saving?: boolean;
  keyOptional?: boolean;
}): boolean {
  if (input.saving) return false;
  if (input.authMode !== "key") return false;
  if (input.keyOptional) return true;
  return input.apiKey.trim() !== "";
}

export type AccountSetupActionKind = "codex" | "logged-in" | "busy" | "logged-out";

export function accountSetupActionKind(input: {
  kind: string;
  loggedIn: boolean;
  busy: boolean;
}): AccountSetupActionKind {
  if (input.kind === "codex") return "codex";
  if (input.loggedIn) return "logged-in";
  if (input.busy) return "busy";
  return "logged-out";
}

export function accountSetupStatusCopy(
  loggedIn: boolean,
  email: string | undefined,
): { kind: "email"; email: string } | { kind: "logged-in" } | { kind: "logged-out" } {
  if (!loggedIn) return { kind: "logged-out" };
  if (email) return { kind: "email", email };
  return { kind: "logged-in" };
}

export function accountSetupCodexLoginKind(
  loggedIn: boolean,
  busy: boolean,
): "enabling" | "add" | "login" {
  if (busy) return "enabling";
  if (loggedIn) return "add";
  return "login";
}
