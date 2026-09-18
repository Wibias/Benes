import type { TFn } from "../i18n/shared";
import type { ProviderWorkspaceEvent } from "./workspace";

const VALIDATION_EVENT_TYPES = new Set([
  "credentials_validated",
  "connection_validated",
  "oauth_reauthenticated",
]);

/**
 * User-facing label for provider activity. Known event types are localized; an
 * unknown event keeps its server detail/type rather than inventing meaning.
 */
export function providerEventLabel(event: ProviderWorkspaceEvent, t: TFn): string {
  const detail = event.detail?.trim();
  if (detail) return detail;
  switch (event.type) {
    case "credentials_validated":
    case "connection_validated":
    case "oauth_reauthenticated":
      return t("prov.health.credentials");
    case "model_catalogue_synchronized":
      return t("prov.health.models");
    case "model_catalogue_stale":
      return t("prov.overview.cataloguesStale");
    case "provider_health_failure":
    case "provider_health_recovered":
      return t("prov.health.connection");
    case "quota_exhausted":
    case "quota_recovered":
    case "rate_limited":
      return t("prov.health.quota");
    case "api_key_added":
      return t("prov.keyAdded", { name: event.provider });
    case "api_key_removed":
      return t("prov.keyRemoved", { key: "…" });
    case "api_key_selected":
      return t("prov.keySwitched", { key: "…" });
    case "api_key_used":
      return t("prov.access.apiKeys");
    case "default_access_changed":
      return t("prov.access.defaultAccess");
    default:
      return event.type;
  }
}

/** Latest genuine credential/connection validation event, or null when unknown. */
export function latestProviderValidationAt(events: readonly ProviderWorkspaceEvent[]): number | null {
  let latest = 0;
  for (const event of events) {
    if (!VALIDATION_EVENT_TYPES.has(event.type)) continue;
    if (!Number.isFinite(event.timestamp) || event.timestamp <= latest) continue;
    latest = event.timestamp;
  }
  return latest > 0 ? latest : null;
}
