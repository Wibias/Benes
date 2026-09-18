/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useRef } from "react";
import { useT, type TKey } from "../i18n/shared";
import type { AccountPoolStrategy } from "../account-pool-strategy";
import {
  autoSwitchBlurCommit,
  autoSwitchDescribedBy,
  autoSwitchFeedbackView,
  type AutoSwitchCardController,
} from "../lib/codex-auto-switch-policy";
import {
  CodexAutoSwitchCopy,
  CodexAutoSwitchFeedbackBanner,
  CodexAutoSwitchThresholdField,
  CodexAutoSwitchToggle,
} from "./codex-auto-switch-sections";

/** Card chrome, kept in one place. */
const CARD_CLASS = "card card-row codex-auto-switch-card";
const CONTROLS_CLASS = "codex-auto-switch-controls";
const RETRY_BUTTON_CLASS = "btn btn-ghost btn-sm";
const CARD_TOP_GAP = { marginTop: 16 } as const;

const RETRY_ACCOUNTS: TKey = "pws.retryAccounts";

export interface CodexAutoSwitchSettingProps {
  /** The controller the Access page owns; the card never opens its own /active read. */
  controller: AutoSwitchCardController;
  strategy?: AccountPoolStrategy;
}

/**
 * Remembers that a press started on the switch.
 *
 * Pressing the switch moves focus, which fires the row's blur handler before the click —
 * and that blur must not be read as "the operator finished typing". The flag is set on
 * pointer-down and dropped again once the press is over, one way or another.
 */
function useSwitchPress() {
  const armed = useRef(false);
  return {
    isArmed: () => armed.current,
    press: () => { armed.current = true; },
    release: () => { armed.current = false; },
  };
}

/**
 * Codex auto-switch threshold settings.
 *
 * Presentation and intent only. The transitions live in the auto-switch hook and the copy,
 * blur and key policy in the auto-switch policy module, so the card renders the current
 * threshold and forwards what the operator did. It takes the controller rather than its
 * fourteen fields, which keeps the card's own interface about the card.
 */
export function CodexAutoSwitchSetting({ controller, strategy = "quota" }: CodexAutoSwitchSettingProps) {
  const t = useT();
  const press = useSwitchPress();
  const enabled = controller.threshold > 0;
  const controlsDisabled = controller.saving || !controller.hydrated;
  const feedbackView = autoSwitchFeedbackView(controller.saving, controller.feedback);
  const describedBy = autoSwitchDescribedBy(feedbackView);
  const awaitingFirstRead = !controller.hydrated && !controller.loadError;

  return (
    <div
      className={CARD_CLASS}
      style={CARD_TOP_GAP}
      aria-busy={controller.saving || awaitingFirstRead || undefined}
    >
      <CodexAutoSwitchCopy
        strategy={strategy}
        enabled={enabled}
        loadError={controller.loadError}
        threshold={controller.threshold}
      />
      <div
        className={CONTROLS_CLASS}
        onBlur={event => {
          const action = autoSwitchBlurCommit({
            relatedTargetInside: event.currentTarget.contains(event.relatedTarget as Node | null),
            pointerIntent: press.isArmed(),
            enabled,
            controlsDisabled,
          });
          if (action === "ignore") return;
          controller.setEditing(false);
          if (action === "clear-pointer") {
            press.release();
            return;
          }
          if (action === "commit") void controller.commit();
        }}
      >
        {controller.loadError && (
          <button type="button" className={RETRY_BUTTON_CLASS} onClick={controller.retry}>
            {t(RETRY_ACCOUNTS)}
          </button>
        )}
        {enabled && (
          <CodexAutoSwitchThresholdField
            draft={controller.draft}
            controlsDisabled={controlsDisabled}
            describedBy={describedBy}
            onDraftChange={controller.setDraft}
            onEditingChange={controller.setEditing}
            onCommit={controller.commit}
            onCancel={controller.cancel}
          />
        )}
        <CodexAutoSwitchToggle
          enabled={enabled}
          controlsDisabled={controlsDisabled}
          describedBy={describedBy}
          onPointerDown={press.press}
          onPointerUp={press.release}
          onPointerCancel={press.release}
          onToggle={() => {
            press.release();
            void controller.toggle();
          }}
        />
      </div>
      <CodexAutoSwitchFeedbackBanner view={feedbackView} />
    </div>
  );
}

export default CodexAutoSwitchSetting;
