/** Copy and control sections for the Codex account-picker setting. */
import { useT, type TKey } from "../i18n/shared";
import type { NoticeTone } from "../ui";
import {
  accountPickerCopyKind,
  accountPickerShowsCompatibility,
  accountPickerShowsRefreshFailed,
} from "../lib/codex-account-picker-policy";

/** Class vocabulary for the picker card, kept in one place. */
const COPY_CLASS = "codex-account-picker-copy";
const CONTROLS_CLASS = "codex-account-picker-controls";
const SUB_CLASS = "card-sub";
const SUB_FAINT_CLASS = "card-sub faint";
const RETRY_CLASS = "btn btn-ghost btn-sm";
const TOGGLE_CLASS = "toggle";
const TOGGLE_ON_CLASS = "toggle on";
const KNOB_CLASS = "toggle-knob";
const FEEDBACK_CLASS = "codex-account-picker-feedback";

/** Copy this section resolves. */
const TITLE_KEY: TKey = "codexAuth.accountPickerTitle";
const LOAD_FAILED_KEY: TKey = "codexAuth.accountPickerLoadFailed";
const LOADING_KEY: TKey = "common.loading";
const ON_DESC_KEY: TKey = "codexAuth.accountPickerOnDesc";
const OFF_DESC_KEY: TKey = "codexAuth.accountPickerOffDesc";
const COMPATIBILITY_KEY: TKey = "codexAuth.accountPickerCompatibility";
const REFRESH_FAILED_KEY: TKey = "codexAuth.accountPickerRefreshFailed";
const RETRY_KEY: TKey = "common.retry";

/**
 * One muted line under the title.
 *
 * The three lines the card can show differ only in how faint they are and whether a screen
 * reader should announce them, so they come from one component rather than three nearly
 * identical blocks.
 */
function PickerSubLine({ copy, faint = false, announced = false }: {
  copy: string;
  faint?: boolean;
  announced?: boolean;
}) {
  return (
    <div className={faint ? SUB_FAINT_CLASS : SUB_CLASS} role={announced ? "status" : undefined}>
      {copy}
    </div>
  );
}

export function CodexAccountPickerCopy({
  loadError,
  hydrated,
  enabled,
}: {
  loadError: boolean;
  hydrated: boolean;
  enabled: boolean;
}) {
  const t = useT();
  const kind = accountPickerCopyKind({ loadError, hydrated, enabled });
  const description = kind === "load-failed"
    ? t(LOAD_FAILED_KEY)
    : kind === "loading"
      ? t(LOADING_KEY)
      : t(kind === "on" ? ON_DESC_KEY : OFF_DESC_KEY);

  return (
    <div className={COPY_CLASS}>
      <strong>{t(TITLE_KEY)}</strong>
      <PickerSubLine copy={description} announced={kind === "load-failed"} />
      {accountPickerShowsCompatibility(hydrated, enabled) && (
        <PickerSubLine copy={t(COMPATIBILITY_KEY)} faint />
      )}
      {accountPickerShowsRefreshFailed(hydrated, loadError) && (
        <PickerSubLine copy={t(REFRESH_FAILED_KEY)} faint announced />
      )}
    </div>
  );
}

export function CodexAccountPickerControls({
  loadError,
  hydrated,
  enabled,
  saving,
  onRetry,
  onToggle,
}: {
  loadError: boolean;
  hydrated: boolean;
  enabled: boolean;
  saving: boolean;
  onRetry: () => void;
  onToggle: () => void;
}) {
  const t = useT();
  const title = t(TITLE_KEY);
  return (
    <div className={CONTROLS_CLASS}>
      {loadError && (
        <button type="button" className={RETRY_CLASS} onClick={onRetry} disabled={saving}>
          {t(RETRY_KEY)}
        </button>
      )}
      {hydrated && (
        <button
          type="button"
          className={enabled ? TOGGLE_ON_CLASS : TOGGLE_CLASS}
          onClick={onToggle}
          disabled={saving}
          aria-pressed={enabled}
          aria-label={title}
          title={title}
        >
          <span className={KNOB_CLASS} />
        </button>
      )}
    </div>
  );
}

export function CodexAccountPickerFeedback({
  feedback,
}: {
  feedback: { tone: NoticeTone; message: string };
}) {
  const isError = feedback.tone === "err";
  return (
    <div
      className={FEEDBACK_CLASS + " is-" + feedback.tone}
      role={isError ? "alert" : "status"}
      aria-atomic="true"
    >
      {feedback.message}
    </div>
  );
}
