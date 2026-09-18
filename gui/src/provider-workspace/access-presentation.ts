import type { SelectionCapabilities } from "./auth";

export type AccessEmptyHintKey =
  | "prov.access.forwardHint"
  | "prov.access.localHint"
  | "prov.access.noneHint";

export type OverviewDefaultAccessKind = "openai-api" | "chatgpt-pool" | "valid" | "none";

export type CredentialSelectionFacts = {
  poolMode: boolean;
  showStrategy: boolean;
  showAutoSwitch: boolean;
  showStickyLimit: boolean;
  stickyDisplay: number | "—";
  thresholdLabel: string;
};

export type OAuthAccountInteraction = {
  switching: boolean;
  showReauth: boolean;
  inCooldown: boolean;
  canActivate: boolean;
  rowDisabled: boolean;
};

export function accessEmptyHintKey(
  authMode: string | undefined,
  localProvider: boolean,
): AccessEmptyHintKey {
  if (authMode === "forward") return "prov.access.forwardHint";
  if (authMode === "local" || localProvider) return "prov.access.localHint";
  return "prov.access.noneHint";
}

export function overviewConnected(input: {
  oauthEmail?: string;
  oauthLoggedIn?: boolean;
  hasActiveKey?: boolean;
}): boolean {
  return Boolean(input.oauthEmail || input.oauthLoggedIn || input.hasActiveKey);
}

export function overviewDefaultAccessKind(input: {
  defaultMethodId?: string;
  defaultAccess?: boolean;
  connected: boolean;
}): OverviewDefaultAccessKind {
  if (input.defaultMethodId === "api") return "openai-api";
  if (input.defaultAccess) return "chatgpt-pool";
  return input.connected ? "valid" : "none";
}

export function overviewListedModels(
  selectedModels: readonly string[],
  availableModels: readonly string[],
): { listed: readonly string[]; unavailableCount: number } {
  const listed = selectedModels.length > 0 ? selectedModels : availableModels;
  const unavailableCount = selectedModels.length > 0 && availableModels.length > 0
    ? selectedModels.filter(id => !availableModels.includes(id)).length
    : 0;
  return { listed, unavailableCount };
}

export function overviewLastValidatedAt(
  lastValidated: number | null | undefined,
  fallback: number | null,
): number | null {
  return lastValidated === undefined ? fallback : lastValidated;
}

/**
 * A recent event's severity as the one tone every provider event list renders.
 *
 * The listener owns this classification: `internal/provideractivity` records `info`,
 * `warn` or `error`, and `/api/providers/workspace` forwards that field, so the
 * dashboard reads the field instead of guessing a tone from an event type or a
 * rendered label. An unknown severity stays neutral, and so does `info`: no event
 * severity means healthy, and an ordinary row must not be painted green for existing.
 */
export type ProviderEventSeverity =
  | { tone: "warn"; className: "is-warn" }
  | { tone: "error"; className: "is-error" }
  | { tone: "off"; className: null };

export function providerEventSeverity(severity: string | undefined): ProviderEventSeverity {
  if (severity === "error") return { tone: "error", className: "is-error" };
  if (severity === "warn") return { tone: "warn", className: "is-warn" };
  return { tone: "off", className: null };
}

/**
 * The accessible word behind a decorated row. Colour plus a glyph is not a cue a screen
 * reader can read, so a warn/error event announces what it is.
 */
export function providerEventSeverityWordKey(
  tone: "warn" | "error",
): "prov.event.warn" | "prov.event.error" {
  return tone === "warn" ? "prov.event.warn" : "prov.event.error";
}

/**
 * The fleet issue severity as a status tone.
 *
 * The listener already owns this classification (`providers_workspace.go` sends
 * `error` only for `provider_health_failure`, `warn` for the recoverable codes),
 * so the Overview reads the same field the events list reads instead of keeping
 * a second code table in the dashboard. An unknown severity stays neutral: an
 * issue row is never proven healthy.
 */
export type OverviewIssueTone = "error" | "warn" | "off";

export function overviewIssueTone(severity: string | undefined): OverviewIssueTone {
  if (severity === "error") return "error";
  if (severity === "warn") return "warn";
  return "off";
}

/** One issue row class, so Overview and the fleet events share the tone vocabulary. */
export function overviewIssueToneClass(severity: string | undefined): string {
  return `providers-overview-issue-status is-${overviewIssueTone(severity)}`;
}

export function recentOverviewEvents<T>(events: readonly T[]): T[] {
  return events.slice(-4).reverse();
}

export function recentAccessEvents<T>(events: readonly T[]): T[] {
  return events.slice(-4).reverse();
}

export type AccessCredentialLane = "oauth" | "api-key";

export function accessShowsCredentialLaneSwitch(oauthAvailable: boolean, keysAvailable: boolean): boolean {
  return oauthAvailable && keysAvailable;
}

export function accessCredentialLane(
  oauthAvailable: boolean,
  keysAvailable: boolean,
  selected: AccessCredentialLane,
): AccessCredentialLane | null {
  if (oauthAvailable && keysAvailable) return selected;
  if (oauthAvailable) return "oauth";
  if (keysAvailable) return "api-key";
  return null;
}

export function credentialSelectionFacts(
  selection: Pick<SelectionCapabilities, "mode" | "showAutoSwitch" | "showStickyLimit" | "autoSwitchThreshold" | "stickyLimit">,
): CredentialSelectionFacts {
  const poolMode = selection.mode !== "direct";
  return {
    poolMode,
    showStrategy: poolMode,
    showAutoSwitch: poolMode && Boolean(selection.showAutoSwitch),
    showStickyLimit: poolMode && Boolean(selection.showStickyLimit),
    stickyDisplay: selection.stickyLimit ?? "—",
    thresholdLabel: selection.autoSwitchThreshold != null
      ? `${selection.autoSwitchThreshold}%`
      : "—",
  };
}

export type CredentialSelectionDisplay = {
  mode: "pool" | "direct" | "—";
  strategy: string;
  autoSwitch: string;
  sticky: string | number;
};

/** One-account pools keep the Credential selection rows, with dashes instead of live values. */
export function credentialSelectionDisplay(
  facts: CredentialSelectionFacts,
  strategy: string,
  interactive: boolean,
): CredentialSelectionDisplay {
  if (!interactive) {
    return { mode: "—", strategy: "—", autoSwitch: "—", sticky: "—" };
  }
  return {
    mode: facts.poolMode ? "pool" : "direct",
    strategy: facts.showStrategy ? strategy : "—",
    autoSwitch: facts.showAutoSwitch ? facts.thresholdLabel : "—",
    sticky: facts.showStickyLimit ? facts.stickyDisplay : "—",
  };
}

/** Selection-order control is only meaningful when more than one credential can rotate. */
export function accessShowsSelectionOrder(accountCount: number): boolean {
  return accountCount > 1;
}

/** Rows the Access table shows before its body becomes a scroll window. */
export const ACCESS_VISIBLE_ROWS = 3;

/**
 * A body that fits its rows must not clip them: the row action menu hangs below the row, and
 * an overflow container cuts it off. Only a pool longer than the table's window scrolls, and
 * there the menu can be scrolled into view.
 */
export function accessTableScrolls(rowCount: number): boolean {
  return rowCount > ACCESS_VISIBLE_ROWS;
}

export function oauthAccountInteraction(
  account: {
    id: string;
    active: boolean;
    needsReauth?: boolean;
    health?: { status?: "healthy" | "cooldown" | "reauth_required" | "warning" };
  },
  switchingAccountId: string | null,
): OAuthAccountInteraction {
  const showReauth = Boolean(account.needsReauth) || account.health?.status === "reauth_required";
  const inCooldown = account.health?.status === "cooldown";
  const switching = switchingAccountId === account.id;
  return {
    switching,
    showReauth,
    inCooldown,
    canActivate: !account.active && !showReauth && !inCooldown && !switchingAccountId,
    rowDisabled: Boolean(showReauth || inCooldown || (switchingAccountId && !switching)),
  };
}

export function oauthSessionLoggedIn(
  accountsLength: number,
  oauth?: { loggedIn?: boolean },
): boolean {
  return accountsLength > 0 || oauth?.loggedIn === true;
}
