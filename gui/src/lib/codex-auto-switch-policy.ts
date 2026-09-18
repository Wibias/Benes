/** Copy, blur-commit, and key-command policy for Codex auto-switch. */
import type { AccountPoolStrategy } from "../account-pool-strategy";
import type { TKey } from "../i18n/shared";

const AUTO_SWITCH_DESCRIPTION_KEYS = {
  quota: {
    on: "codexAuth.autoSwitchQuotaDesc",
    off: "codexAuth.autoSwitchQuotaOffDesc",
  },
  "round-robin": {
    on: "codexAuth.autoSwitchRoundRobinDesc",
    off: "codexAuth.autoSwitchRoundRobinDesc",
  },
  "fill-first": {
    on: "codexAuth.autoSwitchFillFirstDesc",
    off: "codexAuth.autoSwitchFillFirstOffDesc",
  },
  "reset-window": {
    on: "codexAuth.autoSwitchResetWindowDesc",
    off: "codexAuth.autoSwitchResetWindowOffDesc",
  },
} as const satisfies Record<AccountPoolStrategy, { on: TKey; off: TKey }>;

export function autoSwitchDescriptionKey(
  strategy: AccountPoolStrategy,
  enabled: boolean,
): TKey {
  return AUTO_SWITCH_DESCRIPTION_KEYS[strategy][enabled ? "on" : "off"];
}

export type AutoSwitchFeedbackView =
  | { kind: "saving" }
  | { kind: "empty" }
  | { kind: "message"; tone: "ok" | "err"; message: string };

/** The lane's newest feedback, as the card shows it. */
export type AutoSwitchFeedback = { tone: "ok" | "err"; message: string } | null;

/**
 * The slice of the auto-switch controller the card renders and drives.
 *
 * The hook publishes more than this — its observer callbacks belong to whoever owns the
 * `/active` subscription — so the card states the part it actually reads and calls.
 */
export interface AutoSwitchCardController {
  threshold: number;
  draft: string;
  hydrated: boolean;
  saving: boolean;
  loadError: boolean;
  feedback: AutoSwitchFeedback;
  setDraft(value: string): void;
  setEditing(editing: boolean): void;
  commit(): Promise<boolean>;
  cancel(): void;
  toggle(): Promise<boolean>;
  retry(): void;
}

export function autoSwitchFeedbackView(
  saving: boolean,
  feedback: { tone: "ok" | "err"; message: string } | null,
): AutoSwitchFeedbackView {
  if (saving) return { kind: "saving" };
  if (!feedback?.message) return { kind: "empty" };
  return { kind: "message", tone: feedback.tone, message: feedback.message };
}

export type AutoSwitchBlurCommit = "ignore" | "clear-pointer" | "commit" | "idle";

export function autoSwitchBlurCommit(input: {
  relatedTargetInside: boolean;
  pointerIntent: boolean;
  enabled: boolean;
  controlsDisabled: boolean;
}): AutoSwitchBlurCommit {
  if (input.relatedTargetInside) return "ignore";
  if (input.pointerIntent) return "clear-pointer";
  if (input.enabled && !input.controlsDisabled) return "commit";
  return "idle";
}

export type AutoSwitchKeyCommand = "ignore" | "commit" | "cancel" | "none";

export function autoSwitchKeyCommand(input: {
  composing: boolean;
  controlsDisabled: boolean;
  key: string;
}): AutoSwitchKeyCommand {
  if (input.composing || input.controlsDisabled) return "ignore";
  if (input.key === "Enter") return "commit";
  if (input.key === "Escape") return "cancel";
  return "none";
}

export function autoSwitchDescribedBy(feedback: AutoSwitchFeedbackView): string {
  return feedback.kind === "empty"
    ? "codex-auto-switch-desc"
    : "codex-auto-switch-desc codex-auto-switch-feedback";
}
