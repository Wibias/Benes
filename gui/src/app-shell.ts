import type { Page } from "./app-routing";
import type { TKey } from "./i18n/shared";

export const PAGE_TKEY: Record<Page, TKey> = {
  dashboard: "nav.dashboard",
  startup: "nav.control",
  routing: "nav.routing",
  providers: "nav.providers",
  models: "nav.models",
  subagents: "nav.subagents",
  sessions: "nav.sessions",
  logs: "nav.diagnostics",
  usage: "nav.usage",
  tasks: "nav.tasks",
  storage: "nav.storage",
  api: "nav.api",
  integrations: "nav.integrations",
  harnesses: "nav.harnesses",
};

const PAGE_INNER: Partial<Record<Page, string>> = {
  startup: "main-inner--control",
  routing: "main-inner--routing",
  models: "main-inner--models",
  harnesses: "main-inner--harnesses",
  subagents: "main-inner--subagents",
  providers: "main-inner--providers",
  tasks: "main-inner--tasks",
  api: "main-inner--api",
  sessions: "main-inner--sessions",
  logs: "main-inner--diagnostics",
  usage: "main-inner--usage",
  storage: "main-inner--storage",
};

export function mainInnerClass(page: Page): string {
  const modifiers = ["main-inner"];
  const extra = PAGE_INNER[page];
  if (extra) modifiers.push(extra);
  return modifiers.join(" ");
}

export function nextTheme(theme: "light" | "dark" | "system"): "light" | "dark" | "system" {
  if (theme === "light") return "dark";
  if (theme === "dark") return "system";
  return "light";
}
