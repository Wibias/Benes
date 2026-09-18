import {
  DEFAULT_ACCOUNT_POOL_STICKY_LIMIT,
  DEFAULT_ACCOUNT_POOL_STRATEGY,
  normalizeAccountPoolStickyLimit,
  normalizeAccountPoolStrategy,
  type AccountPoolStrategy,
} from "../account-pool-strategy.ts";

export type AnthropicPoolSnapshot = {
  enabled: boolean;
  threshold: number;
  strategy: AccountPoolStrategy;
  stickyLimit: number;
};

export type AnthropicThresholdDraft =
  | { kind: "invalid" }
  | { kind: "unchanged"; value: number }
  | { kind: "next"; value: number };

function recordFromUnknown(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

export function anthropicPoolSnapshotFromPayload(json: unknown): AnthropicPoolSnapshot | null {
  if (json == null) return null;
  const row = typeof json === "object" ? json as Record<string, unknown> : {};
  return {
    enabled: row.enabled === true,
    threshold: typeof row.autoSwitchThreshold === "number" ? row.autoSwitchThreshold : 80,
    strategy: normalizeAccountPoolStrategy(row.strategy),
    stickyLimit: normalizeAccountPoolStickyLimit(row.stickyLimit),
  };
}

export function anthropicPoolSavedSnapshot(
  next: AnthropicPoolSnapshot,
  json: unknown,
): AnthropicPoolSnapshot {
  const row = recordFromUnknown(json);
  return {
    enabled: next.enabled,
    threshold: next.threshold,
    strategy: normalizeAccountPoolStrategy(row?.strategy ?? next.strategy),
    stickyLimit: normalizeAccountPoolStickyLimit(row?.stickyLimit ?? next.stickyLimit),
  };
}

export function anthropicPoolToggleDisabled(input: {
  loading: boolean;
  saving: boolean;
  loadError: boolean;
  enabled: boolean;
  accountCount: number;
}): boolean {
  return input.loading || input.saving || input.loadError || (!input.enabled && input.accountCount < 2);
}

export function parseAnthropicThresholdDraft(draft: string, current: number): AnthropicThresholdDraft {
  const parsed = Number(draft);
  if (!Number.isInteger(parsed) || parsed < 0 || parsed > 100) return { kind: "invalid" };
  if (parsed === current) return { kind: "unchanged", value: parsed };
  return { kind: "next", value: parsed };
}

export function anthropicPoolView(state: AnthropicPoolSnapshot | null): AnthropicPoolSnapshot {
  return {
    enabled: state?.enabled === true,
    threshold: state?.threshold ?? 80,
    strategy: state?.strategy ?? DEFAULT_ACCOUNT_POOL_STRATEGY,
    stickyLimit: state?.stickyLimit ?? DEFAULT_ACCOUNT_POOL_STICKY_LIMIT,
  };
}
