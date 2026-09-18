/**
 * Provider-scoped listener calls the detail head can issue.
 *
 * Both decode the listener's payload into one notice plus the follow-up reads
 * the caller has to invalidate, so the head only surfaces the outcome. Neither
 * reports success for a rejected response.
 */
import type { TFn } from "../i18n/shared";
import { postModelDiscoverySync } from "../model-discovery-sync.ts";
import { connectionTestOutcome } from "./connection-test.ts";

export interface DetailActionOutcome {
  /** Notice text for the page toast. */
  message: string;
  ok: boolean;
  /** A successful probe invalidates the cached quota reports. */
  refreshQuotas: boolean;
  /** A discovery run rewrites the catalog, so the model lists must be re-read. */
  refreshModels: boolean;
}

/** `POST /api/providers/test` for one provider. */
export async function probeProviderConnection(
  apiBase: string,
  name: string,
  t: TFn,
): Promise<DetailActionOutcome> {
  try {
    const res = await fetch(`${apiBase}/api/providers/test?name=${encodeURIComponent(name)}`, { method: "POST" });
    const payload = await res.json().catch(() => ({}));
    const outcome = connectionTestOutcome(res.ok, payload);
    return {
      message: outcome.text ?? t(outcome.messageKey ?? "pws.connectionFailed"),
      ok: outcome.ok,
      refreshQuotas: outcome.ok,
      refreshModels: false,
    };
  } catch {
    return { message: t("prov.networkError"), ok: false, refreshQuotas: false, refreshModels: false };
  }
}

/**
 * `POST /api/model-discovery` for one provider. The listener replaces its
 * in-memory catalog either way, so the caller re-reads the model lists even when
 * the run reports that discovery is disabled for this provider.
 */
export async function syncProviderModels(
  apiBase: string,
  name: string,
  t: TFn,
): Promise<DetailActionOutcome> {
  try {
    const payload = await postModelDiscoverySync(apiBase, name);
    if (payload.ok) {
      return { message: t("prov.health.models"), ok: true, refreshQuotas: false, refreshModels: true };
    }
    const message = payload.applicable === false
      ? t("models.emptyDiscoveryDisabled")
      : (payload.error || t("prov.networkError"));
    return { message, ok: false, refreshQuotas: false, refreshModels: true };
  } catch {
    return { message: t("prov.networkError"), ok: false, refreshQuotas: false, refreshModels: true };
  }
}
