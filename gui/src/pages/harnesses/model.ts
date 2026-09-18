import type {
  HarnessFilterId,
  HarnessGroupId,
  HarnessRecord,
} from "./types";

export const HARNESS_FILTERS: readonly HarnessFilterId[] = [
  "applied",
  "not-applied",
  "conflict",
  "update-needed",
  "not-installed",
];

export function harnessGroup(harness: HarnessRecord): HarnessGroupId {
  if (!harness.installed) return "not-installed";
  if (harness.applied) return "connected";
  return "available";
}

export function harnessMatchesFilter(harness: HarnessRecord, filter: HarnessFilterId): boolean {
  switch (filter) {
    case "applied":
      return harness.applied;
    case "not-applied":
      return harness.installed && !harness.applied;
    case "conflict":
      return harness.issue === "conflict";
    case "update-needed":
      return harness.issue === "update-needed";
    case "not-installed":
      return !harness.installed;
    default: {
      const _never: never = filter;
      return _never;
    }
  }
}

export function searchHaystack(harness: HarnessRecord, name: string): string {
  return [
    name,
    harness.id,
    harness.detectPath ?? "",
    harness.configPath ?? "",
    harness.snapshotId ?? "",
  ].join(" ").toLowerCase();
}

export function visibleHarnesses(
  harnesses: readonly HarnessRecord[],
  query: string,
  filters: ReadonlySet<HarnessFilterId>,
  nameOf: (harness: HarnessRecord) => string,
): HarnessRecord[] {
  const needle = query.trim().toLowerCase();
  return harnesses.filter((harness) => {
    if (filters.size > 0 && ![...filters].some((filter) => harnessMatchesFilter(harness, filter))) {
      return false;
    }
    if (!needle) return true;
    return searchHaystack(harness, nameOf(harness)).includes(needle);
  });
}

export function groupHarnesses(harnesses: readonly HarnessRecord[]): Record<HarnessGroupId, HarnessRecord[]> {
  const groups: Record<HarnessGroupId, HarnessRecord[]> = {
    connected: [],
    available: [],
    "not-installed": [],
  };
  for (const harness of harnesses) {
    groups[harnessGroup(harness)].push(harness);
  }
  return groups;
}

export function summaryCounts(harnesses: readonly HarnessRecord[]) {
  return {
    active: harnesses.filter((harness) => harness.applied && harness.issue !== "conflict").length,
    available: harnesses.filter((harness) => harness.installed && !harness.applied).length,
    conflict: harnesses.filter((harness) => harness.issue === "conflict").length,
    updateNeeded: harnesses.filter((harness) => harness.issue === "update-needed").length,
    total: harnesses.length,
  };
}
