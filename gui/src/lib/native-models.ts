/** ChatGPT/Codex wire id observed for the account-native Daybreak Blue surface. */
export const NATIVE_DAYBREAK_BLUE_MODEL = "gpt-daybreak-blue-latest";

/**
 * Native OpenAI model ids that this release can route and restore with authoritative metadata.
 * Dashboard combo native-alias checks use this same allowlist.
 */
export const NATIVE_OPENAI_MODELS = [
  "gpt-5.5",
  "gpt-5.4",
  "gpt-5.4-mini",
  "gpt-5.3-codex-spark",
  "gpt-5.6-sol",
  "gpt-5.6-terra",
  "gpt-5.6-luna",
  NATIVE_DAYBREAK_BLUE_MODEL,
];

export const SUPPORTED_NATIVE_OPENAI_SLUGS = new Set(NATIVE_OPENAI_MODELS);
