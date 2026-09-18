export type DetailTab = "overview" | "access" | "configuration";

/** Saves the Configuration pane draft; resolves false when the listener refused it. */
export type DetailConfigSaver = () => Promise<boolean>;

/** The Configuration pane registers its saver so a tab or back request can flush it first. */
export type DetailConfigSaveRegistrar = (saver: DetailConfigSaver | null) => void;

export function detailAccessCredentialPresent(apiLane: unknown, keys: { length?: number } | undefined): boolean {
  return Boolean(apiLane) || Boolean(keys?.length);
}

export function scopedAccountsFocusToken(
  accountsFocusProvider: string | null | undefined,
  itemName: string,
  accountsFocusToken: number,
): number {
  return accountsFocusProvider === itemName ? accountsFocusToken : 0;
}

export function clampedDetailTab(showAccess: boolean, tab: DetailTab): DetailTab {
  return !showAccess && tab === "access" ? "overview" : tab;
}

export function accountsFocusAdjustment(
  token: number,
  seenToken: number,
): { seen: number; tab: DetailTab | null } | null {
  if (token === seenToken) return null;
  return { seen: token, tab: token ? "access" : null };
}

export function pendingLeaveTab(
  next: DetailTab,
  configDirty: boolean,
): { pending: DetailTab } | { tab: DetailTab } {
  if (next !== "configuration" && configDirty) return { pending: next };
  return { tab: next };
}

export function pendingLeaveBack(configDirty: boolean): "pending" | "leave" {
  return configDirty ? "pending" : "leave";
}

export function closeOverflowDetails(root: { open: boolean } | null | undefined): void {
  if (root) root.open = false;
}

export function closeDetailsOverflow(event: { currentTarget: EventTarget & { closest: (selector: string) => { open: boolean } | null } }): void {
  closeOverflowDetails(event.currentTarget.closest("details"));
}
