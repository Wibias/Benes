/** Benes dashboard client for the Go proxy (`internal/server`). */

/** How the OpenAI provider is currently run: a credential pool, a single login, or nothing. */
export type CodexAccountModeState = "pool" | "direct" | "disabled" | "absent";

function asRecord(value: unknown): Record<string, unknown> | null {
  if (typeof value !== "object" || value === null || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

/**
 * Read the OpenAI provider's account mode out of a `/api/config` payload.
 *
 * A row the dashboard cannot recognise as a live OpenAI provider — absent, malformed,
 * or carrying an unknown mode string — is reported as `absent`, so callers never paint
 * pool controls over something that is not there. A disabled row is reported before its
 * mode, because a disabled provider routes nothing regardless of how it is configured.
 */
export function codexAccountModeState(config: unknown): CodexAccountModeState {
  const root = asRecord(config);
  if (root === null) return "absent";
  const providers = asRecord(root.providers);
  if (providers === null || !Object.hasOwn(providers, "openai")) return "absent";
  const openai = asRecord(providers.openai);
  if (openai === null) return "absent";
  if (openai.disabled === true) return "disabled";
  const configured = openai.codexAccountMode;
  if (configured === "direct") return "direct";
  if (configured === undefined || configured === "pool") return "pool";
  return "absent";
}
