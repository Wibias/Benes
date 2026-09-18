/**
 * The Tooltip's rendering projection.
 *
 * Visibility, the show delay, and placement are owned by `tooltip-controller.ts` (and, under
 * it, `floating-layer.ts`). This module only builds the trigger and its portaled bubble.
 */
import { useCallback, useRef } from "react";
import type { ReactNode } from "react";
import { createPortal } from "react-dom";
import { TOOLTIP_TRIGGER_STYLE } from "../../ui-presentation";
import { useTooltipController } from "./tooltip-controller";
import type { TooltipSide } from "../../tooltip-position";

export interface TooltipProps {
  readonly content: ReactNode;
  readonly children: ReactNode;
  readonly side?: TooltipSide;
  readonly maxWidth?: number;
}

/* Hover/focus tooltip — styled replacement for the native `title` attribute. */
export function Tooltip({ content, children, side = "top", maxWidth = 280 }: TooltipProps) {
  const triggerRef = useRef<HTMLButtonElement>(null);
  const bubbleRef = useRef<HTMLSpanElement>(null);
  const tooltip = useTooltipController(side, { trigger: triggerRef, bubble: bubbleRef });

  /**
   * The bubble measures itself when it lands in the DOM, which is the only moment the browser
   * can tell us how big it is and the last moment before it would be painted.
   */
  const remeasure = tooltip.remeasure;
  const attachBubble = useCallback((element: HTMLSpanElement | null) => {
    bubbleRef.current = element;
    if (element !== null) remeasure();
  }, [remeasure]);

  return (
    <button
      ref={triggerRef}
      type="button"
      className="benes-tooltip"
      onMouseEnter={tooltip.show}
      onMouseLeave={tooltip.hide}
      onFocus={tooltip.show}
      onBlur={tooltip.hide}
      onKeyDown={tooltip.handleKeyDown}
      aria-describedby={tooltip.open ? tooltip.tipId : undefined}
      style={TOOLTIP_TRIGGER_STYLE}
    >
      {children}
      {tooltip.open && createPortal(
        <span
          id={tooltip.tipId}
          ref={attachBubble}
          className="benes-tooltip-bubble benes-tooltip-bubble--portaled"
          role="tooltip"
          style={{
            maxWidth,
            top: tooltip.style.top,
            left: tooltip.style.left,
            visibility: tooltip.style.visibility,
          }}
        >
          {content}
        </span>,
        document.body,
      )}
    </button>
  );
}
