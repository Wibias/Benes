import type { TKey } from "../../i18n/shared";
import { IntegrationApiError } from "./integration-api.ts";
import { NativeApiError, type NativeRefusalEnvelope } from "./native-api.ts";

export type Translate = (key: TKey, vars?: Record<string, string>) => string;

const BUSY = "integration_mutation_busy";

function copyOrphanedMarker(t: Translate, configPath: string): string {
  return t("integrations.native.error.orphanedMarker", { path: configPath });
}

function copyHomeMismatch(t: Translate, serverMessage: string): string {
  return `${t("integrations.native.error.homeMismatch")} ${serverMessage}`;
}

function copyDesktopMetadata(t: Translate, configPath: string): string {
  return t("integrations.native.error.desktopUnsafeMetadata", { path: configPath });
}

function copyDesktopCleanup(t: Translate, leftover: readonly string[]): string {
  return t("integrations.native.error.desktopCleanupIncomplete", { paths: leftover.join(", ") });
}

function nativeSentence(t: Translate, refusal: NativeRefusalEnvelope, configPath: string): string {
  switch (refusal.reason) {
    case "orphaned_marker":
      return copyOrphanedMarker(t, configPath);
    case "home_mismatch":
      return copyHomeMismatch(t, refusal.message);
    case "not_installed":
      return t("integrations.native.error.notInstalled");
    case "config_busy":
      return t("integrations.native.error.configBusy");
    case "metadata_unreadable":
      return copyDesktopMetadata(t, configPath);
    case "cleanup_incomplete":
      return copyDesktopCleanup(t, refusal.residualPaths ?? []);
    default:
      return refusal.message || t("integrations.error.generic");
  }
}

function fileLead(reason: string | undefined): TKey {
  if (reason === "conflict") return "integrations.error.conflict";
  if (reason === "unsafe") return "integrations.error.unsafe";
  if (reason === "non_loopback") return "integrations.error.nonLoopback";
  return "integrations.error.generic";
}

function busyOrPlain(t: Translate, error: IntegrationApiError, fallback?: string): string {
  if (String(error.body.code ?? "") === BUSY) return t("integrations.error.busy");
  return error.message || fallback || t("integrations.error.generic");
}

function snapshotFollowUp(t: Translate, message: string, snapshotPath: string, residual: boolean | undefined): string {
  const template: TKey = residual ? "integrations.error.residual" : "integrations.error.recover";
  return t(template, { message, path: snapshotPath });
}

function fileDetail(t: Translate, reason: string, clientId: string, message: string, key: TKey): string {
  if (reason === "non_loopback") return t(key, { client: clientId });
  return message || t(key);
}

function fileSentence(t: Translate, error: IntegrationApiError, fallback?: string): string {
  const refusal = error.refusal;
  if (!refusal) return busyOrPlain(t, error, fallback);
  const key = fileLead(refusal.reason);
  const detail = fileDetail(t, refusal.reason, refusal.clientId, refusal.message, key);
  if (refusal.snapshotPath) return snapshotFollowUp(t, detail, refusal.snapshotPath, refusal.residual);
  if (refusal.reason === "conflict" || refusal.reason === "unsafe") return `${t(key)} ${detail}`;
  return detail;
}

export function describeRefusal(
  t: Translate,
  error: unknown,
  fallback?: string,
  nativeConfigPath?: string,
): string {
  if (error instanceof NativeApiError) {
    return error.refusal
      ? nativeSentence(t, error.refusal, nativeConfigPath ?? "")
      : (error.message || t("integrations.error.generic"));
  }
  if (error instanceof IntegrationApiError) return fileSentence(t, error, fallback);
  if (error instanceof Error && error.message) return error.message;
  return fallback ?? t("integrations.error.generic");
}
