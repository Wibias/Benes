/** Toast copy for an on-demand model probe. Pure so the button can stay a thin fetch wrapper. */

import type { TFn } from "./i18n/shared";

export type ProbeState = "available" | "unavailable" | "auth_required" | "quota_limited" | "unsupported_probe" | "unknown";

export type ProbeResult = {
  provider?: string;
  model?: string;
  state?: ProbeState;
  reason?: string;
  timing?: { headersMs?: number; firstOutputMs?: number; totalMs?: number };
};

export type ProbeNotice = {
  ok: boolean;
  text: string;
};

export function probeResultNotice(t: TFn, result: ProbeResult): ProbeNotice {
  const model = (result.model ?? "").trim() || "—";
  switch (result.state) {
    case "available":
      return { ok: true, text: t("models.probeToastAvailable", { model }) };
    case "unavailable":
      return { ok: false, text: t("models.probeToastUnavailable", { model }) };
    case "auth_required":
      return { ok: false, text: t("models.probeToastAuth", { model }) };
    case "quota_limited":
      return { ok: false, text: t("models.probeToastQuota", { model }) };
    case "unsupported_probe":
      if (result.reason === "forward_auth") {
        return { ok: false, text: t("models.probeToastForward") };
      }
      return { ok: false, text: t("models.probeToastUnsupported") };
    default:
      if (result.reason === "probe_failed") {
        return { ok: false, text: t("models.probeFailed") };
      }
      return { ok: false, text: t("models.probeToastUnknown", { model }) };
  }
}

export function probeBulkNotice(t: TFn, results: ProbeResult[]): ProbeNotice {
  if (results.length === 1) return probeResultNotice(t, results[0]);
  if (results.length === 0) return { ok: false, text: t("models.probeFailed") };
  if (results.every(row => row.state === "unsupported_probe")) {
    if (results.some(row => row.reason === "forward_auth")) {
      return { ok: false, text: t("models.probeToastForward") };
    }
    return { ok: false, text: t("models.probeToastUnsupported") };
  }
  const available = results.filter(row => row.state === "available").length;
  const total = String(results.length);
  if (available === results.length) {
    return { ok: true, text: t("models.probeToastAllOk", { total }) };
  }
  return { ok: false, text: t("models.probeToastAll", { available: String(available), total }) };
}
