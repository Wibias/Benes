/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { ReactNode } from "react";
import { useT, type TKey } from "../i18n/shared";
import {
  ACCOUNT_POOL_RESET_ORDERS,
  ACCOUNT_POOL_STRATEGIES,
  type AccountPoolResetOrder,
  type AccountPoolStrategy,
} from "../account-pool-strategy";
import { clampNumberDraft } from "../clamp-draft";
import { NumberStepper } from "./NumberStepper";
import { Select } from "../ui";

/** Class vocabulary for the rotation controls, kept in one place. */
const CONTROLS_CLASS = "account-pool-strategy-controls";
const ROW_CLASS = "setting-row";
const LABEL_CLASS = "setting-label";
const TITLE_CLASS = "title";
const DESC_CLASS = "desc";
const CONTROLS_COLUMN_CLASS = "setting-controls";
const NUMBER_WRAP_CLASS = "codex-auto-switch-input-wrap";
const NUMBER_INPUT_CLASS = "input mono codex-auto-switch-input";

/** The sticky limit's range; the steppers clamp to the same pair. */
const STICKY_MIN = 1;
const STICKY_MAX = 100;
const STICKY_STEP = 1;

/** Copy for every strategy. The record shape makes a new strategy a compile error here. */
const STRATEGY_COPY: Record<AccountPoolStrategy, { label: TKey; hint: TKey }> = {
  quota: { label: "accountPool.strategyQuota", hint: "accountPool.strategyHintQuota" },
  "round-robin": { label: "accountPool.strategyRoundRobin", hint: "accountPool.strategyHintRoundRobin" },
  "fill-first": { label: "accountPool.strategyFillFirst", hint: "accountPool.strategyHintFillFirst" },
  "reset-window": { label: "accountPool.strategyResetWindow", hint: "accountPool.strategyHintResetWindow" },
};

const RESET_ORDER_COPY: Record<AccountPoolResetOrder, TKey> = {
  soonest: "accountPool.resetOrderSoonest",
  latest: "accountPool.resetOrderLatest",
};

/** Copy for the rows themselves and for the controls inside them. */
const ROTATION_TITLE: TKey = "accountPool.strategy";
const ROTATION_DESC: TKey = "accountPool.strategyDesc";
const ROTATION_AFFINITY: TKey = "accountPool.unboundDefinition";
const RESET_ORDER_TITLE: TKey = "accountPool.resetOrder";
const RESET_ORDER_DESC: TKey = "accountPool.resetOrderDesc";
const STICKY_TITLE: TKey = "accountPool.stickyLimit";
const STICKY_DESC: TKey = "accountPool.stickyLimitHelp";
const STICKY_ARIA: TKey = "accountPool.stickyLimitAria";
const STICKY_INC: TKey = "accountPool.stickyLimitInc";
const STICKY_DEC: TKey = "accountPool.stickyLimitDec";

/**
 * One line of the controls.
 *
 * The rows differ in which element carries the name: a plain description block, or a
 * `<label>` bound to the input it captions. The element is chosen from `htmlFor` rather
 * than by keeping two near-identical blocks of markup.
 */
interface RotationRow {
  key: string;
  title: string;
  /** The label element's own id; omitted where nothing references it. */
  titleId?: string;
  /** Set on rows whose label names a control; those rows render a `<label>`. */
  htmlFor?: string;
  descriptions: string[];
  control: ReactNode;
}

function RotationRowView({ row }: { row: RotationRow }) {
  const caption = (
    <>
      <span className={TITLE_CLASS} id={row.titleId}>{row.title}</span>
      {row.descriptions.map((text, index) => (
        <span className={DESC_CLASS} key={index}>{text}</span>
      ))}
    </>
  );
  return (
    <div className={ROW_CLASS}>
      {row.htmlFor === undefined
        ? <div className={LABEL_CLASS}>{caption}</div>
        : <label className={LABEL_CLASS} htmlFor={row.htmlFor}>{caption}</label>}
      <div className={CONTROLS_COLUMN_CLASS}>{row.control}</div>
    </div>
  );
}

/** A control's label id, derived from the pool's select id so two pools never collide. */
function labelIdOf(selectId: string, suffix: string): string {
  return selectId + "-" + suffix;
}

/** The ids a pool's two labelled controls pair their labels with. */
export interface AccountPoolControlsIds {
  strategySelectId?: string;
  stickyInputId?: string;
}

export interface AccountPoolStrategyControlsProps extends AccountPoolControlsIds {
  strategy: AccountPoolStrategy;
  resetOrder?: AccountPoolResetOrder;
  stickyDraft: string;
  disabled?: boolean;
  onStrategyChange(strategy: AccountPoolStrategy): void;
  onResetOrderChange?(order: AccountPoolResetOrder): void;
  onStickyDraftChange(value: string): void;
  /** Optional draft override when a stepper commits in the same tick as a draft change. */
  onStickyCommit(nextDraft?: string): void;
}

/**
 * The sticky limit, with the steppers that commit as they move.
 *
 * A stepper press is a draft change and a commit at once, which is why the two callbacks
 * are driven from one step function rather than from the arrows individually.
 */
function StickyLimitControl({
  inputId,
  draft,
  disabled,
  onDraftChange,
  onCommit,
}: {
  inputId: string;
  draft: string;
  disabled: boolean;
  onDraftChange(value: string): void;
  onCommit(nextDraft?: string): void;
}) {
  const t = useT();
  const step = (delta: number) => {
    const next = clampNumberDraft(draft, delta, STICKY_MIN, STICKY_MAX);
    onDraftChange(next);
    onCommit(next);
  };
  return (
    <span className={NUMBER_WRAP_CLASS}>
      <input
        id={inputId}
        className={NUMBER_INPUT_CLASS}
        type="number"
        min={STICKY_MIN}
        max={STICKY_MAX}
        step={STICKY_STEP}
        inputMode="numeric"
        value={draft}
        disabled={disabled}
        aria-label={t(STICKY_ARIA)}
        onChange={event => onDraftChange(event.target.value)}
        onBlur={() => onCommit()}
        onKeyDown={event => {
          if (event.nativeEvent.isComposing || disabled) return;
          if (event.key !== "Enter") return;
          event.preventDefault();
          onCommit();
        }}
      />
      <NumberStepper
        disabled={disabled}
        incrementLabel={t(STICKY_INC)}
        decrementLabel={t(STICKY_DEC)}
        onIncrement={() => step(1)}
        onDecrement={() => step(-1)}
      />
    </span>
  );
}

/**
 * Rotation controls shared by the Codex and Anthropic account pools.
 *
 * Both pools expose the same three settings over the same value domains, so the rows are
 * assembled here from one description each and rendered by `RotationRowView`. The strategy
 * row always shows; the other two belong to the strategies they qualify, which is why a
 * non-qualifying strategy simply does not contribute a row.
 *
 * The strategy row states three separate things on purpose: what the setting does, what it
 * means for a thread that is already running, and what "unbound" counts. Collapsing them
 * would drop the answer about account affinity.
 */
export default function AccountPoolStrategyControls({
  strategy,
  resetOrder = "soonest",
  stickyDraft,
  disabled = false,
  strategySelectId = "account-pool-strategy",
  stickyInputId = "account-pool-sticky-limit",
  onStrategyChange,
  onResetOrderChange,
  onStickyDraftChange,
  onStickyCommit,
}: AccountPoolStrategyControlsProps) {
  const t = useT();
  const strategyName = t(ROTATION_TITLE);
  const resetOrderName = t(RESET_ORDER_TITLE);

  const rows: RotationRow[] = [{
    key: "strategy",
    title: strategyName,
    titleId: labelIdOf(strategySelectId, "label"),
    descriptions: [t(ROTATION_DESC), t(STRATEGY_COPY[strategy].hint), t(ROTATION_AFFINITY)],
    control: (
      <Select
        id={strategySelectId}
        value={strategy}
        options={ACCOUNT_POOL_STRATEGIES.map(value => ({ value, label: t(STRATEGY_COPY[value].label) }))}
        disabled={disabled}
        label={strategyName}
        onChange={next => onStrategyChange(next as AccountPoolStrategy)}
      />
    ),
  }];

  if (strategy === "reset-window" && onResetOrderChange !== undefined) {
    const chooseOrder = onResetOrderChange;
    rows.push({
      key: "reset-order",
      title: resetOrderName,
      titleId: labelIdOf(strategySelectId, "reset-order-label"),
      descriptions: [t(RESET_ORDER_DESC)],
      control: (
        <Select
          id={`${strategySelectId}-reset-order`}
          value={resetOrder}
          options={ACCOUNT_POOL_RESET_ORDERS.map(value => ({ value, label: t(RESET_ORDER_COPY[value]) }))}
          disabled={disabled}
          label={resetOrderName}
          onChange={next => chooseOrder(next as AccountPoolResetOrder)}
        />
      ),
    });
  }

  if (strategy === "round-robin") {
    rows.push({
      key: "sticky-limit",
      title: t(STICKY_TITLE),
      htmlFor: stickyInputId,
      descriptions: [t(STICKY_DESC)],
      control: (
        <StickyLimitControl
          inputId={stickyInputId}
          draft={stickyDraft}
          disabled={disabled}
          onDraftChange={onStickyDraftChange}
          onCommit={onStickyCommit}
        />
      ),
    });
  }

  return (
    <div className={CONTROLS_CLASS}>
      {rows.map(row => <RotationRowView key={row.key} row={row} />)}
    </div>
  );
}
