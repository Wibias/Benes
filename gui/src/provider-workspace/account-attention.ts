export function buildActiveAccountNeedsReauthMap<T extends { id: string; active: boolean }>(
  accountSets: Record<string, { activeAccountId: string | null; accounts: T[] }>,
  codexActiveNeedsReauth: boolean,
  accountNeedsReauth: (account: T | null | undefined) => boolean,
): Record<string, boolean> {
  const flagged: Record<string, boolean> = {};
  Object.keys(accountSets).forEach(providerId => {
    const pack = accountSets[providerId];
    if (!pack) return;
    const selected = pack.accounts.find(row => row.active)
      ?? pack.accounts.find(row => row.id === pack.activeAccountId);
    if (accountNeedsReauth(selected)) flagged[providerId] = true;
  });
  if (codexActiveNeedsReauth) flagged.openai = true;
  return flagged;
}
