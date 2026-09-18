/**
 * Map a provider endpoint dropdown choice onto the URL that should be stored.
 * Preset rows own a fixed URL. The custom row keeps whatever the operator typed,
 * unless that text is itself a known preset (then the field clears for a paste).
 */

export type BaseUrlChoice = { id: string; label: string; baseUrl?: string };

type ChoiceList = readonly BaseUrlChoice[] | undefined;

function withoutTrailingSlash(value: string): string {
  return value.trim().replace(/\/+$/, "");
}

function firstPresetId(choices: ChoiceList, baseUrl: string): string | undefined {
  const want = withoutTrailingSlash(baseUrl);
  if (!want) return undefined;
  for (const row of choices ?? []) {
    if (!row.baseUrl) continue;
    if (withoutTrailingSlash(row.baseUrl) === want) return row.id;
  }
  return undefined;
}

function rowById(choices: ChoiceList, choiceId: string): BaseUrlChoice | undefined {
  return choices?.find((row) => row.id === choiceId);
}

function hasCustomRow(choices: ChoiceList): boolean {
  return Boolean(choices?.some((row) => row.id === "custom"));
}

export function baseUrlForChoice(choices: ChoiceList, choiceId: string, previousBaseUrl: string): string {
  const selected = rowById(choices, choiceId);
  if (!selected) return previousBaseUrl;
  if (selected.baseUrl) return selected.baseUrl;
  if (firstPresetId(choices, previousBaseUrl)) return "";
  return previousBaseUrl;
}

export function matchChoiceId(choices: ChoiceList, baseUrl: string): string {
  if (!choices?.length) return "custom";
  const hit = firstPresetId(choices, baseUrl);
  if (hit) return hit;
  if (hasCustomRow(choices)) return "custom";
  return choices[0]!.id;
}

export function resolvedBaseUrlForChoice(choices: ChoiceList, choiceId: string, customBaseUrl: string): string {
  const selected = rowById(choices, choiceId);
  if (selected?.baseUrl) return selected.baseUrl.trim();
  return customBaseUrl.trim();
}
