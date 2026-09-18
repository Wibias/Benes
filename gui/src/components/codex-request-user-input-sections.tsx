/** Copy, control, and feedback sections for the Codex request-user-input flag card. */
import { useT } from "../i18n/shared";
import type { RequestUserInputFeedback } from "../hooks/useCodexRequestUserInputFlag";

/** What Codex needs in `$CODEX_HOME/config.toml` for this flag; shown so the operator can diff it. */
const CONFIG_LINES = ["[features]", "default_mode_request_user_input = true"];

/** `is-error` and the live-region role are the only things a failed write changes. */
const FEEDBACK_STYLES: Record<RequestUserInputFeedback["tone"], { className: string; role: "status" | "alert" }> = {
  ok: { className: "codex-request-user-input-feedback", role: "status" },
  err: { className: "codex-request-user-input-feedback is-error", role: "alert" },
};

export function RequestUserInputCopy({ loadError }: { loadError: boolean }) {
  const t = useT();
  const heading = t("codexAuth.requestUserInput");
  const notice = {
    role: loadError ? "alert" as const : undefined,
    text: t(loadError ? "codexAuth.requestUserInputLoadFailed" : "codexAuth.requestUserInputDesc"),
  };
  return (
    <div className="codex-request-user-input-copy">
      <strong>{heading}</strong>
      <div className="card-sub" role={notice.role}>{notice.text}</div>
      <code className="mono codex-request-user-input-config">{CONFIG_LINES.join("\n")}</code>
    </div>
  );
}

export function RequestUserInputControls({
  enabled,
  locked,
  showRetry,
  onRetry,
  onToggle,
}: {
  enabled: boolean;
  locked: boolean;
  showRetry: boolean;
  onRetry(): void;
  onToggle(): void;
}) {
  const t = useT();
  const label = t("codexAuth.requestUserInput");
  const retry = showRetry
    ? <button type="button" className="btn btn-ghost btn-sm" onClick={onRetry}>{t("common.retry")}</button>
    : null;
  return (
    <div className="codex-request-user-input-controls">
      {retry}
      <button
        type="button"
        className={["toggle", enabled ? "on" : ""].join(" ")}
        onClick={onToggle}
        disabled={locked}
        aria-pressed={enabled}
        aria-label={label}
        title={label}
      >
        <span className="toggle-knob" />
      </button>
    </div>
  );
}

export function RequestUserInputFeedbackLine({ feedback }: { feedback: RequestUserInputFeedback }) {
  const style = FEEDBACK_STYLES[feedback.tone];
  return (
    <div className={style.className} role={style.role} aria-atomic="true">
      {feedback.message}
    </div>
  );
}

