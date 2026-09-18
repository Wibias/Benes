/**
 * Add Provider catalog groups sibling config ids (Command Code Auth + API,
 * OpenAI Codex + API key, …) into one picker row. Connection still posts
 * the selected member id; families are display-only.
 */

/** Brand groups. Member order is the Connection-step default order. */
export const CATALOG_FAMILY_MEMBERS: ReadonlyArray<readonly string[]> = [
  ["openai", "openai-apikey"],
  ["command-code", "commandcode"],
  ["anthropic", "anthropic-apikey"],
  ["kimi", "kimi-code", "moonshot"],
  ["opencode-zen", "opencode-go", "opencode-free"],
  ["google", "google-vertex"],
  ["ollama", "ollama-cloud"],
  ["volcengine", "volcengine-coding-plan", "volcengine-agent-plan"],
  ["alibaba", "alibaba-token-plan", "alibaba-token-plan-intl"],
  ["minimax", "minimax-cn"],
  ["mimo-free", "xiaomi", "xiaomi-mimo", "mimo"],
  ["zai", "zhipu-bigmodel", "zhipu-bigmodel-coding"],
  ["cloudflare-ai-gateway", "cloudflare-workers-ai"],
  ["fireworks", "firepass"],
];

const FAMILY_LABELS: Record<string, string> = {
  openai: "OpenAI",
  "command-code": "Command Code",
  anthropic: "Anthropic",
  kimi: "Kimi",
  "opencode-zen": "OpenCode",
  google: "Google",
  ollama: "Ollama",
  volcengine: "Volcengine",
  alibaba: "Alibaba",
  minimax: "MiniMax",
  "mimo-free": "Xiaomi MiMo",
  zai: "Zhipu AI",
  "cloudflare-ai-gateway": "Cloudflare",
  fireworks: "Fireworks",
};

const MEMBER_TO_FAMILY = new Map<string, string>();
for (const members of CATALOG_FAMILY_MEMBERS) {
  const familyId = members[0]!;
  for (const id of members) MEMBER_TO_FAMILY.set(id, familyId);
}

export type CatalogFamilyMember = {
  id: string;
  label: string;
};

export type CatalogFamily<T extends CatalogFamilyMember = CatalogFamilyMember> = {
  id: string;
  label: string;
  members: T[];
};

export function catalogFamilyId(presetId: string): string {
  return MEMBER_TO_FAMILY.get(presetId) ?? presetId;
}

export function catalogFamilyLabel(familyId: string, members: CatalogFamilyMember[]): string {
  return FAMILY_LABELS[familyId] ?? members[0]?.label ?? familyId;
}

export function groupCatalogFamilies<T extends CatalogFamilyMember>(presets: T[]): CatalogFamily<T>[] {
  const byId = new Map(presets.filter(p => p.id !== "custom").map(p => [p.id, p]));
  const used = new Set<string>();
  const families: CatalogFamily<T>[] = [];
  for (const memberIds of CATALOG_FAMILY_MEMBERS) {
    const members = memberIds.map(id => byId.get(id)).filter((row): row is T => !!row);
    if (members.length === 0) continue;
    const id = memberIds[0]!;
    for (const row of members) used.add(row.id);
    families.push({ id, label: catalogFamilyLabel(id, members), members });
  }
  for (const preset of byId.values()) {
    if (used.has(preset.id)) continue;
    families.push({ id: preset.id, label: preset.label, members: [preset] });
  }
  return families;
}

export function catalogFamilyForPreset<T extends CatalogFamilyMember>(
  presetId: string,
  catalog: T[],
): CatalogFamily<T> | undefined {
  return groupCatalogFamilies(catalog).find(family => family.members.some(row => row.id === presetId));
}

function familyMatchesQuery<T extends CatalogFamilyMember>(family: CatalogFamily<T>, query: string): boolean {
  const q = query.trim().toLowerCase();
  if (!q) return true;
  if (family.label.toLowerCase().includes(q) || family.id.toLowerCase().includes(q)) return true;
  return family.members.some(row => row.label.toLowerCase().includes(q) || row.id.toLowerCase().includes(q));
}

export function visibleCatalogFamilies<T extends CatalogFamilyMember>(
  presets: T[],
  query: string,
  featuredIds: readonly string[],
  memberVisible: (row: T) => boolean,
): CatalogFamily<T>[] {
  const families = groupCatalogFamilies(presets).filter(family => familyMatchesQuery(family, query));
  const matching = families.filter(family => family.members.some(memberVisible));
  const byLabel = (a: CatalogFamily<T>, b: CatalogFamily<T>) =>
    a.label.localeCompare(b.label, undefined, { sensitivity: "base" }) || a.id.localeCompare(b.id);
  if (query.trim()) return matching.toSorted(byLabel);
  const featured: CatalogFamily<T>[] = [];
  const seen = new Set<string>();
  for (const presetId of featuredIds) {
    const family = matching.find(row => row.members.some(member => member.id === presetId));
    if (!family || seen.has(family.id)) continue;
    seen.add(family.id);
    featured.push(family);
  }
  const rest = matching.filter(row => !seen.has(row.id)).toSorted(byLabel);
  return [...featured, ...rest];
}

export function preferredCatalogFamilyMember<T extends CatalogFamilyMember>(
  family: CatalogFamily<T>,
  existingNames: readonly string[],
  hintedIds?: ReadonlySet<string>,
): T {
  const names = new Set(existingNames.map(name => name.toLowerCase()));
  const pool = hintedIds && hintedIds.size > 0
    ? family.members.filter(row => hintedIds.has(row.id))
    : family.members;
  const pickFrom = pool.length > 0 ? pool : family.members;
  return pickFrom.find(row => !names.has(row.id.toLowerCase())) ?? pickFrom[0]!;
}
