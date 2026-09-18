/** Default control values for the Usage board orchestration hook. */

import {
  defaultCustomRange,
  type UsageRange,
  type UsageSurface,
} from "./usage-range.ts";

export type UsageWorkspaceControlDefaults = {
  range: UsageRange;
  surface: UsageSurface;
  modelQuery: string;
  timeZone: string;
  customStart: string;
  customEnd: string;
};

export function resolveUsageViewerTimeZone(
  resolved: string | undefined | null,
): string {
  if (resolved && resolved.trim()) return resolved;
  return "UTC";
}

export function buildUsageWorkspaceControlDefaults(
  timeZone: string = resolveUsageViewerTimeZone(
    typeof Intl !== "undefined"
      ? Intl.DateTimeFormat().resolvedOptions().timeZone
      : "UTC",
  ),
): UsageWorkspaceControlDefaults {
  const custom = defaultCustomRange(timeZone);
  return {
    range: "30d",
    surface: "all",
    modelQuery: "",
    timeZone,
    customStart: custom.start,
    customEnd: custom.end,
  };
}
