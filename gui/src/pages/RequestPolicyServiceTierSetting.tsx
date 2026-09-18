/** Global Benes request-tier policy control. */
import { useCallback, useEffect, useId, useRef, useState } from "react";
import { useT, type TKey } from "../i18n/shared";
import { ToastNotice } from "../ui";
import {
  isRequestPolicyServiceTier,
  readRequestPolicyServiceTier,
  requestPolicyPutBody,
  type RequestPolicyServiceTier,
} from "../request-policy-service-tier";

const TIER_LABEL_KEYS: Record<RequestPolicyServiceTier, TKey> = {
  "": "routing.requestPolicy.preserve",
  auto: "routing.requestPolicy.auto",
  default: "routing.requestPolicy.default",
  flex: "routing.requestPolicy.flex",
  priority: "routing.requestPolicy.priority",
};

const TIER_VALUES = Object.keys(TIER_LABEL_KEYS) as RequestPolicyServiceTier[];

/** How long a read or save result stays on screen before it dismisses itself. */
const NOTICE_MS = 8_000;

type Notice = { tone: "ok" | "err"; text: string };

/**
 * Read one settings response.
 *
 * The listener reports a failed management call as `{"error":{"code":…,"message":…}}`. That
 * message is what the failure toast has to show, so the envelope is read here rather than
 * reduced to a bare status.
 */
async function settingsEnvelope(response: Response): Promise<unknown> {
  const body = await response.json().catch(() => null) as unknown;
  if (!response.ok) {
    const failure = body as { error?: { message?: string }; message?: string } | null;
    throw new Error(failure?.error?.message || failure?.message || `HTTP ${response.status}`);
  }
  return body;
}

export default function RequestPolicyServiceTierSetting({ apiBase, active = true }: { apiBase: string; active?: boolean }) {
  const t = useT();
  const titleId = useId();
  const selectId = useId();
  const descriptionId = useId();
  const [tier, setTier] = useState<RequestPolicyServiceTier>("");
  const [savedTier, setSavedTier] = useState<RequestPolicyServiceTier>("");
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);
  const [notice, setNotice] = useState<Notice | null>(null);
  const noticeTimer = useRef<number | null>(null);

  // A settings read or write never reports silently and never parks a result in page flow: the
  // outcome is one auto-dismissing toast, and a failure carries the listener's own message.
  const announce = useCallback((tone: Notice["tone"], text: string) => {
    if (noticeTimer.current !== null) window.clearTimeout(noticeTimer.current);
    setNotice({ tone, text });
    noticeTimer.current = window.setTimeout(() => {
      noticeTimer.current = null;
      setNotice(null);
    }, NOTICE_MS);
  }, []);

  useEffect(() => () => {
    if (noticeTimer.current !== null) window.clearTimeout(noticeTimer.current);
  }, []);

  /** Adopts the tier the listener's settings envelope reports. */
  const adoptSettings = useCallback((body: unknown) => {
    const next = readRequestPolicyServiceTier(body);
    setTier(next);
    setSavedTier(next);
  }, []);

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true);
    try {
      adoptSettings(await settingsEnvelope(await fetch(`${apiBase}/api/settings`, { signal })));
    } catch (error) {
      if (error instanceof Error && error.name === "AbortError") return;
      announce("err", error instanceof Error ? error.message : String(error));
    } finally {
      setLoading(false);
    }
  }, [adoptSettings, announce, apiBase]);

  useEffect(() => {
    if (!active) return;
    const controller = new AbortController();
    // Deferred so the effect body performs no synchronous state update.
    const timer = window.setTimeout(() => {
      void load(controller.signal);
    }, 0);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [active, load]);

  const save = async () => {
    setSaving(true);
    try {
      adoptSettings(await settingsEnvelope(await fetch(`${apiBase}/api/settings`, {
        method: "PUT",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(requestPolicyPutBody(tier)),
      })));
      announce("ok", t("routing.requestPolicy.saved"));
    } catch (error) {
      announce("err", error instanceof Error ? error.message : String(error));
    } finally {
      setSaving(false);
    }
  };

  const dirty = tier !== savedTier;

  return (
    <>
      <section className="settings-card" aria-labelledby={titleId}>
        <div className="setting-row">
          <div className="setting-label">
            <span className="title" id={titleId}>{t("routing.requestPolicy.title")}</span>
            <span className="desc" id={descriptionId}>{t("routing.requestPolicy.desc")}</span>
          </div>
          <div className="setting-control">
            <select
              id={selectId}
              value={tier}
              aria-describedby={descriptionId}
              disabled={loading || saving}
              onChange={(event) => {
                if (isRequestPolicyServiceTier(event.target.value)) setTier(event.target.value);
              }}
            >
              {TIER_VALUES.map(value => (
                <option key={value || "preserve"} value={value}>{t(TIER_LABEL_KEYS[value])}</option>
              ))}
            </select>
            <button type="button" className="btn btn-primary btn-sm" disabled={!dirty || loading || saving} onClick={() => { void save(); }}>
              {saving ? t("common.saving") : t("common.save")}
            </button>
          </div>
        </div>
      </section>
      {notice !== null && (
        <ToastNotice
          tone={notice.tone}
          dismissLabel={t("common.close")}
          onDismiss={() => setNotice(null)}
        >
          {notice.text}
        </ToastNotice>
      )}
    </>
  );
}
