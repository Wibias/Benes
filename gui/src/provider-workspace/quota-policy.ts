export type QuotaWindowKey = "fiveHour" | "weekly" | "monthly";

export type QuotaAccess =
  | { kind: "none" }
  | { kind: "windows"; windows: readonly QuotaWindowKey[] }
  | { kind: "custom" };

export interface QuotaPolicyItem {
  name: string;
  baseUrl?: string;
  authMode?: string;
}

const FIVE_WEEK_MONTH = ["fiveHour", "weekly", "monthly"] as const;
const FIVE_WEEK = ["fiveHour", "weekly"] as const;
const FIVE = ["fiveHour"] as const;
const MONTH = ["monthly"] as const;

const CUSTOM: QuotaAccess = { kind: "custom" };
const FIVE_WEEK_MONTH_ACCESS: QuotaAccess = { kind: "windows", windows: FIVE_WEEK_MONTH };
const FIVE_WEEK_ACCESS: QuotaAccess = { kind: "windows", windows: FIVE_WEEK };
const FIVE_ACCESS: QuotaAccess = { kind: "windows", windows: FIVE };
const MONTH_ACCESS: QuotaAccess = { kind: "windows", windows: MONTH };

const CODEX_FORWARD = "https://chatgpt.com/backend-api/codex";

const BY_NAME: Readonly<Record<string, QuotaAccess>> = {
  openai: FIVE_WEEK_MONTH_ACCESS,
  "command-code": FIVE_WEEK_ACCESS,
  commandcode: FIVE_WEEK_ACCESS,
  cursor: CUSTOM,
  "google-antigravity": CUSTOM,
  kimi: FIVE_WEEK_ACCESS,
  xai: MONTH_ACCESS,
  anthropic: FIVE_WEEK_ACCESS,
  openrouter: CUSTOM,
  deepseek: CUSTOM,
  moonshot: CUSTOM,
  venice: CUSTOM,
  "cline-pass": FIVE_WEEK_MONTH_ACCESS,
  cline: FIVE_WEEK_MONTH_ACCESS,
  minimax: CUSTOM,
  "minimax-cn": CUSTOM,
  deepinfra: CUSTOM,
  neuralwatt: FIVE_ACCESS,
  synthetic: FIVE_WEEK_ACCESS,
  "opencode-go": FIVE_WEEK_MONTH_ACCESS,
  zai: FIVE_WEEK_MONTH_ACCESS,
};

type BasePreset = "custom" | "five" | "five-week" | "five-week-month";

const BASE_PRESET: Readonly<Record<string, BasePreset>> = {
  "https://api.kimi.com/coding/v1": "five-week",
  "https://openrouter.ai/api/v1": "custom",
  "https://api.deepseek.com": "custom",
  "https://api.deepseek.com/v1": "custom",
  "https://api.moonshot.ai/v1": "custom",
  "https://api.moonshot.cn/v1": "custom",
  "https://api.venice.ai/api/v1": "custom",
  "https://api.cline.bot": "five-week-month",
  "https://api.cline.bot/api/v1": "five-week-month",
  "https://api.minimax.io/v1": "custom",
  "https://api.minimaxi.com/v1": "custom",
  "https://api.deepinfra.com": "custom",
  "https://api.deepinfra.com/v1/openai": "custom",
  "https://api.neuralwatt.com/v1": "five",
  "https://api.synthetic.new/v2": "five-week",
  "https://api.synthetic.new/openai/v1": "five-week",
  "https://opencode.ai/zen/go/v1": "five-week-month",
  "https://api.a6api.com": "custom",
  "https://api.a6api.com/v1": "custom",
  "https://api.z.ai": "five-week-month",
  "https://api.z.ai/api/coding/paas/v4": "five-week-month",
};

function normalizeBase(raw: string | undefined): string {
  return (raw ?? "").trim().replace(/\/+$/, "");
}

function baseAccess(preset: BasePreset | undefined): QuotaAccess {
  switch (preset) {
    case "custom":
      return { kind: "custom" };
    case "five":
      return { kind: "windows", windows: FIVE };
    case "five-week":
      return { kind: "windows", windows: FIVE_WEEK };
    case "five-week-month":
      return { kind: "windows", windows: FIVE_WEEK_MONTH };
    default:
      return { kind: "none" };
  }
}

export function quotaAccessPolicy(item: QuotaPolicyItem): QuotaAccess {
  const named = BY_NAME[item.name];
  if (named) return named;

  const mode = (item.authMode ?? "").trim();
  const base = normalizeBase(item.baseUrl);
  if (mode === "forward" || base === CODEX_FORWARD) {
    return { kind: "windows", windows: FIVE_WEEK_MONTH };
  }
  if (mode === "oauth") return { kind: "none" };
  if (mode !== "" && mode !== "key") return { kind: "none" };
  return baseAccess(BASE_PRESET[base]);
}
