import type { MouseEvent, ReactNode } from "react";
import { IconArrowDown, IconArrowUp } from "../icons";

export interface NumberStepperProps {
  onIncrement(): void;
  /** Names the raise action; the parent owns parsing and clamping. */
  incrementLabel: string;
  onDecrement(): void;
  /** Names the lower action. */
  decrementLabel: string;
  disabled?: boolean;
}

/** Geometry of the arrow glyphs; both arrows are the same size. */
const ARROW_GLYPH_SIZE = { width: 10, height: 10 } as const;

/** Chrome both arrows share; only the glyph and the handler differ. */
const ARROW_BUTTON_CHROME = { className: "benes-stepper__btn" } as const;

/**
 * A press is preempted so it never steals focus or the caret from the input whose
 * value the stepper edits. Named rather than inlined because both arrows share it.
 */
function keepInputFocus(event: MouseEvent<HTMLButtonElement>): void {
  event.preventDefault();
}

/**
 * One arrow of the pair. The glyph arrives as children, so the pair below reads
 * as the two directions it offers rather than as a lookup keyed by direction.
 */
function StepperArrow({
  label,
  disabled,
  onStep,
  children,
}: {
  label: string;
  disabled: boolean;
  onStep(): void;
  children: ReactNode;
}) {
  return (
    <button
      type="button"
      {...ARROW_BUTTON_CHROME}
      disabled={disabled}
      aria-label={label}
      onMouseDown={keepInputFocus}
      onClick={onStep}
    >
      {children}
    </button>
  );
}

/** Compact up/down pair matching dashboard control chrome (inbound inside input wraps). */
export function NumberStepper({
  onIncrement,
  incrementLabel,
  onDecrement,
  decrementLabel,
  disabled = false,
}: NumberStepperProps) {
  return (
    <div className="benes-stepper" role="group">
      <StepperArrow label={incrementLabel} disabled={disabled} onStep={onIncrement}>
        <IconArrowUp {...ARROW_GLYPH_SIZE} aria-hidden="true" />
      </StepperArrow>
      <StepperArrow label={decrementLabel} disabled={disabled} onStep={onDecrement}>
        <IconArrowDown {...ARROW_GLYPH_SIZE} aria-hidden="true" />
      </StepperArrow>
    </div>
  );
}
