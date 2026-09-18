export type ConnectionTestMessageKey =
  | "pws.connectionNotApplicable"
  | "pws.connectionOk"
  | "pws.connectionFailed";

export type ConnectionTestOutcome = {
  ok: boolean;
  text?: string;
  messageKey?: ConnectionTestMessageKey;
};

function recordFromUnknown(value: unknown): Record<string, unknown> {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : {};
}

export function connectionTestOutcome(responseOk: boolean, payload: unknown): ConnectionTestOutcome {
  const test = recordFromUnknown(payload);
  const ok = test.ok !== false && responseOk;
  if (typeof test.message === "string" && test.message) return { ok, text: test.message };
  if (typeof test.error === "string" && test.error) return { ok, text: test.error };
  if (test.applicable === false) return { ok, messageKey: "pws.connectionNotApplicable" };
  return { ok, messageKey: ok ? "pws.connectionOk" : "pws.connectionFailed" };
}

export function nextDetailTabIndex(key: string, index: number, length: number): number | null {
  if (length <= 0) return null;
  if (key === "ArrowRight") return (index + 1) % length;
  if (key === "ArrowLeft") return (index - 1 + length) % length;
  if (key === "Home") return 0;
  if (key === "End") return length - 1;
  return null;
}

export type DetailStatusPillKind = "ready" | "disabled" | "attention";

export function detailStatusPillKind(status: string): DetailStatusPillKind {
  if (status === "ready" || status === "disabled") return status;
  return "attention";
}

export function detailStatusPill(
  kind: DetailStatusPillKind,
  labels: { connected: string; disabled: string; attention: string },
): { cls: string; label: string } {
  if (kind === "ready") return { cls: "providers-pill--on", label: labels.connected };
  if (kind === "disabled") return { cls: "providers-pill--muted", label: labels.disabled };
  return { cls: "providers-pill--amber", label: labels.attention };
}

export function overviewReauthAccountId<T extends { active: boolean; needsReauth?: boolean; id: string }>(
  accounts: readonly T[],
): string | undefined {
  const active = accounts.find(account => account.active && account.needsReauth)
    ?? accounts.find(account => account.needsReauth);
  return active?.id;
}
