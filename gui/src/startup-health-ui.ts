/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { TKey } from "./i18n/shared";

/*
 * Startup-health policy for the dashboard chip and the Startup page.
 *
 * The vocabulary lives here as data: the statuses a chip can show, the routing kinds and
 * shim coverages a risk detail carries, and the guards and rules that read them. The
 * exported functions are thin adapters over that single policy, so no caller restates a
 * status list or a seed rule of its own.
 */

/** Statuses the dashboard chip renders straight from a payload. */
const CHIP_STATUSES = ["native", "protected", "at-risk"] as const;

/** The one status a chip never takes from the server: a hard read failure on our side. */
const READ_FAILURE_STATUS = "error";

export type StartupHealthStatus = (typeof CHIP_STATUSES)[number] | typeof READ_FAILURE_STATUS;

/** How the runtime reaches upstream, as `/api/startup-health` reports it. */
const ROUTING_KINDS = ["native", "benes-local", "custom-local", "custom-remote", "unknown"] as const;

/** How completely the Windows shim covers the installed clients. */
const SHIM_COVERAGES = ["full", "cli-only", "none"] as const;

export type StartupRiskDetail = {
  routingKind: (typeof ROUTING_KINDS)[number];
  shimCoverage: (typeof SHIM_COVERAGES)[number];
};

/**
 * Why an install is at risk, most specific first. The first rule that matches wins and the
 * fallback below covers every remaining at-risk install, so adding an explanation means
 * adding one entry rather than another branch.
 */
const RISK_DETAIL_RULES: readonly { applies: (risk: StartupRiskDetail) => boolean; key: TKey }[] = [
  { applies: (risk) => risk.routingKind === "custom-local", key: "startup.riskDetailCustomLocal" },
  { applies: (risk) => risk.shimCoverage === "cli-only", key: "startup.riskDetailWindowsShim" },
];

const FALLBACK_RISK_DETAIL_KEY: TKey = "startup.riskDetail";

/** The most specific explanation the detail panel can offer for an at-risk install. */
export function startupRiskDetailKey(risk: StartupRiskDetail): TKey {
  const rule = RISK_DETAIL_RULES.find((candidate) => candidate.applies(risk));
  return rule ? rule.key : FALLBACK_RISK_DETAIL_KEY;
}

/** One request or mutation counter a poll bumps and snapshots. */
export type EpochRef = { current: number };

/** The epochs one poll captures before its fetches go out. */
export type SettingsPollEpoch = { request: number; mutation: number };

/** The same pair plus the write-in-flight flag a poll must also respect. */
export type SettingsPollWindow = SettingsPollEpoch & { mutationInFlight: boolean };

/**
 * Everything that voids a poll's answer, in one place so the settings poll and the
 * shadow-call poll apply the same rule: a write in flight owns the UI, and a newer request
 * or a completed mutation makes this answer stale.
 */
const POLL_AUTHORITY_GUARDS: readonly ((captured: SettingsPollEpoch, now: SettingsPollWindow) => boolean)[] = [
  (_captured, now) => !now.mutationInFlight,
  (captured, now) => captured.request === now.request,
  (captured, now) => captured.mutation === now.mutation,
];

export function settingsPollMayCommit(captured: SettingsPollEpoch, now: SettingsPollWindow): boolean {
  return POLL_AUTHORITY_GUARDS.every((guard) => guard(captured, now));
}

/** Snapshot + bump one request epoch before issuing a poll fetch. */
export function beginPollEpoch(requestRef: EpochRef, mutationRef: EpochRef): SettingsPollEpoch {
  requestRef.current += 1;
  return { request: requestRef.current, mutation: mutationRef.current };
}

/**
 * Probe result for the dashboard chip: the status plus whether the server answered from its
 * stale-while-revalidate fallback.
 *
 * The status alone is not enough. `/api/startup-health` answers immediately from a 30s cache
 * and kicks the real probe off in the background, so the first response after a cold start
 * (or after the TTL expires) is a conservative placeholder. A consumer that only reads the
 * status has no way to know it should look again soon, and on the dashboard that meant the
 * chip sat on the placeholder until the next 30s poll — which is why clicking "refresh
 * quota" (any action that re-mounted the chip) appeared to be what fixed it.
 */
export type StartupHealthProbe = { status: StartupHealthStatus; stale: boolean };

/** How soon to re-ask after a stale answer; matches the Startup page's follow-up delay. */
export const STARTUP_HEALTH_STALE_RETRY_MS = 2_000;

/** A probe record that may not have arrived yet. */
export type SettledStartupHealthProbe = StartupHealthProbe | undefined | null;

/** The raw `/api/startup-health` payload, in both the current and the pre-chip shape. */
export type StartupHealthPayload = {
  status?: unknown;
  diagnosticStale?: unknown;
  ok?: unknown;
  proxy?: unknown;
};

/** True for a status the chip takes from the server unchanged. */
function isChipStatus(value: unknown): value is StartupHealthStatus {
  return typeof value === "string" && (CHIP_STATUSES as readonly string[]).includes(value);
}

/** The pre-chip payload's answer: a running loopback proxy is protected, anything else is not. */
function legacyProxyStatus(proxy: unknown): StartupHealthStatus {
  return proxy === "running" ? "protected" : "at-risk";
}

/**
 * Payload readers in priority order: the current `status` field first, then the pre-chip
 * `{ ok, proxy }` shape older listeners still answer. The first reader that recognizes the
 * payload owns the result, and a payload neither recognizes reads as unknown.
 */
const PROBE_READERS: readonly ((payload: StartupHealthPayload) => StartupHealthStatus | null)[] = [
  (payload) => (isChipStatus(payload.status) ? payload.status : null),
  (payload) => (payload.ok === true ? legacyProxyStatus(payload.proxy) : null),
];

/**
 * Map a startup-health API payload to the dashboard chip status.
 *
 * `diagnosticStale` means the server is still refreshing (SWR fallback / expired TTL). That
 * is NOT a hard read failure — collapsing it to "error" shows the misleading "Could not read
 * startup protection" chip whenever the 30s cache misses. Keep the payload status and
 * reserve "error" for a failed read on our side.
 */
export function mapStartupHealthProbe(payload: StartupHealthPayload): StartupHealthStatus | null {
  for (const read of PROBE_READERS) {
    const status = read(payload);
    if (status !== null) return status;
  }
  return null;
}

/**
 * Which probe results resolve on their own: an in-progress server refresh lands within
 * seconds and is worth a short timer, while a hard read failure waits for the normal poll.
 */
export function probeNeedsFastRetry(latest: SettledStartupHealthProbe): boolean {
  if (!latest || !latest.stale) return false;
  return isChipStatus(latest.status);
}

/** Single owner cadence for project-config diagnostics (ms). */
export const PROJECT_CONFIG_DIAGNOSTICS_POLL_MS = 30_000;
