import type { TFn } from "../i18n/shared";
import type { StartupHealthStatus } from "../startup-health-ui";
import type { DashboardProviderRow, ProjectCodexConfigGroup } from "./dashboard-core-poll";
import type { SettingsData } from "./dashboard-shared";

export function listenAddr(settings: SettingsData | null): string {
  const host = settings?.hostname?.trim() || "127.0.0.1";
  const port = settings?.port && settings.port > 0 ? settings.port : 23100;
  return `${host}:${port}`;
}

/** Up to three default models, in provider order. */
export function defaultModelRows(providers: DashboardProviderRow[]): string[] {
  const rows: string[] = [];
  for (const provider of providers) {
    const model = provider.defaultModel?.trim();
    if (model) rows.push(model);
    if (rows.length >= 3) break;
  }
  return rows;
}

export function issueRows(
  t: TFn,
  startupHealth: StartupHealthStatus | null,
  projectConfigWarnings: ProjectCodexConfigGroup[],
): Array<{ tone: "warn" | "err"; text: string }> {
  const rows: Array<{ tone: "warn" | "err"; text: string }> = [];
  if (startupHealth === "at-risk") {
    rows.push({ tone: "warn", text: t("startup.summary.atRisk") });
  }
  const groups = Array.isArray(projectConfigWarnings) ? projectConfigWarnings : [];
  for (const group of groups) {
    const detail = Array.isArray(group.issues) ? group.issues.join(", ") : "";
    rows.push({
      tone: "warn",
      text: detail ? `${group.path} — ${detail}` : group.path,
    });
  }
  return rows;
}
