/** Benes dashboard client for the Go proxy (`internal/server`). */
/** Cleanup-policy run-now orchestration; callers apply returned policy snapshots. */

import {
  interpretRunStartResponse,
  jobMatchesStart,
  policyFieldsFromResponse,
  type CleanupJobOutcome,
  type CleanupPolicy,
} from "./storage-cleanup-policy.ts";

export type CleanupRunFetch = (input: string, init?: RequestInit) => Promise<Response>;

export type CleanupRunResult =
  | { kind: "aborted" }
  | { kind: "save_failed" }
  | { kind: "already_running"; policy?: CleanupPolicy }
  | { kind: "run_failed"; policy?: CleanupPolicy }
  | { kind: "outcome"; policy?: CleanupPolicy; outcome: CleanupJobOutcome };

async function readJson(response: Response): Promise<Record<string, unknown>> {
  return await response.json().catch(() => ({})) as Record<string, unknown>;
}

export async function pollCleanupJob(opts: {
  apiBase: string;
  startedAt: number;
  deadline: number;
  signal: AbortSignal;
  sleep: (ms: number) => Promise<void>;
  now?: () => number;
  fetchImpl?: CleanupRunFetch;
  onPolicy?: (policy: CleanupPolicy) => void;
}): Promise<{ aborted: boolean; finalPolicy?: CleanupPolicy; outcome?: CleanupJobOutcome }> {
  const now = opts.now ?? Date.now;
  const fetchImpl = opts.fetchImpl ?? fetch;
  const deadline = opts.deadline;
  const signal = opts.signal;
  const sleep = opts.sleep;
  const apiBase = opts.apiBase;
  const startedAt = opts.startedAt;
  const onPolicy = opts.onPolicy;
  let finalPolicy: CleanupPolicy | undefined;
  let outcome: CleanupJobOutcome | undefined;

  while (now() < deadline) {
    if (signal.aborted) return { aborted: true, finalPolicy, outcome };
    await sleep(250);
    if (signal.aborted) return { aborted: true, finalPolicy, outcome };
    const pollRes = await fetchImpl(`${apiBase}/api/storage/cleanup-policy`, { signal });
    if (signal.aborted) return { aborted: true, finalPolicy, outcome };
    if (!pollRes.ok) continue;
    const body = await pollRes.json() as CleanupPolicy;
    if (signal.aborted) return { aborted: true, finalPolicy, outcome };
    finalPolicy = policyFieldsFromResponse(body);
    onPolicy?.(body);
    outcome = jobMatchesStart(body.job, startedAt);
    if (outcome) break;
  }

  return { aborted: signal.aborted, finalPolicy, outcome };
}

async function putCleanupPolicy(
  apiBase: string,
  body: CleanupPolicy,
  signal: AbortSignal,
  fetchImpl: CleanupRunFetch,
): Promise<{ ok: boolean; policy?: CleanupPolicy }> {
  const saveRes = await fetchImpl(`${apiBase}/api/storage/cleanup-policy`, {
    method: "PUT",
    headers: { "content-type": "application/json" },
    body: JSON.stringify(body),
    signal,
  });
  if (!saveRes.ok) return { ok: false };
  const saved = await saveRes.json() as { policy?: CleanupPolicy };
  return saved.policy ? { ok: true, policy: saved.policy } : { ok: false };
}

export async function executeCleanupRun(opts: {
  apiBase: string;
  body: CleanupPolicy;
  signal: AbortSignal;
  sleep: (ms: number) => Promise<void>;
  now?: () => number;
  fetchImpl?: CleanupRunFetch;
  onPolicy?: (policy: CleanupPolicy) => void;
}): Promise<CleanupRunResult> {
  const fetchImpl = opts.fetchImpl ?? fetch;
  const saved = await putCleanupPolicy(opts.apiBase, opts.body, opts.signal, fetchImpl);
  if (opts.signal.aborted) return { kind: "aborted" };
  if (!saved.ok || !saved.policy) return { kind: "save_failed" };
  opts.onPolicy?.(saved.policy);

  const res = await fetchImpl(`${opts.apiBase}/api/storage/cleanup-policy/run`, {
    method: "POST",
    signal: opts.signal,
  });
  if (opts.signal.aborted) return { kind: "aborted" };
  const json = await readJson(res);
  const start = interpretRunStartResponse(res.status, json);
  if (start.policy) opts.onPolicy?.(start.policy);
  if (start.kind !== "started") return start;

  const polled = await pollCleanupJob({
    apiBase: opts.apiBase,
    startedAt: start.startedAt,
    deadline: (opts.now ?? Date.now)() + 120_000,
    signal: opts.signal,
    sleep: opts.sleep,
    now: opts.now,
    fetchImpl,
    onPolicy: opts.onPolicy,
  });
  if (polled.aborted) return { kind: "aborted" };
  if (polled.finalPolicy) opts.onPolicy?.(polled.finalPolicy);
  if (!polled.outcome) return { kind: "run_failed", policy: polled.finalPolicy };
  return { kind: "outcome", policy: polled.finalPolicy, outcome: polled.outcome };
}
