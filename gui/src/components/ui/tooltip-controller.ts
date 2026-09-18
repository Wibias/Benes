/**
 * Tooltip behaviour.
 *
 * A tooltip is a delayed, hover-or-focus triggered layer. Two things are worth owning
 * separately: the delay discipline (a trigger entered and left repeatedly must leave exactly
 * one pending timer, and unmounting must leave none) and the layer itself, which
 * `floating-layer.ts` already owns. What is left is the projection.
 */
import { useCallback, useEffect, useId, useReducer, useState } from "react";
import type { RefObject } from "react";
import { cssPx } from "../../css-transform.ts";
import { useFloatingLayer } from "./floating-layer.ts";
import type { AnchorBox, LayerSize } from "./floating-layer.ts";
import { computeTooltipStyle } from "../../tooltip-position.ts";
import type { TooltipSide } from "../../tooltip-position.ts";

/** How long the trigger has to be held before the bubble appears. */
export const TOOLTIP_SHOW_DELAY_MS = 150;

/** The slice of the timer API a delay needs, so the discipline is testable without a clock. */
export interface TimerClock {
  setTimeout(run: () => void, delayMs: number): number;
  clearTimeout(handle: number): void;
}

export interface DelayTimer {
  /** (Re)start the delay. A pending run is replaced, never stacked. */
  schedule(run: () => void): void;
  /** Cancel a pending run. Safe when nothing is pending. */
  cancel(): void;
  /** Whether a run is pending. */
  readonly pending: boolean;
}

/**
 * One restartable delay.
 *
 * The handle is owned here rather than at the call site because the failure mode is silent:
 * a second `setTimeout` that nobody cleared leaves a timer that fires after unmount.
 */
export function createDelayTimer(delayMs: number, clock: TimerClock): DelayTimer {
  let handle: number | null = null;
  return {
    schedule(run) {
      if (handle !== null) clock.clearTimeout(handle);
      handle = clock.setTimeout(() => {
        handle = null;
        run();
      }, delayMs);
    },
    cancel() {
      if (handle === null) return;
      clock.clearTimeout(handle);
      handle = null;
    },
    get pending() {
      return handle !== null;
    },
  };
}

/** The window-backed clock. Resolved lazily so importing the module never touches the DOM. */
const systemClock: TimerClock = {
  setTimeout: (run, delayMs) => window.setTimeout(run, delayMs),
  clearTimeout: handle => window.clearTimeout(handle),
};

/** Tooltip visibility. */
export interface TooltipState {
  readonly open: boolean;
}

export type TooltipEvent =
  | { readonly kind: "show" }
  | { readonly kind: "hide" };

export function tooltipState(_state: TooltipState, event: TooltipEvent): TooltipState {
  return { open: event.kind === "show" };
}

export interface TooltipBubbleStyle {
  readonly top: string;
  readonly left: string;
  readonly visibility: "visible" | "hidden";
}

export interface TooltipController {
  readonly open: boolean;
  readonly tipId: string;
  /** Placement for the bubble. Hidden at the origin until it has been measured. */
  readonly style: TooltipBubbleStyle;
  /**
   * Read the trigger and the bubble and place the bubble now. The projection calls this from
   * its own attach callback on the bubble: the first placement has to happen while the bubble is
   * in the DOM and before anything is painted.
   */
  readonly remeasure: () => void;
  show(): void;
  hide(): void;
  /** Escape closes an open bubble. */
  handleKeyDown(event: { key: string }): void;
}

/** The nodes the controller drives; the projection owns them. */
export interface TooltipControllerRefs {
  readonly trigger: RefObject<HTMLButtonElement | null>;
  readonly bubble: RefObject<HTMLSpanElement | null>;
}

export function useTooltipController(
  side: TooltipSide,
  { trigger, bubble }: TooltipControllerRefs,
): TooltipController {
  const [state, dispatch] = useReducer(tooltipState, { open: false });
  // One timer per mounted tooltip, created once rather than per render.
  const [timer] = useState(() => createDelayTimer(TOOLTIP_SHOW_DELAY_MS, systemClock));
  const tipId = useId();

  const place = useCallback(
    (anchor: AnchorBox, size: LayerSize | null) =>
      computeTooltipStyle(anchor, side, size ?? { width: 0, height: 0 }),
    [side],
  );

  const layer = useFloatingLayer<{ top: number; left: number }>({
    open: state.open,
    anchor: trigger,
    layer: bubble,
    place,
  });

  const hide = useCallback(() => {
    timer.cancel();
    dispatch({ kind: "hide" });
  }, [timer]);

  const show = useCallback(() => {
    timer.schedule(() => dispatch({ kind: "show" }));
  }, [timer]);

  useEffect(() => () => timer.cancel(), [timer]);

  const handleKeyDown = useCallback((event: { key: string }) => {
    if (event.key === "Escape") hide();
  }, [hide]);

  const placement = layer.style;
  return {
    open: state.open,
    tipId,
    remeasure: layer.remeasure,
    style: placement === undefined
      ? { top: cssPx(0), left: cssPx(0), visibility: "hidden" }
      : { top: cssPx(placement.top), left: cssPx(placement.left), visibility: "visible" },
    show,
    hide,
    handleKeyDown,
  };
}
