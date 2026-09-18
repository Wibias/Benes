/** Copy, threshold, and toggle sections for Codex auto-switch. */
import { useT, type TKey } from "../i18n/shared";
import { clampNumberDraft } from "../clamp-draft";
import { NumberStepper } from "./NumberStepper";
import {
  autoSwitchDescriptionKey,
  autoSwitchKeyCommand,
  type AutoSwitchFeedbackView,
} from "../lib/codex-auto-switch-policy";
import type { AccountPoolStrategy } from "../account-pool-strategy";

/** Class vocabulary for the card's sections, kept in one place. */
const COPY_CLASS = "codex-auto-switch-copy";
const SUB_CLASS = "card-sub";
const THRESHOLD_CLASS = "codex-auto-switch-threshold";
const FIELD_LABEL_CLASS = "field-label";
const INPUT_WRAP_CLASS = "codex-auto-switch-input-wrap";
const INPUT_CLASS = "input mono codex-auto-switch-input";
const UNIT_CLASS = "codex-auto-switch-unit";
const TOGGLE_SLOT_CLASS = "codex-auto-switch-toggle-slot";
const TOGGLE_CLASS = "toggle";
const TOGGLE_ON_CLASS = "toggle on";
const KNOB_CLASS = "toggle-knob";
const FEEDBACK_CLASS = "codex-auto-switch-feedback";
const FEEDBACK_ERROR_CLASS = "codex-auto-switch-feedback is-error";

/** The threshold input's range; the steppers clamp to the same pair. */
const THRESHOLD_MIN = 1;
const THRESHOLD_MAX = 100;
const THRESHOLD_STEP = 1;

/** Ids the threshold control points at, so the copy and the feedback can be announced. */
const DESCRIPTION_ID = "codex-auto-switch-desc";
const FEEDBACK_ID = "codex-auto-switch-feedback";

/** The unit drawn inside the threshold input. */
const PERCENT_UNIT = "%";

/** Copy keys this file renders. */
const TITLE_KEY: TKey = "codexAuth.autoSwitch";
const LOAD_FAILED_KEY: TKey = "codexAuth.autoSwitchLoadFailed";
const THRESHOLD_LABEL_KEY: TKey = "codexAuth.autoSwitchThreshold";
const THRESHOLD_ARIA_KEY: TKey = "codexAuth.autoSwitchThresholdAria";
const THRESHOLD_INC_KEY: TKey = "codexAuth.autoSwitchThresholdInc";
const THRESHOLD_DEC_KEY: TKey = "codexAuth.autoSwitchThresholdDec";
const SAVING_KEY: TKey = "common.saving";

/** The two standing caveats under the description. Each answers a different question. */
const STANDING_NOTES = ["codexAuth.failureRecoveryNote", "codexAuth.cacheWarning"] as const satisfies readonly TKey[];

export function CodexAutoSwitchCopy({
  strategy,
  enabled,
  loadError,
  threshold,
}: {
  strategy: AccountPoolStrategy;
  enabled: boolean;
  loadError: boolean;
  threshold: number;
}) {
  const t = useT();
  const description = loadError
    ? t(LOAD_FAILED_KEY)
    : t(autoSwitchDescriptionKey(strategy, enabled), { threshold });
  return (
    <div className={COPY_CLASS}>
      <strong>{t(TITLE_KEY)}</strong>
      <div id={DESCRIPTION_ID} className={SUB_CLASS} role={loadError ? "alert" : undefined}>
        {description}
      </div>
      {STANDING_NOTES.map(key => <div className={SUB_CLASS} key={key}>{t(key)}</div>)}
    </div>
  );
}

export function CodexAutoSwitchThresholdField({
  draft,
  controlsDisabled,
  describedBy,
  onDraftChange,
  onEditingChange,
  onCommit,
  onCancel,
}: {
  draft: string;
  controlsDisabled: boolean;
  describedBy: string;
  onDraftChange(value: string): void;
  onEditingChange(editing: boolean): void;
  onCommit(): Promise<boolean>;
  onCancel(): void;
}) {
  const t = useT();
  // A stepper press is an edit like any other, so it opens the draft before it moves it.
  const step = (delta: number) => {
    onEditingChange(true);
    onDraftChange(clampNumberDraft(draft, delta, THRESHOLD_MIN, THRESHOLD_MAX));
  };
  return (
    <label className={THRESHOLD_CLASS}>
      <span className={FIELD_LABEL_CLASS}>{t(THRESHOLD_LABEL_KEY)}</span>
      <span className={INPUT_WRAP_CLASS}>
        <input
          className={INPUT_CLASS}
          type="number"
          min={THRESHOLD_MIN}
          max={THRESHOLD_MAX}
          step={THRESHOLD_STEP}
          inputMode="numeric"
          value={draft}
          readOnly={controlsDisabled}
          aria-disabled={controlsDisabled}
          aria-label={t(THRESHOLD_ARIA_KEY)}
          aria-describedby={describedBy}
          onChange={event => onDraftChange(event.target.value)}
          onFocus={() => {
            if (!controlsDisabled) onEditingChange(true);
          }}
          onKeyDown={event => {
            const command = autoSwitchKeyCommand({
              composing: event.nativeEvent.isComposing,
              controlsDisabled,
              key: event.key,
            });
            if (command === "ignore" || command === "none") return;
            event.preventDefault();
            if (command === "commit") void onCommit();
            else onCancel();
          }}
        />
        <span className={UNIT_CLASS} aria-hidden="true">{PERCENT_UNIT}</span>
        <NumberStepper
          disabled={controlsDisabled}
          incrementLabel={t(THRESHOLD_INC_KEY)}
          decrementLabel={t(THRESHOLD_DEC_KEY)}
          onIncrement={() => step(1)}
          onDecrement={() => step(-1)}
        />
      </span>
    </label>
  );
}

/**
 * The switch, wrapped in a slot as tall as the threshold field.
 *
 * The row is bottom-aligned, so a 20px switch next to a 32px number compound would sit 6px
 * off the shared center line; the slot fixes that without flipping the container to center
 * alignment, which the mobile breakpoint deliberately depends on. The slot renders outside
 * the enabled branch so the switch does not move when the field beside it appears.
 */
export function CodexAutoSwitchToggle({
  enabled,
  controlsDisabled,
  describedBy,
  onPointerDown,
  onPointerUp,
  onPointerCancel,
  onToggle,
}: {
  enabled: boolean;
  controlsDisabled: boolean;
  describedBy: string;
  onPointerDown(): void;
  onPointerUp(): void;
  onPointerCancel(): void;
  onToggle(): void;
}) {
  const t = useT();
  const label = t(TITLE_KEY);
  return (
    <span className={TOGGLE_SLOT_CLASS}>
      <button
        type="button"
        className={enabled ? TOGGLE_ON_CLASS : TOGGLE_CLASS}
        onPointerDownCapture={onPointerDown}
        onPointerUp={onPointerUp}
        onPointerCancel={onPointerCancel}
        onClick={onToggle}
        disabled={controlsDisabled}
        aria-pressed={enabled}
        aria-label={label}
        aria-describedby={describedBy}
        title={label}
      >
        <span className={KNOB_CLASS} />
      </button>
    </span>
  );
}

export function CodexAutoSwitchFeedbackBanner({ view }: { view: AutoSwitchFeedbackView }) {
  const t = useT();
  if (view.kind === "empty") return null;
  const isError = view.kind === "message" && view.tone === "err";
  const message = view.kind === "saving" ? t(SAVING_KEY) : view.message;
  return (
    <div
      id={FEEDBACK_ID}
      className={isError ? FEEDBACK_ERROR_CLASS : FEEDBACK_CLASS}
      role={isError ? "alert" : "status"}
      aria-atomic="true"
    >
      {message}
    </div>
  );
}
