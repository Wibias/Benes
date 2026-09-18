/** Separator standing in for "/" inside the model-id portion of a Codex-facing slug. */
export const SLUG_ALIAS_SEPARATOR = "-";

/** Native model id -> Codex-facing alias id. No-op for ids without "/". */
export function encodeRoutedModelId(id: string): string {
  return id.includes("/") ? id.replaceAll("/", SLUG_ALIAS_SEPARATOR) : id;
}

/**
 * True when `modelId` shares a Codex-facing encoded form with a different known id.
 * That collision is what makes `provider/openai-gpt-5.5` decode to native `openai-gpt-5.5`
 * while a custom `openai/gpt-5.5` row is still visible.
 */
export function encodedModelIdCollides(modelId: string, knownIds: Iterable<string>): boolean {
  const encoded = encodeRoutedModelId(modelId);
  for (const id of knownIds) {
    if (id === modelId) continue;
    if (encodeRoutedModelId(id) === encoded) return true;
  }
  return false;
}
