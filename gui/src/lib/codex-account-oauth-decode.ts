/** Wire decode for Codex add-account OAuth start, poll ticks, and paste-code. */

export function codexAuthLoginAccountId(
  reauthAccountId: string | undefined,
  requestedId: string | undefined,
): string {
  return reauthAccountId ?? requestedId?.trim() ?? "";
}

export function codexAuthLoginRequestBody(
  reauthAccountId: string | undefined,
  requestedId: string | undefined,
): Record<string, unknown> {
  const accountId = codexAuthLoginAccountId(reauthAccountId, requestedId);
  if (reauthAccountId) return { id: reauthAccountId, reauth: true };
  return accountId ? { id: accountId } : {};
}

export function codexAuthLoginStatusUrl(
  apiBase: string,
  flowId: string | undefined,
  accountId: string,
  reauthAccountId: string | undefined,
): string {
  const reauthQuery = reauthAccountId ? "&reauth=1" : "";
  const fid = flowId ?? "";
  if (!fid) return `${apiBase}/api/codex-auth/login-status`;
  const accountQuery = accountId ? `&accountId=${encodeURIComponent(accountId)}` : "";
  return `${apiBase}/api/codex-auth/login-status?flowId=${encodeURIComponent(fid)}${accountQuery}${reauthQuery}`;
}

export type CodexAuthLoginOpened =
  | { kind: "opened"; url: string; flowId: string | null }
  | { kind: "error"; error: string }
  | { kind: "noop" };

export function decodeCodexAuthLoginOpened(
  data: { url?: string; flowId?: string; error?: string } | null | undefined,
): CodexAuthLoginOpened {
  if (!data) return { kind: "noop" };
  if (data.url) return { kind: "opened", url: data.url, flowId: data.flowId ?? null };
  if (data.error && !data.url) return { kind: "error", error: data.error };
  return { kind: "noop" };
}

export type CodexAuthConflictStep =
  | { kind: "dead" }
  | { kind: "retry" }
  | { kind: "already-in-progress" }
  | { kind: "continue" };

export function decodeCodexAuthConflictStep(input: {
  firstStatus: number;
  alive: boolean;
  aborted: boolean;
  retryStatus?: number;
}): CodexAuthConflictStep {
  if (input.firstStatus !== 409) return { kind: "continue" };
  if (!input.alive || input.aborted) return { kind: "dead" };
  if (input.retryStatus === undefined) return { kind: "retry" };
  if (input.retryStatus === 409) return { kind: "already-in-progress" };
  return { kind: "continue" };
}

export type CodexAuthPollDecision =
  | { kind: "abort" }
  | { kind: "missing"; retrying: boolean; nextStreak: number }
  | { kind: "in-progress"; waitingForCode: boolean; nextStreak: number }
  | { kind: "done"; payload: { catalogRefreshPending?: unknown } }
  | { kind: "failed"; error?: string };

export function decodeCodexAuthPollTick(input: {
  alive: boolean;
  aborted: boolean;
  status: { status: string; error?: string; catalogRefreshPending?: unknown } | null | undefined;
  errorStreak: number;
  manualCodeWaiting: boolean;
}): CodexAuthPollDecision {
  if (!input.alive || input.aborted) return { kind: "abort" };
  if (!input.status) {
    const nextStreak = input.errorStreak + 1;
    return { kind: "missing", retrying: nextStreak >= 3, nextStreak };
  }
  if (input.status.status === "done") {
    return { kind: "done", payload: input.status };
  }
  if (input.status.status === "error" || input.status.status === "expired") {
    return { kind: "failed", error: input.status.error };
  }
  return {
    kind: "in-progress",
    waitingForCode: input.manualCodeWaiting,
    nextStreak: 0,
  };
}

export function decodeCodexAuthPollCatch(input: {
  alive: boolean;
  aborted: boolean;
  isAbortError: boolean;
  errorStreak: number;
}): Extract<CodexAuthPollDecision, { kind: "abort" | "missing" }> {
  if (!input.alive || input.aborted || input.isAbortError) return { kind: "abort" };
  const nextStreak = input.errorStreak + 1;
  return { kind: "missing", retrying: nextStreak >= 3, nextStreak };
}

export function codexAuthManualCodeBlocked(
  flowId: string | null,
  input: string,
  busy: boolean,
  waiting: boolean,
): boolean {
  return !flowId || !input || busy || waiting;
}

export function decodeCodexAuthManualCodeFailure(
  data: { error?: string } | undefined,
  statusText: string,
): string {
  return data?.error ?? statusText;
}

export function decodeCodexAuthTimeoutApplies(pollStillRunning: boolean): boolean {
  return Boolean(pollStillRunning);
}

export function decodeCodexAuthStartCatch(
  alive: boolean,
  isAbortError: boolean,
): boolean {
  return alive && !isAbortError;
}
