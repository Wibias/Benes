import type { KeyboardEvent } from "react";
import { parseHashRoute } from "../hash-routing";
import {
  usageBreakdownFromPath,
  usageTabFromPath,
  usageTabHash,
  type UsageBoardTab,
  type UsageBreakdownTab,
} from "./usage-contract";

export function readUsageTabFromHash(hash?: string): UsageBoardTab {
  const raw = hash ?? (typeof window !== "undefined" ? window.location.hash : "");
  return usageTabFromPath(parseHashRoute(raw).path);
}

export function readUsageBreakdownFromHash(hash?: string): UsageBreakdownTab {
  const raw = hash ?? (typeof window !== "undefined" ? window.location.hash : "");
  return usageBreakdownFromPath(parseHashRoute(raw).path);
}

export function selectUsageTab(tab: UsageBoardTab, breakdown: UsageBreakdownTab = "models") {
  window.location.hash = usageTabHash(tab, breakdown);
}

export function usageTabKeyDown(e: KeyboardEvent, current: UsageBoardTab, breakdown: UsageBreakdownTab) {
  const order: UsageBoardTab[] = ["overview", "breakdown", "coverage"];
  const index = order.indexOf(current);
  if (e.key === "Home") {
    e.preventDefault();
    selectUsageTab("overview");
    document.getElementById("usage-tab-overview")?.focus();
    return;
  }
  if (e.key === "End") {
    e.preventDefault();
    selectUsageTab("coverage");
    document.getElementById("usage-tab-coverage")?.focus();
    return;
  }
  if (e.key === "ArrowLeft" && index > 0) {
    e.preventDefault();
    const next = order[index - 1];
    selectUsageTab(next, breakdown);
    document.getElementById(`usage-tab-${next}`)?.focus();
  } else if (e.key === "ArrowRight" && index < order.length - 1) {
    e.preventDefault();
    const next = order[index + 1];
    selectUsageTab(next, breakdown);
    document.getElementById(`usage-tab-${next}`)?.focus();
  }
}
