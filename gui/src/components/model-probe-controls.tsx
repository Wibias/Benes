import { useCallback, useState } from "react";
import { useT } from "../i18n/shared";
import type { TFn } from "../i18n/shared";
import { readJsonOrThrow } from "../fetch-json";
import {
  probeBulkNotice,
  probeResultNotice,
  type ProbeResult,
  type ProbeState,
} from "../model-probe-notice";

function stateLabel(t: TFn, state: ProbeState | undefined): string {
  switch (state) {
    case "available":
      return t("models.probeAvailable");
    case "unavailable":
      return t("models.probeUnavailable");
    case "auth_required":
      return t("models.probeAuthRequired");
    case "quota_limited":
      return t("models.probeQuotaLimited");
    case "unsupported_probe":
      return t("models.probeUnsupported");
    default:
      return t("models.probeUnknown");
  }
}

async function runProbe(apiBase: string, provider: string, models: string[]): Promise<ProbeResult[]> {
  const body = models.length === 1
    ? { provider, model: models[0] }
    : { provider, models };
  // Empty apiBase is same-origin (`VITE_API_BASE=""`); the relative `/api/...` path is correct.
  const response = await fetch(`${apiBase}/api/models/probe`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  const payload = await readJsonOrThrow<ProbeResult | { results?: ProbeResult[] }>(response, "probe failed");
  if (payload && "results" in payload && Array.isArray(payload.results)) {
    return payload.results;
  }
  return [payload as ProbeResult];
}

export function ModelProbeAllButton({
  apiBase,
  provider,
  models,
  onNotice,
}: {
  apiBase: string;
  provider: string;
  models: string[];
  onNotice?: (ok: boolean, message: string) => void;
}) {
  const t = useT();
  const [busy, setBusy] = useState(false);
  const [summary, setSummary] = useState("");
  const onClick = useCallback(async () => {
    if (models.length === 0 || busy) return;
    setBusy(true);
    try {
      const results = await runProbe(apiBase, provider, models);
      const available = results.filter((row) => row.state === "available").length;
      setSummary(`${available}/${results.length}`);
      const notice = probeBulkNotice(t, results);
      onNotice?.(notice.ok, notice.text);
    } catch {
      setSummary(t("models.probeFailed"));
      onNotice?.(false, t("models.probeFailed"));
    } finally {
      setBusy(false);
    }
  }, [apiBase, busy, models, onNotice, provider, t]);
  return (
    <button
      type="button"
      className="btn btn-ghost btn-sm text-caption"
      onClick={(event) => {
        event.stopPropagation();
        void onClick();
      }}
      disabled={busy || models.length === 0}
      title={t("models.probeHint")}
    >
      {busy ? t("models.probing") : `${t("models.probeAll")}${summary ? ` ${summary}` : ""}`}
    </button>
  );
}

export function ModelProbeButton({
  apiBase,
  provider,
  model,
  onNotice,
}: {
  apiBase: string;
  provider: string;
  model: string;
  onNotice?: (ok: boolean, message: string) => void;
}) {
  const t = useT();
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState<ProbeResult | null>(null);
  const onClick = useCallback(async () => {
    if (busy) return;
    setBusy(true);
    try {
      const [got] = await runProbe(apiBase, provider, [model]);
      setResult(got);
      const notice = probeResultNotice(t, got ?? { model, reason: "probe_failed" });
      onNotice?.(notice.ok, notice.text);
    } catch {
      setResult({ state: "unknown", reason: "probe_failed" });
      onNotice?.(false, t("models.probeFailed"));
    } finally {
      setBusy(false);
    }
  }, [apiBase, busy, model, onNotice, provider, t]);
  const label = busy ? t("models.probing") : (result ? stateLabel(t, result.state) : t("models.probe"));
  const title = result?.timing
    ? t("models.probeLatency", {
      headers: String(result.timing.headersMs ?? "—"),
      first: String(result.timing.firstOutputMs ?? "—"),
      total: String(result.timing.totalMs ?? "—"),
    })
    : t("models.probeHint");
  return (
    <button
      type="button"
      className="btn btn-ghost btn-sm text-caption"
      onClick={(event) => {
        event.stopPropagation();
        void onClick();
      }}
      disabled={busy}
      title={title}
    >
      {label}
    </button>
  );
}
