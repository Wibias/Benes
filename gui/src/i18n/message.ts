/** Named-placeholder interpolation and closed-domain locale lookup. */

export type MessageVars = Record<string, string | number>;

const PLACEHOLDER = /\{([^{}]+)\}/g;

export function interpolate(template: string, vars?: MessageVars): string {
  if (!vars) return template;
  return template.replace(PLACEHOLDER, (matched, name: string) => {
    if (!Object.hasOwn(vars, name)) return matched;
    return String(vars[name]);
  });
}

export function closedText<K extends string>(
  locale: string,
  key: K,
  english: Record<K, string>,
  overrides: Partial<Record<string, Partial<Record<K, string>>>>,
): string {
  if (locale !== "en") {
    const translated = overrides[locale]?.[key];
    if (translated !== undefined) return translated;
  }
  return english[key] ?? key;
}
