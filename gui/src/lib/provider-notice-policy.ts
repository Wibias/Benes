export const PROVIDER_HARNESS_SYNC_TOAST_MS = 60_000;
export const PROVIDER_SUCCESS_TOAST_MS = 4_500;
export const PROVIDER_ERROR_TOAST_MS = 15_000;

export type ProviderNotifyOptions = { offerHarnessSync?: boolean };

export function providerToastDismissMs(input: { ok: boolean; offerHarnessSync?: boolean }): number {
  if (!input.ok) return PROVIDER_ERROR_TOAST_MS;
  if (input.offerHarnessSync) return PROVIDER_HARNESS_SYNC_TOAST_MS;
  return PROVIDER_SUCCESS_TOAST_MS;
}

export function providerNoticeActionParts(message: string): { pre: string; post: string } | null {
  const token = "{action}";
  const index = message.indexOf(token);
  if (index < 0) return null;
  return { pre: message.slice(0, index), post: message.slice(index + token.length) };
}

export function harnessSyncResultKey(result: { attempted: number; failed: number }): "prov.harnessSyncNone" | "prov.harnessSyncOk" | "prov.harnessSyncFail" {
  if (result.attempted === 0) return "prov.harnessSyncNone";
  if (result.failed > 0) return "prov.harnessSyncFail";
  return "prov.harnessSyncOk";
}
