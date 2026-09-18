import type { TKey } from "../../i18n/shared";
import type { HarnessRecord, HarnessToken } from "./types";

const TOKEN_KEY: Record<HarnessToken, TKey> = {
  valid: "harnesses.auth.valid",
  expired: "harnesses.auth.expired",
  missing: "harnesses.auth.missing",
  none: "harnesses.auth.unavailable",
};

export function listBadge(harness: HarnessRecord): TKey {
  if (!harness.installed) return "harnesses.badge.notInstalled";
  if (harness.issue === "conflict") return "harnesses.badge.conflict";
  if (harness.applied) return "harnesses.badge.applied";
  return "harnesses.badge.notApplied";
}

export function badgeTone(harness: HarnessRecord): "applied" | "conflict" | "update" | "muted" {
  if (!harness.installed) return "muted";
  if (harness.issue === "conflict") return "conflict";
  if (harness.issue === "update-needed") return "update";
  if (harness.applied) return "applied";
  return "muted";
}

export function historyTone(kind: HarnessRecord["history"][number]["kind"]): "applied" | "muted" {
  return kind === "disable" ? "muted" : "applied";
}

export function authTokenKey(token: HarnessToken): TKey {
  return TOKEN_KEY[token];
}

export type HarnessHeaderAction =
  | { kind: "apply"; enabled: boolean }
  | { kind: "disable"; enabled: boolean }
  | { kind: "none" };

/**
 * Header Apply is the connect/inject action for a harness that is installed and
 * not yet applied. Settings `autoApply` does not itself inject, so it must not
 * invent a dirty Apply state. Re-apply lives in the actions list, not here.
 */
export function harnessHeaderAction(
  harness: Pick<HarnessRecord, "installed" | "applied" | "issue">,
): HarnessHeaderAction {
  if (harness.applied) {
    return { kind: "disable", enabled: harness.issue !== "conflict" };
  }
  if (!harness.installed || harness.issue === "conflict") {
    return { kind: "none" };
  }
  return { kind: "apply", enabled: true };
}

export function formatStamp(value: string | null, empty: string): string {
  if (!value) return empty;
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return empty;
  return date.toLocaleString();
}

export function fileName(path: string): string {
  const parts = path.split(/[/\\]/);
  return parts[parts.length - 1] || path;
}
