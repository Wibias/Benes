/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useT } from "../i18n/shared";
import {
  usePoolRotation,
  type PoolRotationBinding,
  type PoolRotationErrorKey,
  type PoolRotationSource,
} from "../codex-pool-rotation-machine.ts";
import AccountPoolStrategyControls from "./AccountPoolStrategyControls";

/**
 * The card's render decisions, derived once so the markup below only reads booleans.
 *
 * `connected` is false until an authoritative `/active` read confirms the values, which is
 * why the controls are inert while the card's own defaults are already painted.
 */
interface RotationCardView {
  busy: boolean;
  controlsDisabled: boolean;
  loadFailureVisible: boolean;
  retryVisible: boolean;
  controlsVisible: boolean;
  errorKey: PoolRotationErrorKey | null;
  errorVisible: boolean;
}

function rotationCardView(binding: PoolRotationBinding): RotationCardView {
  return {
    busy: binding.saving || !binding.connected,
    controlsDisabled: binding.locked,
    loadFailureVisible: binding.readFailed,
    retryVisible: binding.readFailed,
    controlsVisible: !binding.readFailed,
    errorKey: binding.errorKey,
    errorVisible: binding.errorKey !== null,
  };
}

/** One notice line: the copy is always the caller's translation of `message`. */
function RotationNotice({ tone, message, action }: {
  tone: "plain" | "failure";
  message: string;
  action?: { label: string; run(): void };
}) {
  return (
    <>
      <div className={tone === "failure" ? "card-sub account-pool-strategy-card__error" : "card-sub"} role="alert">
        {message}
      </div>
      {action !== undefined && (
        <button type="button" className="btn btn-ghost btn-sm account-pool-strategy-card__retry" onClick={action.run}>
          {action.label}
        </button>
      )}
    </>
  );
}

/**
 * Codex account-pool rotation controls.
 *
 * Presentation only: the state machine and the `/active` wiring live in
 * `codex-pool-rotation-machine`, so this file renders the current rotation and forwards
 * the operator's intent.
 */
export default function CodexPoolStrategySetting(source: PoolRotationSource) {
  const t = useT();
  const binding = usePoolRotation(source);
  const card = rotationCardView(binding);

  return (
    <div className="card account-pool-strategy-card" aria-busy={card.busy}>
      {/* The title and description live in the setting row itself, so the card never
          repeats them above an unnamed select. Only a load failure still needs its own
          line: there is no row to attach it to once the controls become a retry button. */}
      {card.loadFailureVisible && (
        <RotationNotice
          tone="plain"
          message={t("accountPool.strategyLoadFailed")}
          action={{ label: t("common.retry"), run: binding.reload }}
        />
      )}
      {card.controlsVisible && (
        <AccountPoolStrategyControls
          strategy={binding.visible.strategy}
          resetOrder={binding.visible.resetOrder}
          stickyDraft={binding.stickyDraft}
          disabled={card.controlsDisabled}
          strategySelectId="codex-pool-strategy"
          stickyInputId="codex-pool-sticky-limit"
          onStrategyChange={binding.chooseStrategy}
          onResetOrderChange={binding.chooseResetOrder}
          onStickyDraftChange={binding.typeSticky}
          onStickyCommit={binding.commitSticky}
        />
      )}
      {card.errorVisible && card.errorKey !== null && (
        <RotationNotice tone="failure" message={t(card.errorKey)} />
      )}
    </div>
  );
}
