import type { HarnessId } from "./types";

export type HarnessDetailTab = "overview" | "settings";

const HARNESS_IDS: readonly HarnessId[] = [
  "claude-desktop",
  "claude",
  "codex",
  "dsh",
  "opencode",
  "pi",
  "prime",
  "omp",
  "hermes",
  "openclaw",
  "kimi",
  "gajae",
  "grok",
  "mcode",
];

export function isHarnessId(value: string): value is HarnessId {
  return (HARNESS_IDS as readonly string[]).includes(value);
}

/** Clients whose detail carries a Settings tab, i.e. a surface of their own. */
const SETTINGS_HARNESS_IDS: readonly HarnessId[] = ["claude", "claude-desktop"];

export function harnessShowsSettings(id: HarnessId | null): boolean {
  return id !== null && SETTINGS_HARNESS_IDS.includes(id);
}

export function parseHarnessHash(hash?: string): { id: HarnessId | null; tab: HarnessDetailTab } {
  const source = hash ?? (typeof window !== "undefined" ? window.location.hash : "");
  const raw = source.replace(/^#\/?/, "");
  if (!raw.startsWith("harnesses/")) return { id: null, tab: "overview" };
  const parts = raw.slice("harnesses/".length).split("/").filter(Boolean);
  const id = parts[0] && isHarnessId(parts[0]) ? parts[0] : null;
  const tab = harnessShowsSettings(id) && parts[1] === "settings" ? "settings" : "overview";
  return { id, tab };
}

export function harnessHash(id: HarnessId, tab: HarnessDetailTab = "overview"): string {
  if (tab === "settings" && harnessShowsSettings(id)) return `harnesses/${id}/settings`;
  return `harnesses/${id}`;
}
