/**
 * Provider visual identity.
 *
 * One row per provider id: the mark it renders, the label it shows, and the dark-mode plate
 * treatment for boxed marks. Three parallel tables used to carry those facts separately, so
 * an id could gain a mark without gaining a label — or gain a knockout with no entry to hang
 * it on — and nothing in the module would notice. Here the id appears once and every reader
 * derives from it.
 *
 * Provider ids are this project's external vocabulary: they mirror upstream provider names
 * and the listener's config keys, so they are written as they are, not renamed for style.
 */
import type { TFn, TKey } from "./i18n/shared";

/** Dark-mode plate knockout. Light-mode CSS restores boxed plates for white glyphs. */
export type ProviderIconKnockout = {
  lightPlate?: string;
  lightPlateRadius?: string;
  invertDark?: boolean;
};

/** One provider's visual identity. */
interface ProviderIdentity {
  /** File name under `public/provider-icons/`. */
  readonly mark: string;
  /** Literal brand label, for names the dashboard does not translate. */
  readonly label?: string;
  /** Catalogue key, for names the dashboard does translate. */
  readonly labelKey?: TKey;
  /** Dark-mode treatment for a boxed mark; absent means the mark needs none. */
  readonly knockout?: ProviderIconKnockout;
}

/** Provider id → identity. */
const PROVIDER_IDENTITIES: Record<string, ProviderIdentity> = {
  anthropic: { mark: "claude-color.svg", label: "Anthropic Claude" },
  "anthropic-apikey": { mark: "claude-color.svg", label: "Anthropic Claude" },
  "azure-openai": { mark: "openai.svg", label: "Azure OpenAI" },
  chatgpt: { mark: "openai.svg", label: "ChatGPT" },
  "cloudflare-ai-gateway": { mark: "cloudflare-ai-gateway-color.svg", label: "Cloudflare AI Gateway" },
  "cloudflare-workers-ai": { mark: "cloudflare-ai-gateway-color.svg", label: "Cloudflare Workers AI" },
  cline: { mark: "cline-color.svg", label: "Cline" },
  "cline-pass": { mark: "cline-color.svg", label: "ClinePass" },
  "command-code": { mark: "commandcode-color.svg", labelKey: "provider.name.commandCodeAuth" },
  commandcode: { mark: "commandcode-color.svg", labelKey: "provider.name.commandCodeApi" },
  cursor: { mark: "cursor-color.svg", label: "Cursor", knockout: { lightPlate: "#14120B", lightPlateRadius: "22%" } },
  deepseek: { mark: "deepseek-color.svg", label: "DeepSeek" },
  firepass: { mark: "firepass-color.svg" },
  fireworks: { mark: "fireworks-color.svg" },
  github: { mark: "github-copilot-color.svg", label: "GitHub" },
  "github-copilot": { mark: "copilot-color.svg", label: "GitHub Copilot" },
  "gitlab-duo": { mark: "gitlab-duo-color.svg", label: "GitLab Duo" },
  google: { mark: "gemini-color.svg", label: "Google" },
  "google-antigravity": { mark: "antigravity-color.svg" },
  "google-vertex": { mark: "gemini-color.svg", label: "Google Vertex" },
  groq: { mark: "groq-color.svg", label: "Groq" },
  huggingface: { mark: "huggingface-color.svg", label: "Hugging Face" },
  kimi: { mark: "kimi-color.svg", label: "Kimi" },
  "kimi-code": { mark: "kimi-color.svg", label: "Kimi" },
  kiro: { mark: "kiro-color.svg" },
  "lm-studio": { mark: "lm-studio-color.svg", label: "LM Studio" },
  mistral: { mark: "mistral-color.svg", label: "Mistral" },
  moonshot: { mark: "moonshot-color.svg", label: "Moonshot" },
  nvidia: { mark: "nvidia-color.svg", label: "NVIDIA NIM" },
  ollama: { mark: "ollama-color.svg", label: "Ollama" },
  "ollama-cloud": { mark: "ollama-color.svg", label: "Ollama Cloud" },
  openai: { mark: "openai.svg", label: "OpenAI (Codex login)" },
  "openai-apikey": { mark: "openai.svg", label: "OpenAI API" },
  "opencode-free": { mark: "opencode.svg", label: "OpenCode Free" },
  "opencode-go": { mark: "opencode.svg", label: "OpenCode Go" },
  "opencode-zen": { mark: "opencode.svg", label: "OpenCode Zen" },
  openrouter: { mark: "openrouter-color.svg", label: "OpenRouter" },
  qianfan: { mark: "qianfan-color.svg" },
  alibaba: { mark: "alibaba-color.svg", label: "Alibaba Coding Plan" },
  "alibaba-token-plan": { mark: "alibaba-color.svg", label: "Alibaba Token Plan" },
  "alibaba-token-plan-intl": { mark: "alibaba-color.svg", label: "Alibaba Token Plan (Intl)" },
  "qwen-cloud": { mark: "qwen-portal-color.svg", label: "Qwen Cloud" },
  "vercel-ai-gateway": { mark: "vercel-ai-gateway-color.svg", label: "Vercel AI Gateway" },
  vllm: { mark: "vllm-color.svg", label: "vLLM" },
  xai: { mark: "grok.svg", label: "xAI Grok" },
  "mimo-free": { mark: "xiaomi-color.svg", label: "MiMo Free" },
  xiaomi: { mark: "xiaomi-color.svg", label: "Xiaomi" },
  "xiaomi-mimo": { mark: "xiaomi-color.svg" },
  mimo: { mark: "xiaomi-color.svg" },
  nous: { mark: "nous-color.svg" },
  umans: { mark: "umans-color.svg" },
  neuralwatt: { mark: "neuralwatt-color.svg" },
  orcarouter: { mark: "orcarouter-color.svg", knockout: {} },
  bizrouter: { mark: "bizrouter-color.png", knockout: { lightPlate: "#000000" } },
  cerebras: { mark: "cerebras-color.svg" },
  chutes: { mark: "chutes-color.png", knockout: { lightPlate: "#121212" } },
  deepinfra: { mark: "deepinfra-color.svg" },
  hyperbolic: { mark: "hyperbolic-color.svg" },
  nscale: { mark: "nscale-color.png", knockout: { lightPlate: "#0F41F3", lightPlateRadius: "22%" } },
  vultr: { mark: "vultr-color.svg" },
  baseten: { mark: "baseten-color.svg" },
  sambanova: { mark: "sambanova-color.svg" },
  nebius: { mark: "nebius-color.svg" },
  digitalocean: { mark: "digitalocean-color.svg" },
  scaleway: { mark: "scaleway-color.svg" },
  featherless: { mark: "featherless-color.svg" },
  novita: { mark: "novita-color.svg" },
  together: { mark: "together-color.svg" },
  venice: { mark: "venice-color.svg" },
  zai: { mark: "zai-color.svg" },
  "zhipu-bigmodel": { mark: "zhipu-color.svg" },
  "zhipu-bigmodel-coding": { mark: "zhipu-color.svg" },
  nanogpt: { mark: "nano-gpt-color.svg" },
  synthetic: { mark: "synthetic-color.png", knockout: { invertDark: true } },
  siliconflow: { mark: "siliconflow-color.svg", label: "SiliconFlow" },
  "tencent-coding-plan": { mark: "tencentcloud-color.svg", label: "Tencent Cloud Coding Plan" },
  volcengine: { mark: "volcengine-color.svg", labelKey: "provider.name.volcengine" },
  "volcengine-coding-plan": { mark: "volcengine-color.svg", labelKey: "provider.name.volcengineCodingPlan" },
  "volcengine-agent-plan": { mark: "volcengine-color.svg", labelKey: "provider.name.volcengineAgentPlan" },
  parallel: { mark: "parallel-color.png", knockout: { invertDark: true } },
  zenmux: { mark: "zenmux-color.svg" },
  litellm: { mark: "litellm-color.png", label: "LiteLLM" },
  minimax: { mark: "minimax-color.svg" },
  "minimax-cn": { mark: "minimax-color.svg" },
  kilo: { mark: "kilo-color.svg" },
};

/** Optional hints kept for call-site compatibility; resolution is name-based for now. */
type ProviderIconHints = {
  adapter?: string;
  baseUrl?: string;
};

/** The id's registry row, or undefined when the registry has no row for it. */
function identityFor(provider: string): ProviderIdentity | undefined {
  const key = provider.toLowerCase();
  return Object.hasOwn(PROVIDER_IDENTITIES, key) ? PROVIDER_IDENTITIES[key] : undefined;
}

/** The mark for a provider id, or undefined when the registry has no row for it. */
export function providerIconSrc(provider: string, _hints?: ProviderIconHints): string | undefined {
  void _hints;
  const identity = identityFor(provider);
  return identity === undefined ? undefined : `/provider-icons/${identity.mark}`;
}

/** Dark-mode plate knockout for a provider's mark, when that mark is a boxed plate. */
export function providerIconKnockout(provider: string): ProviderIconKnockout | undefined {
  return identityFor(provider)?.knockout;
}

/** Display label with proper brand casing when known; otherwise the original name. */
export function formatProviderDisplayName(provider: string, t: TFn): string {
  const identity = identityFor(provider);
  if (identity?.labelKey !== undefined) return t(identity.labelKey);
  if (identity?.label) return identity.label;
  // Title-case simple ids like "my-provider" without mangling mixedCase custom names.
  const key = provider.toLowerCase();
  if (provider === key && /^[a-z0-9]+(?:-[a-z0-9]+)*$/.test(provider)) {
    return provider
      .split("-")
      .map(part => (part ? part[0]!.toUpperCase() + part.slice(1) : part))
      .join(" ");
  }
  return provider;
}

/**
 * True for known registry/preset ids (hide ID/adapter/URL behind Advanced by default).
 *
 * A row only counts as catalog when it carries a label. Marks and plate treatments are
 * visual details a custom provider may borrow, so they must not promote an unknown id into
 * the catalog.
 */
export function isCatalogProviderId(provider: string): boolean {
  const identity = identityFor(provider);
  return identity !== undefined && (identity.label !== undefined || identity.labelKey !== undefined);
}

/**
 * The two Command Code config ids differ by a single dash, so their slugs are stated
 * explicitly rather than derived; every other provider keeps its own id as its slug.
 */
const PROVIDER_SLUG_OVERRIDES: Record<string, string> = {
  "command-code": "commandcode-auth",
  commandcode: "commandcode-api",
};

function isCommandCodeId(provider: string): boolean {
  return Object.hasOwn(PROVIDER_SLUG_OVERRIDES, provider);
}

/** Distinguishable lowercase-dash slug for a provider id (command-code -> commandcode-auth). */
export function providerDisplaySlug(provider: string): string {
  return isCommandCodeId(provider) ? PROVIDER_SLUG_OVERRIDES[provider]! : provider;
}

/**
 * Drop the duplicated `<vendor>-` prefix the Command Code catalog encodes
 * (`deepseek-deepseek-v4-flash` -> `deepseek-v4-flash`). Ids that do not match are returned
 * unchanged.
 */
function collapseRepeatedVendorPrefix(model: string): string {
  const vendorAndRest = model.match(/^([a-z0-9]+)-([a-z0-9]+(?:-[a-z0-9]+)+)$/i);
  if (vendorAndRest === null) return model;
  const vendor = vendorAndRest[1]!;
  return model.startsWith(`${vendor}-${vendor}-`) ? model.slice(vendor.length + 1) : model;
}

/**
 * Rewrite a `provider/model` route to a clearly distinguishable slug. Command Code's two
 * config ids differ by a single dash (`command-code` vs `commandcode`), so relabel them to
 * `commandcode-auth/...` and `commandcode-api/...` — the same lowercase-dash style the
 * opencode presets use (`opencode-free/mimo-v2.5`, `opencode-go/hy3`). Every other provider
 * keeps the raw route exactly as before.
 */
export function formatNamespacedModelId(namespaced: string, _t: TFn): string {
  const slash = namespaced.indexOf("/");
  if (slash <= 0) return namespaced;
  const provider = namespaced.slice(0, slash);
  if (!isCommandCodeId(provider)) return namespaced;
  const model = collapseRepeatedVendorPrefix(namespaced.slice(slash + 1));
  return `${providerDisplaySlug(provider)}/${model}`;
}
