/**
 * Named classifiers for hardcoded literals that are machine text, not UI copy.
 * Brand names and model ids live in isBrandOrModelLiteral; this table is the
 * technical-token policy the i18n plugin consults first.
 */

const BRAND_LITERALS = new Set([
  "OpenAI",
  "Anthropic",
  "GitHub",
  "Codex",
  "OpenRouter",
  "Ollama",
  "xAI",
  "Grok",
  "Google",
  "Azure",
  "DeepSeek",
  "Kimi",
  "Moonshot",
  "Cursor",
  "OpenCode",
  "Xiaomi",
  "Mimo",
  "Claude",
  "ChatGPT",
  "Benes",
  "benes",
  "OAuth",
  "API",
]);

const BRAND_LITERALS_LOWER = new Set(
  [...BRAND_LITERALS].map((name) => name.toLowerCase()),
);

/** Single-token technical units / abbreviations shown next to numbers. */
const TECHNICAL_UNITS = new Set([
  "ms",
  "k",
  "M",
  "1M",
  "c",
  "w",
  "v",
  "Mo",
  "Mi",
  "Fr",
  "HTTP",
  // IEC binary unit rendered next to a formatted number; a unit symbol, not UI prose.
  "GiB",
]);

function isHttpUrlLiteral(value: string): boolean {
  return /^https?:\/\//i.test(value)
    || /^https?:\/\/[^\s]+$/i.test(value)
    || /^http:\/\/127\.0\.0\.1(:\d+)?$/i.test(value);
}

function isPathOrQueryLiteral(value: string): boolean {
  if (value.startsWith("/") && /^\/[\w./?=&%-]+$/.test(value)) return true;
  return /^[?&][a-zA-Z_][\w-]*=$/.test(value);
}

function isCssFunctionLiteral(value: string): boolean {
  return /^(var\(--|calc\(|repeat\(|hsl\(|url\(|linear-gradient\()/i.test(value);
}

function isDottedIdentifierLiteral(value: string): boolean {
  return /^[\w-]+(\.[\w-]+)+$/i.test(value);
}

function isShellEnvLiteral(value: string): boolean {
  if (/^export\b/i.test(value)) return true;
  if (/^[A-Z][A-Z0-9_]+=/.test(value)) return true;
  return /^[A-Z][A-Z0-9_]+$/.test(value) && value.includes("_");
}

function isShellCommentLiteral(value: string): boolean {
  return value.startsWith("#");
}

function isCliSampleLiteral(value: string): boolean {
  return /^curl\b/i.test(value)
    || /^-H\b/.test(value)
    || /^-d\b/.test(value)
    || /^\\+\s+-(?:H|d)\b/.test(value)
    || /^benes\b/i.test(value)
    || /^codex\b/i.test(value);
}

function isHttpProtocolLiteral(value: string): boolean {
  return /^HTTP$/i.test(value)
    || /^Authorization\b/i.test(value)
    || /^Bearer\b/i.test(value)
    || /^Content-Type\b/i.test(value)
    || /^x-[\w-]+$/i.test(value);
}

function isDebugAssignmentLiteral(value: string): boolean {
  return /^[a-zA-Z_][\w]*=/.test(value);
}

function isJsonSnippetLiteral(value: string): boolean {
  if (/^[{[]/.test(value) || /[}\]]$/.test(value)) return true;
  return /^"(model|input|type|name)":/.test(value);
}

function isAdapterModeLiteral(value: string): boolean {
  return /^(oauth|passthrough|forward|local|key|openai-chat|openai-responses|anthropic|google|azure-openai|cursor)$/i.test(
    value,
  );
}

function isReleaseChannelLiteral(value: string): boolean {
  return /^(latest|preview)$/i.test(value);
}

function isProtocolFieldLiteral(value: string): boolean {
  return /^(thinking|effort|beta|metadata|system|model|resolved|requestedTier|configuredTier|responseTier|supportsTier)$/i.test(
    value,
  );
}

const TECHNICAL_LITERAL_CLASSIFIERS: ReadonlyArray<(value: string) => boolean> = [
  isHttpUrlLiteral,
  isPathOrQueryLiteral,
  isCssFunctionLiteral,
  isDottedIdentifierLiteral,
  isShellEnvLiteral,
  isShellCommentLiteral,
  isCliSampleLiteral,
  isHttpProtocolLiteral,
  isDebugAssignmentLiteral,
  isJsonSnippetLiteral,
  isAdapterModeLiteral,
  isReleaseChannelLiteral,
  isProtocolFieldLiteral,
];

/** Non-UI technical strings (API paths, CSS, shell, headers, debug fields). */
export function isTechnicalLiteral(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed) return true;
  if (TECHNICAL_UNITS.has(trimmed)) return true;
  return TECHNICAL_LITERAL_CLASSIFIERS.some((matches) => matches(trimmed));
}

/** Model/catalog ids: gpt-4o, claude-3-5-sonnet, deepseek-v4-flash-free, provider/model */
export function isModelIdentifier(value: string): boolean {
  const trimmed = value.trim();
  if (!trimmed || /\s/.test(trimmed)) return false;
  if (!/^[a-z0-9][a-z0-9._\-/+:]*$/i.test(trimmed)) return false;
  return (
    /[a-z]/i.test(trimmed) &&
    (/\d/.test(trimmed) ||
      /[-_/]/.test(trimmed) ||
      /^gpt/i.test(trimmed) ||
      /^claude/i.test(trimmed))
  );
}

export function isBrandOrModelLiteral(value: unknown): boolean {
  if (typeof value !== "string") return false;
  const trimmed = value.trim();
  if (!trimmed) return false;
  if (
    BRAND_LITERALS.has(trimmed) ||
    BRAND_LITERALS_LOWER.has(trimmed.toLowerCase())
  ) {
    return true;
  }
  if (isModelIdentifier(trimmed)) return true;
  return false;
}
